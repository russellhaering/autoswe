package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/russellhaering/autoswe/pkg/tools"
)

// Tool is the `skill` meta-tool: list all skills (no args) or fetch a skill's
// body (with `name`).
type Tool struct {
	skills map[string]Skill
}

func NewTool(skills []Skill) *Tool {
	m := make(map[string]Skill, len(skills))
	for _, s := range skills {
		m[s.Name] = s
	}
	return &Tool{skills: m}
}

func (*Tool) Name() string { return "skill" }

func (*Tool) Description() string {
	return "Read a skill's instructions, or list available skills. " +
		"Call with no args (or empty name) to list. Pass `name` to retrieve a skill's body."
}

func (*Tool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {"type": "string", "description": "Skill name (omit to list)."}
  }
}`)
}

func (*Tool) Effects() []tools.Effect { return []tools.Effect{tools.EffectReadOnly} }

func (t *Tool) Run(_ context.Context, raw json.RawMessage) (tools.Result, error) {
	var a struct {
		Name string `json:"name"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return tools.Result{IsError: true, Content: fmt.Sprintf("invalid args: %v", err)}, nil
		}
	}
	if a.Name == "" {
		if len(t.skills) == 0 {
			return tools.Result{Content: "(no skills available)"}, nil
		}
		var b strings.Builder
		fmt.Fprintln(&b, "Available skills:")
		for name, s := range t.skills {
			fmt.Fprintf(&b, "- %s: %s\n", name, s.Description)
		}
		return tools.Result{Content: b.String()}, nil
	}
	s, ok := t.skills[a.Name]
	if !ok {
		return tools.Result{IsError: true, Content: fmt.Sprintf("unknown skill %q", a.Name)}, nil
	}
	return tools.Result{Content: s.Body}, nil
}
