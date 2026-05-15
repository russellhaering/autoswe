package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/russellhaering/autoswe/pkg/llm"
	"github.com/russellhaering/autoswe/pkg/permissions"
	"github.com/russellhaering/autoswe/pkg/tools"
)

type stubProvider struct {
	mu       sync.Mutex
	streams  [][]llm.Event
	requests []llm.Request
}

func (s *stubProvider) Name() string { return "stub" }

func (s *stubProvider) Stream(_ context.Context, req llm.Request) (<-chan llm.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, req)
	if len(s.streams) == 0 {
		return nil, errors.New("stub: no streams left")
	}
	events := s.streams[0]
	s.streams = s.streams[1:]
	ch := make(chan llm.Event, len(events)+1)
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

type stubTool struct {
	name    string
	effects []tools.Effect

	mu     sync.Mutex
	calls  []json.RawMessage
	result tools.Result
}

func (t *stubTool) Name() string                 { return t.name }
func (t *stubTool) Description() string          { return "" }
func (t *stubTool) Schema() json.RawMessage      { return json.RawMessage(`{"type":"object"}`) }
func (t *stubTool) Effects() []tools.Effect      { return t.effects }
func (t *stubTool) Run(_ context.Context, args json.RawMessage) (tools.Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls = append(t.calls, args)
	return t.result, nil
}

