package agent

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/russellhaering/autoswe/pkg/llm"
	"github.com/russellhaering/autoswe/pkg/permissions"
	"github.com/russellhaering/autoswe/pkg/tools"
)

func (a *Agent) run(ctx context.Context, input string, emit func(Event)) (Result, error) {
	messages := make([]llm.Message, 0, len(a.opts.InitialMessages)+1)
	messages = append(messages, a.opts.InitialMessages...)
	if input != "" {
		messages = append(messages, llm.Message{
			Role:    llm.RoleUser,
			Content: []llm.ContentBlock{llm.TextBlock{Text: input}},
		})
	}
	if len(messages) == 0 {
		return Result{}, fmt.Errorf("agent: no input and no prior messages")
	}
	var (
		totalUsage llm.Usage
		stopReason llm.StopReason
		lastText   string
		turn       int
	)

	for {
		if a.opts.MaxTurns > 0 && turn >= a.opts.MaxTurns {
			return Result{Messages: messages}, fmt.Errorf("agent: exceeded MaxTurns=%d", a.opts.MaxTurns)
		}
		turn++

		req := llm.Request{
			Model:       a.opts.Model,
			System:      a.opts.System,
			Messages:    messages,
			Tools:       a.opts.Tools.Specs(),
			MaxTokens:   a.opts.MaxTokens,
			Temperature: a.opts.Temperature,
		}

		stream, err := a.opts.Provider.Stream(ctx, req)
		if err != nil {
			return Result{Messages: messages}, fmt.Errorf("provider stream: %w", err)
		}

		var (
			turnText   strings.Builder
			toolUses   []llm.ToolUseBlock
			serverBlks []llm.ServerToolBlock
			turnStop   llm.StopReason
			turnUsage  llm.Usage
			streamErr  error
		)

		for ev := range stream {
			switch e := ev.(type) {
			case llm.TextDelta:
				turnText.WriteString(e.Text)
				emit(TextDelta{Text: e.Text})
			case llm.ToolUseStop:
				toolUses = append(toolUses, llm.ToolUseBlock{ID: e.ID, Name: e.Name, Input: e.Input})
				emit(ToolUse{ID: e.ID, Name: e.Name, Input: e.Input})
			case llm.ServerToolStop:
				serverBlks = append(serverBlks, llm.ServerToolBlock{
					Provider:  e.Provider,
					BlockType: e.BlockType,
					Raw:       e.Raw,
				})
				if e.BlockType == "server_tool_use" && e.Name != "" {
					emit(ServerToolUse{Provider: e.Provider, Name: e.Name, Raw: e.Raw})
				}
			case llm.MessageStop:
				turnStop = e.Reason
				turnUsage = e.Usage
			case llm.ErrorEvent:
				streamErr = e.Err
			}
		}

		totalUsage.InputTokens += turnUsage.InputTokens
		totalUsage.OutputTokens += turnUsage.OutputTokens

		if streamErr != nil {
			return Result{Messages: messages, Usage: totalUsage}, streamErr
		}

		var content []llm.ContentBlock
		if turnText.Len() > 0 {
			content = append(content, llm.TextBlock{Text: turnText.String()})
		}
		for _, sb := range serverBlks {
			content = append(content, sb)
		}
		for _, tu := range toolUses {
			content = append(content, tu)
		}
		messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: content})

		stopReason = turnStop
		if turnStop != llm.StopToolUse {
			lastText = turnText.String()
			emit(Stop{Reason: turnStop, Usage: totalUsage})
			return Result{
				Text:     lastText,
				Stop:     stopReason,
				Usage:    totalUsage,
				Messages: messages,
			}, nil
		}

		outputs, err := a.dispatch(ctx, toolUses, emit)
		if err != nil {
			return Result{Messages: messages, Usage: totalUsage}, err
		}
		userContent := make([]llm.ContentBlock, 0, len(outputs))
		exit := false
		for _, o := range outputs {
			userContent = append(userContent, o.Block)
			if o.ExitAfter {
				exit = true
			}
		}
		messages = append(messages, llm.Message{Role: llm.RoleUser, Content: userContent})

		if exit {
			emit(Stop{Reason: llm.StopReason("plan_submitted"), Usage: totalUsage})
			return Result{
				Stop:     llm.StopReason("plan_submitted"),
				Usage:    totalUsage,
				Messages: messages,
			}, nil
		}
	}
}

type dispatchOutput struct {
	Block     llm.ToolResultBlock
	ExitAfter bool
}

func (a *Agent) dispatch(ctx context.Context, toolUses []llm.ToolUseBlock, emit func(Event)) ([]dispatchOutput, error) {
	outputs := make([]dispatchOutput, len(toolUses))
	g, gctx := errgroup.WithContext(ctx)
	for i, tu := range toolUses {
		g.Go(func() error {
			outputs[i] = a.dispatchOne(gctx, tu, emit)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return outputs, nil
}

func (a *Agent) dispatchOne(ctx context.Context, tu llm.ToolUseBlock, emit func(Event)) dispatchOutput {
	finalize := func(call permissions.ToolCall, res tools.Result) dispatchOutput {
		if a.opts.PostToolHook != nil {
			res = a.opts.PostToolHook(ctx, call, res)
		}
		emit(ToolResult{ToolUseID: tu.ID, Name: tu.Name, Content: res.Content, IsError: res.IsError})
		return dispatchOutput{
			Block:     llm.ToolResultBlock{ToolUseID: tu.ID, Content: res.Content, IsError: res.IsError},
			ExitAfter: res.ExitAfter,
		}
	}

	tool, ok := a.opts.Tools.Get(tu.Name)
	call := permissions.ToolCall{Name: tu.Name, Args: tu.Input, Tool: tool}

	if a.opts.PreToolHook != nil {
		modified, err := a.opts.PreToolHook(ctx, call)
		if err != nil {
			return finalize(call, tools.Result{IsError: true, Content: fmt.Sprintf("pre-tool hook: %v", err)})
		}
		call = modified
	}

	if !ok {
		return finalize(call, tools.Result{IsError: true, Content: fmt.Sprintf("tool %q not found", tu.Name)})
	}

	decision, err := a.opts.Policy.Check(ctx, call)
	if err != nil {
		return finalize(call, tools.Result{IsError: true, Content: fmt.Sprintf("policy error: %v", err)})
	}

	args := call.Args
	switch decision.Action {
	case permissions.Deny:
		reason := decision.Reason
		if reason == "" {
			reason = "denied by policy"
		}
		a.opts.Logger.InfoContext(ctx, "tool denied", "tool", tu.Name, "reason", reason)
		return finalize(call, tools.Result{IsError: true, Content: reason})
	case permissions.Modify:
		if len(decision.ModifiedArgs) > 0 {
			args = decision.ModifiedArgs
		}
		a.opts.Logger.InfoContext(ctx, "tool modified", "tool", tu.Name)
	}

	res, err := tool.Run(ctx, args)
	if err != nil {
		return finalize(call, tools.Result{IsError: true, Content: fmt.Sprintf("tool error: %v", err)})
	}
	return finalize(call, res)
}
