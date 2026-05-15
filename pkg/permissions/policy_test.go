package permissions

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/russellhaering/autoswe/pkg/tools"
)

type stubTool struct {
	name    string
	effects []tools.Effect
}

func (s stubTool) Name() string                                                { return s.name }
func (s stubTool) Description() string                                         { return "" }
func (s stubTool) Schema() json.RawMessage                                     { return json.RawMessage(`{"type":"object"}`) }
func (s stubTool) Effects() []tools.Effect                                     { return s.effects }
func (s stubTool) Run(context.Context, json.RawMessage) (tools.Result, error) { return tools.Result{}, nil }

type policyFunc func(context.Context, ToolCall) (Decision, error)

func (f policyFunc) Check(ctx context.Context, c ToolCall) (Decision, error) { return f(ctx, c) }

func TestAllowAll(t *testing.T) {
	d, err := AllowAll{}.Check(context.Background(), ToolCall{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != Allow {
		t.Fatalf("want Allow, got %v", d.Action)
	}
}

func TestDenyAll(t *testing.T) {
	d, err := DenyAll{Reason: "nope"}.Check(context.Background(), ToolCall{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != Deny || d.Reason != "nope" {
		t.Fatalf("unexpected decision: %+v", d)
	}
}

func TestDenyAllDefaultReason(t *testing.T) {
	d, _ := DenyAll{}.Check(context.Background(), ToolCall{Name: "x"})
	if d.Reason == "" {
		t.Fatal("expected non-empty default reason")
	}
}

func TestAllowReadOnly(t *testing.T) {
	cases := []struct {
		name    string
		effects []tools.Effect
		allow   bool
	}{
		{"only_read", []tools.Effect{tools.EffectReadOnly}, true},
		{"only_write", []tools.Effect{tools.EffectFilesystemWrite}, false},
		{"mixed", []tools.Effect{tools.EffectReadOnly, tools.EffectNetwork}, false},
		{"empty_effects", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := AllowReadOnly{}.Check(context.Background(), ToolCall{
				Name: tc.name,
				Tool: stubTool{name: tc.name, effects: tc.effects},
			})
			if err != nil {
				t.Fatal(err)
			}
			got := d.Action == Allow
			if got != tc.allow {
				t.Fatalf("want allow=%v, got decision=%+v", tc.allow, d)
			}
		})
	}
}

func TestAllowReadOnlyNilTool(t *testing.T) {
	d, _ := AllowReadOnly{}.Check(context.Background(), ToolCall{Name: "x"})
	if d.Action != Deny {
		t.Fatalf("nil tool should deny, got %+v", d)
	}
}

func TestAllowlist(t *testing.T) {
	p := NewAllowlist("read", "grep")
	if d, _ := p.Check(context.Background(), ToolCall{Name: "read"}); d.Action != Allow {
		t.Fatalf("read should allow, got %+v", d)
	}
	if d, _ := p.Check(context.Background(), ToolCall{Name: "bash"}); d.Action != Deny {
		t.Fatalf("bash should deny, got %+v", d)
	}
}

func TestChainDenyShortCircuits(t *testing.T) {
	called := false
	track := policyFunc(func(context.Context, ToolCall) (Decision, error) {
		called = true
		return Decision{Action: Allow}, nil
	})
	p := NewChain(DenyAll{Reason: "first"}, track)
	d, _ := p.Check(context.Background(), ToolCall{Name: "x"})
	if d.Action != Deny {
		t.Fatalf("expected deny, got %+v", d)
	}
	if called {
		t.Fatal("downstream policy should not have been consulted after Deny")
	}
}

func TestChainModifyFlowsThrough(t *testing.T) {
	var sawArgs json.RawMessage
	track := policyFunc(func(_ context.Context, c ToolCall) (Decision, error) {
		sawArgs = c.Args
		return Decision{Action: Allow}, nil
	})
	modify := policyFunc(func(context.Context, ToolCall) (Decision, error) {
		return Decision{Action: Modify, ModifiedArgs: json.RawMessage(`{"y":2}`)}, nil
	})
	p := NewChain(modify, track)
	d, _ := p.Check(context.Background(), ToolCall{Name: "x", Args: json.RawMessage(`{"x":1}`)})
	if d.Action != Modify {
		t.Fatalf("expected Modify result, got %+v", d)
	}
	if string(sawArgs) != `{"y":2}` {
		t.Fatalf("downstream policy saw %s, want modified args", sawArgs)
	}
}

func TestChainAllErrorPropagates(t *testing.T) {
	boom := errors.New("boom")
	p := NewChain(policyFunc(func(context.Context, ToolCall) (Decision, error) {
		return Decision{}, boom
	}))
	if _, err := p.Check(context.Background(), ToolCall{}); !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
}

func TestChainAllAllowReturnsAllow(t *testing.T) {
	p := NewChain(AllowAll{}, AllowAll{})
	d, _ := p.Check(context.Background(), ToolCall{Name: "x"})
	if d.Action != Allow {
		t.Fatalf("want Allow, got %+v", d)
	}
}
