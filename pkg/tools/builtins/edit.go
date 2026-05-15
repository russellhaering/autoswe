package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/russellhaering/autoswe/pkg/tools"
)

type editArgs struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

type editTool struct{}

func (editTool) Name() string { return "edit" }

func (editTool) Description() string {
	return "Perform an exact-string replacement in an existing file. " +
		"old_string must match exactly once unless replace_all=true. " +
		"Use this rather than 'write' for incremental changes."
}

func (editTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Path to the file to edit."},
    "old_string": {"type": "string", "description": "Exact text to find."},
    "new_string": {"type": "string", "description": "Replacement text."},
    "replace_all": {"type": "boolean", "description": "If true, replace every occurrence; otherwise old_string must match exactly once. Defaults to false."}
  },
  "required": ["path", "old_string", "new_string"]
}`)
}

func (editTool) Effects() []tools.Effect { return []tools.Effect{tools.EffectFilesystemWrite} }

func (editTool) Run(_ context.Context, raw json.RawMessage) (tools.Result, error) {
	var a editArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("invalid args: %v", err)}, nil
	}
	if a.Path == "" {
		return tools.Result{IsError: true, Content: "path is required"}, nil
	}
	if a.OldString == a.NewString {
		return tools.Result{IsError: true, Content: "old_string and new_string are identical"}, nil
	}

	data, err := os.ReadFile(a.Path)
	if err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("read %s: %v", a.Path, err)}, nil
	}
	content := string(data)

	count := strings.Count(content, a.OldString)
	if count == 0 {
		return tools.Result{IsError: true, Content: fmt.Sprintf("old_string not found in %s", a.Path)}, nil
	}
	if !a.ReplaceAll && count > 1 {
		return tools.Result{IsError: true, Content: fmt.Sprintf("old_string matches %d times in %s; set replace_all=true or include more context", count, a.Path)}, nil
	}

	var updated string
	if a.ReplaceAll {
		updated = strings.ReplaceAll(content, a.OldString, a.NewString)
	} else {
		updated = strings.Replace(content, a.OldString, a.NewString, 1)
	}
	if err := os.WriteFile(a.Path, []byte(updated), 0o644); err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("write %s: %v", a.Path, err)}, nil
	}
	return tools.Result{Content: fmt.Sprintf("replaced %d occurrence(s) in %s", count, a.Path)}, nil
}

// Edit is the built-in edit tool.
var Edit tools.Tool = editTool{}
