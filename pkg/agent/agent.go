// Package agent implements the headless coding agent's core loop: given a
// user prompt, a provider, a tool registry, and a permission policy, it
// drives turns of LLM inference and tool dispatch until the model stops.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/russellhaering/autoswe/pkg/llm"
	"github.com/russellhaering/autoswe/pkg/permissions"
	"github.com/russellhaering/autoswe/pkg/tools"
)

// Options configures a new Agent. Provider is required; everything else has a
// sensible default.
type Options struct {
	Provider    llm.Provider
	Model       string
	System      string
	Tools       *tools.Registry
	Policy      permissions.Policy
	MaxTurns    int     // 0 = unlimited
	MaxTokens   int     // per-call cap; defaults to 4096
	Temperature float64 // 0 = unset
	Logger      *slog.Logger

	// InitialMessages, if set, is prepended to the conversation history
	// before the user's input. Used by --resume to continue a prior session.
	InitialMessages []llm.Message

	// PreToolHook is invoked once per tool call before policy + execution.
	// It may return an error to abort the run, or a possibly-modified
	// ToolCall whose Args are used downstream.
	PreToolHook func(ctx context.Context, call permissions.ToolCall) (permissions.ToolCall, error)
	// PostToolHook is invoked once per tool call after execution (or after a
	// synthetic deny result), with the final Result for inspection or
	// transformation. It may not abort the run; errors from it are logged
	// but otherwise ignored.
	PostToolHook func(ctx context.Context, call permissions.ToolCall, result tools.Result) tools.Result
}

type Agent struct {
	opts Options
}

func New(opts Options) (*Agent, error) {
	if opts.Provider == nil {
		return nil, errors.New("agent: Provider is required")
	}
	if opts.Model == "" {
		return nil, errors.New("agent: Model is required")
	}
	if opts.Tools == nil {
		opts.Tools = tools.NewRegistry()
	}
	if opts.Policy == nil {
		opts.Policy = permissions.AllowAll{}
	}
	if opts.MaxTokens == 0 {
		opts.MaxTokens = 4096
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Agent{opts: opts}, nil
}

// Result is what Run returns once the agent stops.
type Result struct {
	Text     string         // concatenated text from the final assistant turn
	Stop     llm.StopReason // reason the model stopped
	Usage    llm.Usage      // accumulated across all turns
	Messages []llm.Message  // full conversation history
}

// Event is the sealed union of streaming events RunStream emits.
type Event interface {
	isAgentEvent()
}

// TextDelta is a partial text chunk from the assistant's current turn.
type TextDelta struct {
	Text string
}

func (TextDelta) isAgentEvent() {}

// ToolUse fires once the assistant's tool_use block is fully assembled,
// before the tool runs.
type ToolUse struct {
	ID    string
	Name  string
	Input json.RawMessage
}

func (ToolUse) isAgentEvent() {}

// ToolResult fires after the tool runs (or is denied) and the result is ready
// to be appended back into the conversation for the model.
type ToolResult struct {
	ToolUseID string
	Name      string
	Content   string
	IsError   bool
}

func (ToolResult) isAgentEvent() {}

// Stop fires when the agent terminates normally (any stop reason that is not
// tool_use).
type Stop struct {
	Reason llm.StopReason
	Usage  llm.Usage
}

func (Stop) isAgentEvent() {}

// Error fires on a fatal error. The channel is closed after this event.
type Error struct {
	Err error
}

func (Error) isAgentEvent() {}

// Run drives the loop to completion and returns the final Result.
func (a *Agent) Run(ctx context.Context, input string) (Result, error) {
	return a.run(ctx, input, func(Event) {})
}

// RunStream drives the loop and emits events as they happen. The returned
// *Result is populated by the time the events channel closes; callers must
// drain the channel before reading it.
func (a *Agent) RunStream(ctx context.Context, input string) (<-chan Event, *Result) {
	ch := make(chan Event, 16)
	result := &Result{}
	go func() {
		defer close(ch)
		emit := func(ev Event) {
			select {
			case <-ctx.Done():
			case ch <- ev:
			}
		}
		r, err := a.run(ctx, input, emit)
		*result = r
		if err != nil {
			emit(Error{Err: err})
		}
	}()
	return ch, result
}
