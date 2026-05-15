// Package permissions defines the Policy abstraction the agent loop consults
// before every tool invocation, plus a handful of composable built-in policies.
package permissions

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/russellhaering/autoswe/pkg/tools"
)

// ToolCall is the input a Policy sees when deciding whether to permit a tool
// invocation.
type ToolCall struct {
	Name string
	Args json.RawMessage
	Tool tools.Tool
}

type DecisionAction int

const (
	Allow DecisionAction = iota
	Deny
	Modify
)

func (a DecisionAction) String() string {
	switch a {
	case Allow:
		return "allow"
	case Deny:
		return "deny"
	case Modify:
		return "modify"
	default:
		return fmt.Sprintf("DecisionAction(%d)", int(a))
	}
}

// Decision is what a Policy returns. On Modify, ModifiedArgs replaces the
// args passed to the tool; the assistant message in conversation history still
// shows the model's original input. On Deny, Reason is surfaced to the model
// as the tool_result text so it can recover.
type Decision struct {
	Action       DecisionAction
	ModifiedArgs json.RawMessage
	Reason       string
}

type Policy interface {
	Check(ctx context.Context, call ToolCall) (Decision, error)
}

// AllowAll permits every call. Default in CLI; library callers should pick a
// tighter policy in production.
type AllowAll struct{}

func (AllowAll) Check(context.Context, ToolCall) (Decision, error) {
	return Decision{Action: Allow}, nil
}

// DenyAll refuses every call with a configurable Reason (defaulted if empty).
type DenyAll struct {
	Reason string
}

func (d DenyAll) Check(context.Context, ToolCall) (Decision, error) {
	reason := d.Reason
	if reason == "" {
		reason = "denied by policy"
	}
	return Decision{Action: Deny, Reason: reason}, nil
}

// AllowReadOnly permits tools whose Effects() are all EffectReadOnly. Tools
// with no declared effects are also permitted (treated as read-only).
type AllowReadOnly struct{}

func (AllowReadOnly) Check(_ context.Context, call ToolCall) (Decision, error) {
	if call.Tool == nil {
		return Decision{Action: Deny, Reason: "tool not found"}, nil
	}
	for _, e := range call.Tool.Effects() {
		if e != tools.EffectReadOnly {
			return Decision{
				Action: Deny,
				Reason: fmt.Sprintf("tool %q has %s effect; AllowReadOnly only permits read_only", call.Name, e),
			}, nil
		}
	}
	return Decision{Action: Allow}, nil
}

// Allowlist permits only the named tools.
type Allowlist struct {
	names map[string]struct{}
}

func NewAllowlist(names ...string) Allowlist {
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		m[n] = struct{}{}
	}
	return Allowlist{names: m}
}

func (a Allowlist) Check(_ context.Context, call ToolCall) (Decision, error) {
	if _, ok := a.names[call.Name]; ok {
		return Decision{Action: Allow}, nil
	}
	return Decision{Action: Deny, Reason: fmt.Sprintf("tool %q not in allowlist", call.Name)}, nil
}

// Chain composes policies. Each policy sees the args the prior policy left
// behind (so Modify accumulates). A Deny short-circuits the chain; an error
// short-circuits with the error. If any policy returned Modify, the chain's
// final Decision is the last Modify (with the accumulated ModifiedArgs);
// otherwise it is Allow.
type Chain struct {
	Policies []Policy
}

func NewChain(p ...Policy) Chain {
	return Chain{Policies: p}
}

func (c Chain) Check(ctx context.Context, call ToolCall) (Decision, error) {
	var last *Decision
	for _, p := range c.Policies {
		d, err := p.Check(ctx, call)
		if err != nil {
			return Decision{}, err
		}
		switch d.Action {
		case Deny:
			return d, nil
		case Modify:
			call.Args = d.ModifiedArgs
			d := d
			last = &d
		case Allow:
			// continue
		}
	}
	if last != nil {
		return *last, nil
	}
	return Decision{Action: Allow}, nil
}