func newAgent(t *testing.T, p llm.Provider, reg *tools.Registry, pol permissions.Policy) *Agent {
	t.Helper()
	a, err := New(Options{
		Provider: p, Model: "test-model", Tools: reg, Policy: pol, MaxTurns: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestRunSingleTurnText(t *testing.T) {
	p := &stubProvider{streams: [][]llm.Event{{
		llm.TextDelta{Text: "hello "},
		llm.TextDelta{Text: "world"},
		llm.MessageStop{Reason: llm.StopEndTurn, Usage: llm.Usage{InputTokens: 5, OutputTokens: 2}},
	}}}
	a := newAgent(t, p, tools.NewRegistry(), permissions.AllowAll{})
	res, err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "hello world" {
		t.Fatalf("text=%q", res.Text)
	}
	if res.Stop != llm.StopEndTurn {
		t.Fatalf("stop=%v", res.Stop)
	}
	if res.Usage.InputTokens != 5 || res.Usage.OutputTokens != 2 {
		t.Fatalf("usage=%+v", res.Usage)
	}
	if len(res.Messages) != 2 || res.Messages[0].Role != llm.RoleUser || res.Messages[1].Role != llm.RoleAssistant {
		t.Fatalf("messages=%+v", res.Messages)
	}
}

func TestRunDispatchesToolAndReturns(t *testing.T) {
	tool := &stubTool{
		name:    "read",
		effects: []tools.Effect{tools.EffectReadOnly},
		result:  tools.Result{Content: "file contents"},
	}
	reg := tools.NewRegistry()
	_ = reg.Register(tool)

	p := &stubProvider{streams: [][]llm.Event{
		{
			llm.TextDelta{Text: "Let me read it."},
			llm.ToolUseStop{ID: "toolu_1", Name: "read", Input: json.RawMessage(`{"path":"x"}`)},
			llm.MessageStop{Reason: llm.StopToolUse, Usage: llm.Usage{InputTokens: 10, OutputTokens: 5}},
		},
		{
			llm.TextDelta{Text: "Done."},
			llm.MessageStop{Reason: llm.StopEndTurn, Usage: llm.Usage{InputTokens: 20, OutputTokens: 1}},
		},
	}}
	a := newAgent(t, p, reg, permissions.AllowAll{})
	res, err := a.Run(context.Background(), "read x")
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Done." {
		t.Fatalf("text=%q", res.Text)
	}
	if len(tool.calls) != 1 || string(tool.calls[0]) != `{"path":"x"}` {
		t.Fatalf("tool calls=%v", tool.calls)
	}
	if res.Usage.OutputTokens != 6 || res.Usage.InputTokens != 30 {
		t.Fatalf("usage=%+v (expected accumulated)", res.Usage)
	}
	// Messages: user, assistant(text+tool_use), user(tool_result), assistant(text)
	if len(res.Messages) != 4 {
		t.Fatalf("want 4 messages, got %d: %+v", len(res.Messages), res.Messages)
	}
}

func TestRunDenyPolicySurfacesReason(t *testing.T) {
	tool := &stubTool{name: "bash", effects: []tools.Effect{tools.EffectCodeExecution}}
	reg := tools.NewRegistry()
	_ = reg.Register(tool)

	p := &stubProvider{streams: [][]llm.Event{
		{
			llm.ToolUseStop{ID: "tu", Name: "bash", Input: json.RawMessage(`{}`)},
			llm.MessageStop{Reason: llm.StopToolUse},
		},
		{
			llm.TextDelta{Text: "ok"},
			llm.MessageStop{Reason: llm.StopEndTurn},
		},
	}}
	a := newAgent(t, p, reg, permissions.AllowReadOnly{})

	var sawReason string
	events, _ := a.RunStream(context.Background(), "do it")
	for ev := range events {
		if r, ok := ev.(ToolResult); ok && r.ToolUseID == "tu" {
			sawReason = r.Content
			if !r.IsError {
				t.Fatal("denied tool result should be IsError")
			}
		}
	}
	if sawReason == "" {
		t.Fatal("expected deny reason surfaced")
	}
	if len(tool.calls) != 0 {
		t.Fatalf("tool should not have run, got calls=%v", tool.calls)
	}
}

func TestRunModifyPolicyRewritesArgs(t *testing.T) {
	tool := &stubTool{name: "read", effects: []tools.Effect{tools.EffectReadOnly}, result: tools.Result{Content: "ok"}}
	reg := tools.NewRegistry()
	_ = reg.Register(tool)

	p := &stubProvider{streams: [][]llm.Event{
		{
			llm.ToolUseStop{ID: "tu", Name: "read", Input: json.RawMessage(`{"path":"/etc/passwd"}`)},
			llm.MessageStop{Reason: llm.StopToolUse},
		},
		{
			llm.TextDelta{Text: "done"},
			llm.MessageStop{Reason: llm.StopEndTurn},
		},
	}}
	modifyPolicy := policyFunc(func(_ context.Context, c permissions.ToolCall) (permissions.Decision, error) {
		return permissions.Decision{
			Action:       permissions.Modify,
			ModifiedArgs: json.RawMessage(`{"path":"./safe.txt"}`),
		}, nil
	})

	a := newAgent(t, p, reg, modifyPolicy)
	if _, err := a.Run(context.Background(), "read passwd"); err != nil {
		t.Fatal(err)
	}
	if len(tool.calls) != 1 || string(tool.calls[0]) != `{"path":"./safe.txt"}` {
		t.Fatalf("tool received args=%v, want modified", tool.calls)
	}
}

func TestRunMaxTurnsExceeded(t *testing.T) {
	tool := &stubTool{name: "read", effects: []tools.Effect{tools.EffectReadOnly}, result: tools.Result{Content: "ok"}}
	reg := tools.NewRegistry()
	_ = reg.Register(tool)

	infinite := func() []llm.Event {
		return []llm.Event{
			llm.ToolUseStop{ID: "tu", Name: "read", Input: json.RawMessage(`{}`)},
			llm.MessageStop{Reason: llm.StopToolUse},
		}
	}
	p := &stubProvider{streams: [][]llm.Event{infinite(), infinite(), infinite(), infinite(), infinite()}}
	a, _ := New(Options{Provider: p, Model: "m", Tools: reg, Policy: permissions.AllowAll{}, MaxTurns: 2})
	_, err := a.Run(context.Background(), "loop")
	if err == nil || !errors.Is(err, err) || err.Error() == "" {
		t.Fatalf("want MaxTurns error, got %v", err)
	}
}

func TestParallelToolDispatch(t *testing.T) {
	a1 := &stubTool{name: "a", effects: []tools.Effect{tools.EffectReadOnly}, result: tools.Result{Content: "A"}}
	a2 := &stubTool{name: "b", effects: []tools.Effect{tools.EffectReadOnly}, result: tools.Result{Content: "B"}}
	reg := tools.NewRegistry()
	_ = reg.Register(a1)
	_ = reg.Register(a2)

	p := &stubProvider{streams: [][]llm.Event{
		{
			llm.ToolUseStop{ID: "1", Name: "a", Input: json.RawMessage(`{}`)},
			llm.ToolUseStop{ID: "2", Name: "b", Input: json.RawMessage(`{}`)},
			llm.MessageStop{Reason: llm.StopToolUse},
		},
		{
			llm.TextDelta{Text: "done"},
			llm.MessageStop{Reason: llm.StopEndTurn},
		},
	}}
	a := newAgent(t, p, reg, permissions.AllowAll{})
	res, err := a.Run(context.Background(), "do both")
	if err != nil {
		t.Fatal(err)
	}
	if len(a1.calls) != 1 || len(a2.calls) != 1 {
		t.Fatalf("expected each tool called once; a1=%d a2=%d", len(a1.calls), len(a2.calls))
	}
	// Messages: user, assistant(2 tool_use), user(2 tool_result), assistant(text)
	if len(res.Messages) != 4 {
		t.Fatalf("want 4 messages, got %d", len(res.Messages))
	}
	if len(res.Messages[2].Content) != 2 {
		t.Fatalf("expected 2 tool_result blocks in turn-2 user msg, got %d", len(res.Messages[2].Content))
	}
}

type policyFunc func(context.Context, permissions.ToolCall) (permissions.Decision, error)

func (f policyFunc) Check(ctx context.Context, c permissions.ToolCall) (permissions.Decision, error) {
	return f(ctx, c)
}

func TestInitialMessagesResumedConversation(t *testing.T) {
	p := &stubProvider{streams: [][]llm.Event{{
		llm.TextDelta{Text: "ack"},
		llm.MessageStop{Reason: llm.StopEndTurn},
	}}}
	a, _ := New(Options{
		Provider: p,
		Model:    "m",
		Policy:   permissions.AllowAll{},
		Tools:    tools.NewRegistry(),
		InitialMessages: []llm.Message{
			{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "first"}}},
			{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock{Text: "prior"}}},
		},
	})
	res, err := a.Run(context.Background(), "follow-up")
	if err != nil {
		t.Fatal(err)
	}
	// 2 initial + 1 follow-up user + 1 assistant = 4
	if len(res.Messages) != 4 {
		t.Fatalf("want 4 messages, got %d: %+v", len(res.Messages), res.Messages)
	}
	if tb, ok := res.Messages[0].Content[0].(llm.TextBlock); !ok || tb.Text != "first" {
		t.Fatalf("initial message not preserved at index 0: %+v", res.Messages[0])
	}
}

