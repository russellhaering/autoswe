package permissions

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestInteractiveResponses(t *testing.T) {
	cases := []struct {
		resp     PromptResponse
		wantAct  DecisionAction
		wantRem  bool
		wantErr  error
	}{
		{PromptAllowOnce, Allow, false, nil},
		{PromptAllowAlways, Allow, true, nil},
		{PromptDenyOnce, Deny, false, nil},
		{PromptDenyAlways, Deny, true, nil},
		{PromptAbort, 0, false, ErrUserAborted},
	}
	for _, tc := range cases {
		i := Interactive{Prompt: func(context.Context, ToolCall) (PromptResponse, error) { return tc.resp, nil }}
		d, err := i.Check(context.Background(), ToolCall{Name: "x"})
		if !errors.Is(err, tc.wantErr) {
			t.Fatalf("resp=%v: err=%v want %v", tc.resp, err, tc.wantErr)
		}
		if err != nil {
			continue
		}
		if d.Action != tc.wantAct || d.Remember != tc.wantRem {
			t.Fatalf("resp=%v: got %+v", tc.resp, d)
		}
	}
}

func TestTTYPrompterParsing(t *testing.T) {
	cases := map[string]PromptResponse{
		"y":  PromptAllowOnce,
		"":   PromptAllowOnce,
		"a":  PromptAllowAlways,
		"n":  PromptDenyOnce,
		"d":  PromptDenyAlways,
		"q":  PromptAbort,
		"?":  PromptDenyOnce, // unrecognized → deny
	}
	for input, want := range cases {
		out := &strings.Builder{}
		prompt := TTYPrompter(out, strings.NewReader(input+"\n"))
		resp, err := prompt(context.Background(), ToolCall{Name: "tool"})
		if err != nil {
			t.Fatalf("input=%q: %v", input, err)
		}
		if resp != want {
			t.Fatalf("input=%q: got %v want %v", input, resp, want)
		}
	}
}

func TestRemembered(t *testing.T) {
	calls := 0
	inner := policyFunc(func(context.Context, ToolCall) (Decision, error) {
		calls++
		return Decision{Action: Allow, Remember: true}, nil
	})
	r := NewRemembered(inner)

	for i := 0; i < 3; i++ {
		d, err := r.Check(context.Background(), ToolCall{Name: "x"})
		if err != nil || d.Action != Allow {
			t.Fatalf("iter %d: %+v %v", i, d, err)
		}
	}
	if calls != 1 {
		t.Fatalf("inner should be consulted once, got %d", calls)
	}

	// Decisions without Remember are not cached.
	calls = 0
	transient := policyFunc(func(context.Context, ToolCall) (Decision, error) {
		calls++
		return Decision{Action: Allow}, nil
	})
	r2 := NewRemembered(transient)
	for i := 0; i < 3; i++ {
		_, _ = r2.Check(context.Background(), ToolCall{Name: "y"})
	}
	if calls != 3 {
		t.Fatalf("transient policy: want 3 invocations, got %d", calls)
	}

	// Forget evicts.
	r.Forget("x")
	calls2 := 0
	inner2 := policyFunc(func(context.Context, ToolCall) (Decision, error) {
		calls2++
		return Decision{Action: Allow, Remember: true}, nil
	})
	r3 := NewRemembered(inner2)
	_, _ = r3.Check(context.Background(), ToolCall{Name: "z"})
	r3.Forget("z")
	_, _ = r3.Check(context.Background(), ToolCall{Name: "z"})
	if calls2 != 2 {
		t.Fatalf("after Forget, inner should be called again; got %d", calls2)
	}
}
