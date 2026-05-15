package builtins

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/russellhaering/autoswe/pkg/tools"
)

const defaultGrepMaxResults = 200

var grepSkipDirs = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	".idea":        {},
	".vscode":      {},
	"vendor":       {},
}

type grepArgs struct {
	Pattern         string `json:"pattern"`
	Path            string `json:"path,omitempty"`
	Glob            string `json:"glob,omitempty"`
	CaseInsensitive bool   `json:"case_insensitive,omitempty"`
	MaxResults      int    `json:"max_results,omitempty"`
}

type grepMatch struct {
	Path string
	Line int
	Text string
}

type grepTool struct{}

func (grepTool) Name() string { return "grep" }

func (grepTool) Description() string {
	return "Search file contents for a regular expression. Walks 'path' (default '.') skipping common ignore " +
		"directories. 'glob' restricts the files searched. Returns 'path:line:text' lines."
}

func (grepTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "Regular expression (RE2 syntax)."},
    "path": {"type": "string", "description": "Search root. Defaults to working directory."},
    "glob": {"type": "string", "description": "Glob to filter files to search (e.g. '**/*.go')."},
    "case_insensitive": {"type": "boolean", "description": "Case-insensitive match. Defaults to false."},
    "max_results": {"type": "integer", "minimum": 1, "description": "Max matching lines to return. Defaults to 200."}
  },
  "required": ["pattern"]
}`)
}

func (grepTool) Effects() []tools.Effect { return []tools.Effect{tools.EffectReadOnly} }

func (grepTool) Run(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
	var a grepArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("invalid args: %v", err)}, nil
	}
	if a.Pattern == "" {
		return tools.Result{IsError: true, Content: "pattern is required"}, nil
	}
	pattern := a.Pattern
	if a.CaseInsensitive {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("invalid regexp: %v", err)}, nil
	}
	base := a.Path
	if base == "" {
		base = "."
	}
	limit := a.MaxResults
	if limit == 0 {
		limit = defaultGrepMaxResults
	}

	var matches []grepMatch
	truncated := false

	errStop := errors.New("limit reached")
	walkErr := filepath.WalkDir(base, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			if _, skip := grepSkipDirs[d.Name()]; skip && path != base {
				return fs.SkipDir
			}
			return nil
		}
		if a.Glob != "" {
			rel, _ := filepath.Rel(base, path)
			ok, _ := doublestar.Match(a.Glob, rel)
			if !ok {
				return nil
			}
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			if !re.MatchString(line) {
				continue
			}
			matches = append(matches, grepMatch{Path: path, Line: lineNo, Text: line})
			if len(matches) >= limit {
				truncated = true
				return errStop
			}
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, errStop) && !errors.Is(walkErr, ctx.Err()) {
		return tools.Result{IsError: true, Content: fmt.Sprintf("walk error: %v", walkErr)}, nil
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return tools.Result{IsError: true, Content: ctx.Err().Error()}, nil
	}

	if len(matches) == 0 {
		return tools.Result{Content: "(no matches)"}, nil
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Path != matches[j].Path {
			return matches[i].Path < matches[j].Path
		}
		return matches[i].Line < matches[j].Line
	})
	var b strings.Builder
	for _, m := range matches {
		fmt.Fprintf(&b, "%s:%d:%s\n", m.Path, m.Line, m.Text)
	}
	if truncated {
		fmt.Fprintf(&b, "--- truncated at %d matches ---\n", limit)
	}
	return tools.Result{Content: b.String()}, nil
}

// Grep is the built-in grep tool.
var Grep tools.Tool = grepTool{}
