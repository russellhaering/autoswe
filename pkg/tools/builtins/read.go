package builtins

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/russellhaering/autoswe/pkg/tools"
)

const defaultReadLimit = 2000

type readArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type readTool struct{}

func (readTool) Name() string { return "read" }

func (readTool) Description() string {
	return "Read a file from disk. Returns the file contents with 1-indexed line numbers prefixed to each line. " +
		"Use offset/limit to read a window of a large file."
}

func (readTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Path to the file. Absolute or relative to the working directory."},
    "offset": {"type": "integer", "minimum": 1, "description": "1-indexed line to start reading from. Defaults to 1."},
    "limit": {"type": "integer", "minimum": 1, "description": "Maximum number of lines to read. Defaults to 2000."}
  },
  "required": ["path"]
}`)
}

func (readTool) Effects() []tools.Effect { return []tools.Effect{tools.EffectReadOnly} }

func (readTool) Run(_ context.Context, raw json.RawMessage) (tools.Result, error) {
	var a readArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("invalid args: %v", err)}, nil
	}
	if a.Path == "" {
		return tools.Result{IsError: true, Content: "path is required"}, nil
	}
	if a.Offset == 0 {
		a.Offset = 1
	}
	if a.Limit == 0 {
		a.Limit = defaultReadLimit
	}

	f, err := os.Open(a.Path)
	if err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("open %s: %v", a.Path, err)}, nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var b strings.Builder
	lineNo := 0
	emitted := 0
	for scanner.Scan() {
		lineNo++
		if lineNo < a.Offset {
			continue
		}
		if emitted >= a.Limit {
			break
		}
		fmt.Fprintf(&b, "%6d\t%s\n", lineNo, scanner.Text())
		emitted++
	}
	if err := scanner.Err(); err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("read %s: %v", a.Path, err)}, nil
	}
	if emitted == 0 {
		return tools.Result{Content: fmt.Sprintf("(no lines in range %d..%d for %s)", a.Offset, a.Offset+a.Limit-1, a.Path)}, nil
	}
	return tools.Result{Content: b.String()}, nil
}

// Read is the built-in read tool.
var Read tools.Tool = readTool{}
