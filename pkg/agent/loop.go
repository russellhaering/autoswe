package agent

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/russellhaering/autoswe/pkg/llm"
	"github.com/russellhaering/autoswe/pkg/permissions"
)

func (a *Agent) run(ctx context.Context, input string, emit func(Event)) (Result, error) {
	messages := []llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: input}}},
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
			turnText  strings.Builder
			toolUses  []llm.ToolUseBlock
			turnStop  llm.StopReason
			turnUsage llm.Usage
			streamErr error
		)

		for ev := range stream {
			switch e := ev.(type) {
			case llm.TextDelta:
				turnText.WriteString(e.Text)
				emit(TextDelta{Text: e.Text})
			case llm.ToolUseStop:
				toolUses = append(toolUses, llm.ToolUseBlock{ID: e.ID, Name: e.Name, Input: e.Input})
				emit(ToolUse{ID: e.ID, Name: e.Name, Input: e.Input})
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

		results, err := a.dispatch(ctx, toolUses, emit)
		if err != nil {
			return Result{Messages: messages, Usage: totalUsage}, err
		}
		userContent := make([]llm.ContentBlock, 0, len(results))
		for _, r := range results {
			userContent = append(userContent, r)
		}
		messages = append(messages, llm.Message{Role: llm.RoleUser, Content: userContent})
	}
}

func (a *Agent) dispatch(ctx context.Context, toolUses []llm.ToolUseBlock, emit func(Event)) ([]llm.ToolResultBlock, error) {
	results := make([]llm.ToolResultBlock, len(toolUses))
	g, gctx := errgroup.WithContext(ctx)
	for i, tu := range toolUses {
		g.Go(func() error {
			results[i] = a.dispatchOne(gctx, tu, emit)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

func (a *Agent) dispatchOne(ctx context.Context, tu llm.ToolUseBlock, emit func(Event)) llm.ToolResultBlock {
	makeResult := func(content string, isError bool) llm.ToolResultBlock {
		emit(ToolResult{ToolUseID: tu.ID, Name: tu.Name, Content: content, IsError: isError})
		return llm.ToolResultBlock{ToolUseID: tu.ID, Content: content, IsError: isError}
	}

	tool, ok := a.opts.Tools.Get(tu.Name)
	if !ok {
		return makeResult(fmt.Sprintf("tool %q not found", tu.Name), true)
	}

	decision, err := a.opts.Policy.Check(ctx, permissions.ToolCall{
		Name: tu.Name,
		Args: tu.Input,
		Tool: tool,
	})
	if err != nil {
		return makeResult(fmt.Sprintf("policy error: %v", err), true)
	}

	args := tu.Input
	switch decision.Action {
	case permissions.Deny:
		reason := decision.Reason
		if reason == "" {
			reason = "denied by policy"
		}
		a.opts.Logger.InfoContext(ctx, "tool denied", "tool", tu.Name, "reason", reason)
		return makeResult(reason, true)
	case permissions.Modify:
		if len(decision.ModifiedArgs) > 0 {
			args = decision.ModifiedArgs
		}
		a.opts.Logger.InfoContext(ctx, "tool modified", "tool", tu.Name)
	}

	res, err := tool.Run(ctx, args)
	if err != nil {
		return makeResult(fmt.Sprintf("tool error: %v", err), true)
	}
	return makeResult(res.Content, res.IsError)
}
