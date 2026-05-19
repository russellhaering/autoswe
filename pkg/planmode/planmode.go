// Package planmode implements plan mode: the agent runs with a read-only-only
// tool set plus an exit_plan_mode tool that submits a markdown plan and
// terminates the loop.
package planmode

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/russellhaering/autoswe/pkg/tools"
	"github.com/russellhaering/autoswe/pkg/tools/builtins"
)

// SystemPromptAddition is appended to the agent's system prompt while in plan
// mode so the model knows not to modify state and to call exit_plan_mode when
// the plan is ready.
const SystemPromptAddition = `

You are in PLAN MODE. Do NOT write/edit files, run shell commands, or otherwise modify state — only read, search, and explore. Develop a complete plan, then call ` + "`exit_plan_mode({plan: \"...\"})`" + ` from a run_script invocation with the plan rendered as markdown. Calling exit_plan_mode terminates the session.`

// ExitPlanMode is the tool that submits a plan and ends the agent loop.
type ExitPlanMode struct{}

func (ExitPlanMode) Name() string { return "exit_plan_mode" }

func (ExitPlanMode) Description() string {
	return "Submit the plan you have developed and end planning mode. The plan must be complete, structured markdown."
}

func (ExitPlanMode) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "plan": {"type": "string", "description": "The complete implementation plan in markdown."}
  },
  "required": ["plan"]
}`)
}

func (ExitPlanMode) Effects() []tools.Effect { return []tools.Effect{tools.EffectReadOnly} }

func (ExitPlanMode) Run(_ context.Context, raw json.RawMessage) (tools.Result, error) {
	var a struct {
		Plan string `json:"plan"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("invalid args: %v", err)}, nil
	}
	if a.Plan == "" {
		return tools.Result{IsError: true, Content: "plan is required"}, nil
	}
	return tools.Result{
		Content:   "Plan submitted. Planning mode ended.",
		Metadata:  json.RawMessage(`{"autoswe_plan_submitted":true}`),
		ExitAfter: true,
	}, nil
}

// Registry returns a registry containing only read-only built-ins (read, glob,
// grep) plus exit_plan_mode. Extra tools (e.g. an MCP tool_search) can be
// added afterwards via Register, subject to the same read-only constraint
// applied by the caller (typically via an AllowReadOnly policy).
func Registry() *tools.Registry {
	r := tools.NewRegistry()
	for _, t := range builtins.All() {
		if !allReadOnly(t.Effects()) {
			continue
		}
		r.MustRegister(t)
	}
	r.MustRegister(ExitPlanMode{})
	return r
}

func allReadOnly(effects []tools.Effect) bool {
	for _, e := range effects {
		if e != tools.EffectReadOnly {
			return false
		}
	}
	return true
}
