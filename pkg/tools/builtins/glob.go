package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/russellhaering/autoswe/pkg/tools"
)

const globMaxResults = 1000

type globArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

type globTool struct{}

func (globTool) Name() string { return "glob" }

func (globTool) Description() string {
	return "Find files matching a glob pattern. Supports ** for recursive matches " +
		"(e.g. 'pkg/**/*.go'). Path is the search root (default '.')."
}

func (globTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "Glob pattern. ** is supported for recursive matches."},
    "path": {"type": "string", "description": "Search root. Defaults to the working directory."}
  },
  "required": ["pattern"]
}`)
}

func (globTool) Effects() []tools.Effect { return []tools.Effect{tools.EffectReadOnly} }

func (globTool) Run(_ context.Context, raw json.RawMessage) (tools.Result, error) {
	var a globArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("invalid args: %v", err)}, nil
	}
	if a.Pattern == "" {
		return tools.Result{IsError: true, Content: "pattern is required"}, nil
	}
	base := a.Path
	if base == "" {
		base = "."
	}
	matches, err := doublestar.Glob(os.DirFS(base), a.Pattern, doublestar.WithFilesOnly())
	if err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("glob error: %v", err)}, nil
	}
	if len(matches) == 0 {
		return tools.Result{Content: "(no matches)"}, nil
	}
	sort.Strings(matches)
	truncatedNote := ""
	if len(matches) > globMaxResults {
		truncatedNote = fmt.Sprintf("\n--- truncated, %d more matches ---", len(matches)-globMaxResults)
		matches = matches[:globMaxResults]
	}
	for i, m := range matches {
		matches[i] = filepath.Join(base, m)
	}
	return tools.Result{Content: strings.Join(matches, "\n") + truncatedNote}, nil
}

// Glob is the built-in glob tool.
var Glob tools.Tool = globTool{}
