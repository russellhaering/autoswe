package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/russellhaering/autoswe/pkg/tools"
)

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type writeTool struct{}

func (writeTool) Name() string { return "write" }

func (writeTool) Description() string {
	return "Write contents to a file, creating it (and any missing parent directories) or overwriting an existing file. " +
		"Prefer 'edit' for incremental changes to existing files."
}

func (writeTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Path to write. Absolute or working-directory-relative."},
    "content": {"type": "string", "description": "Full file contents. Overwrites any existing file."}
  },
  "required": ["path", "content"]
}`)
}

func (writeTool) Effects() []tools.Effect { return []tools.Effect{tools.EffectFilesystemWrite} }

func (writeTool) Run(_ context.Context, raw json.RawMessage) (tools.Result, error) {
	var a writeArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("invalid args: %v", err)}, nil
	}
	if a.Path == "" {
		return tools.Result{IsError: true, Content: "path is required"}, nil
	}
	if err := os.MkdirAll(filepath.Dir(a.Path), 0o755); err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("mkdir parent of %s: %v", a.Path, err)}, nil
	}
	if err := os.WriteFile(a.Path, []byte(a.Content), 0o644); err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("write %s: %v", a.Path, err)}, nil
	}
	return tools.Result{Content: fmt.Sprintf("wrote %d bytes to %s", len(a.Content), a.Path)}, nil
}

// Write is the built-in write tool.
var Write tools.Tool = writeTool{}