func TestPreToolHookModifiesArgs(t *testing.T) {
	tool := &stubTool{name: "x", effects: []tools.Effect{tools.EffectReadOnly}, result: tools.Result{Content: "done"}}
	reg := tools.NewRegistry()
	_ = reg.Register(tool)

	p := &stubProvider{streams: [][]llm.Event{
		{
			llm.ToolUseStop{ID: "tu", Name: "x", Input: json.RawMessage(`{"v":1}`)},
			llm.MessageStop{Reason: llm.StopToolUse},
		},
		{
			llm.TextDelta{Text: "ok"},
			llm.MessageStop{Reason: llm.StopEndTurn},
		},
	}}
	a, _ := New(Options{
		Provider: p, Model: "m", Tools: reg, Policy: permissions.AllowAll{},
		PreToolHook: func(_ context.Context, c permissions.ToolCall) (permissions.ToolCall, error) {
			c.Args = json.RawMessage(`{"v":99}`)
			return c, nil
		},
	})
	if _, err := a.Run(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if len(tool.calls) != 1 || string(tool.calls[0]) != `{"v":99}` {
		t.Fatalf("tool received %v, want pre-hook-modified args", tool.calls)
	}
}

func TestPostToolHookTransformsResult(t *testing.T) {
	tool := &stubTool{name: "x", effects: []tools.Effect{tools.EffectReadOnly}, result: tools.Result{Content: "original"}}
	reg := tools.NewRegistry()
	_ = reg.Register(tool)

	p := &stubProvider{streams: [][]llm.Event{
		{
			llm.ToolUseStop{ID: "tu", Name: "x", Input: json.RawMessage(`{}`)},
			llm.MessageStop{Reason: llm.StopToolUse},
		},
		{
			llm.TextDelta{Text: "ok"},
			llm.MessageStop{Reason: llm.StopEndTurn},
		},
	}}
	a, _ := New(Options{
		Provider: p, Model: "m", Tools: reg, Policy: permissions.AllowAll{},
		PostToolHook: func(_ context.Context, _ permissions.ToolCall, r tools.Result) tools.Result {
			r.Content = "redacted"
			return r
		},
	})
	res, err := a.Run(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	// turn-2 user message holds the tool_result block.
	tr, ok := res.Messages[2].Content[0].(llm.ToolResultBlock)
	if !ok || tr.Content != "redacted" {
		t.Fatalf("post-hook didn't transform: %+v", res.Messages[2].Content[0])
	}
}

func TestExitAfterStopsLoop(t *testing.T) {
	tool := &stubTool{name: "exit", effects: []tools.Effect{tools.EffectReadOnly}, result: tools.Result{Content: "submitted", ExitAfter: true}}
	reg := tools.NewRegistry()
	_ = reg.Register(tool)

	// Only one provider stream — if the agent loops again it will hit a
	// "no streams left" error.
	p := &stubProvider{streams: [][]llm.Event{{
		llm.ToolUseStop{ID: "tu", Name: "exit", Input: json.RawMessage(`{}`)},
		llm.MessageStop{Reason: llm.StopToolUse},
	}}}
	a, _ := New(Options{Provider: p, Model: "m", Tools: reg, Policy: permissions.AllowAll{}, MaxTurns: 5})
	res, err := a.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("ExitAfter should terminate cleanly, got %v", err)
	}
	if res.Stop != llm.StopReason("plan_submitted") {
		t.Fatalf("stop = %v, want plan_submitted", res.Stop)
	}
}
