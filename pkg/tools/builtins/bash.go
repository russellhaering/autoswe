package builtins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/russellhaering/autoswe/pkg/tools"
)

const (
	defaultBashTimeout = 120 * time.Second
	maxBashTimeout     = 600 * time.Second
	bashOutputBudget   = 200 * 1024 // 200 KiB combined stdout+stderr returned to the model
)

type bashArgs struct {
	Command   string `json:"command"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

type bashTool struct{}

func (bashTool) Name() string { return "bash" }

func (bashTool) Description() string {
	return "Execute a shell command via /bin/sh -c. Captures combined stdout/stderr (truncated if very large). " +
		"Default timeout 120 s; max 600 s."
}

func (bashTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "command": {"type": "string", "description": "Shell command. Executed via /bin/sh -c."},
    "timeout_ms": {"type": "integer", "minimum": 1, "description": "Timeout in milliseconds. Defaults to 120000; capped at 600000."}
  },
  "required": ["command"]
}`)
}

func (bashTool) Effects() []tools.Effect {
	return []tools.Effect{
		tools.EffectCodeExecution,
		tools.EffectFilesystemWrite,
		tools.EffectNetwork,
	}
}

func (bashTool) Run(ctx context.Context, raw json.RawMessage) (tools.Result, error) {
	var a bashArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("invalid args: %v", err)}, nil
	}
	if a.Command == "" {
		return tools.Result{IsError: true, Content: "command is required"}, nil
	}

	timeout := defaultBashTimeout
	if a.TimeoutMS > 0 {
		timeout = min(time.Duration(a.TimeoutMS)*time.Millisecond, maxBashTimeout)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "/bin/sh", "-c", a.Command)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	output := truncate(buf.Bytes(), bashOutputBudget)

	switch {
	case errors.Is(cmdCtx.Err(), context.DeadlineExceeded):
		return tools.Result{
			IsError: true,
			Content: fmt.Sprintf("command timed out after %s\n--- partial output ---\n%s", timeout, output),
		}, nil
	case err != nil:
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			return tools.Result{
				IsError: true,
				Content: fmt.Sprintf("exit %d\n%s", exitErr.ExitCode(), output),
			}, nil
		}
		return tools.Result{IsError: true, Content: fmt.Sprintf("exec error: %v\n%s", err, output)}, nil
	}
	return tools.Result{Content: output}, nil
}

func truncate(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + fmt.Sprintf("\n--- truncated, %d more bytes ---", len(b)-max)
}

// Bash is the built-in bash tool.
var Bash tools.Tool = bashTool{}
