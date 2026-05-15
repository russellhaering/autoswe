// Package tools defines the Tool interface every built-in or wrapped tool
// satisfies, along with a Registry that exposes registered tools to the agent
// loop and to LLM providers.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/russellhaering/autoswe/pkg/llm"
)

// Effect describes a side-effect class a tool may produce. Policies use these
// to gate execution (e.g. AllowReadOnly only permits tools whose effects are
// all EffectReadOnly).
type Effect int

const (
	EffectReadOnly Effect = iota
	EffectFilesystemWrite
	EffectCodeExecution
	EffectNetwork
)

func (e Effect) String() string {
	switch e {
	case EffectReadOnly:
		return "read_only"
	case EffectFilesystemWrite:
		return "filesystem_write"
	case EffectCodeExecution:
		return "code_execution"
	case EffectNetwork:
		return "network"
	default:
		return fmt.Sprintf("Effect(%d)", int(e))
	}
}

// Result is what a tool returns to the agent. Content is the text surfaced to
// the model in the tool_result block; IsError flips the is_error flag (for
// Anthropic) or signals an error result to other providers.
//
// ExitAfter signals the agent loop to terminate after this turn's tool
// results are appended to the conversation — used by plan mode's
// exit_plan_mode tool. The terminating tool's tool_result is still appended;
// the loop simply does not call the provider again.
type Result struct {
	Content   string
	IsError   bool
	Metadata  json.RawMessage
	ExitAfter bool
}

// Tool is implemented by every built-in tool and by MCP/source-adapter wrappers.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Effects() []Effect
	Run(ctx context.Context, args json.RawMessage) (Result, error)
}

// Registry is a name-keyed, concurrency-safe collection of Tools.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

var ErrDuplicateTool = errors.New("tool already registered")

func (r *Registry) Register(t Tool) error {
	if t == nil {
		return errors.New("nil tool")
	}
	name := t.Name()
	if name == "" {
		return errors.New("tool has empty name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; ok {
		return fmt.Errorf("%w: %q", ErrDuplicateTool, name)
	}
	r.tools[name] = t
	return nil
}

// MustRegister panics on error. Convenience for package init.
func (r *Registry) MustRegister(t Tool) {
	if err := r.Register(t); err != nil {
		panic(err)
	}
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Specs returns the provider-facing ToolSpec list, sorted by name for stable
// request shapes.
func (r *Registry) Specs() []llm.ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	specs := make([]llm.ToolSpec, 0, len(names))
	for _, n := range names {
		t := r.tools[n]
		specs = append(specs, llm.ToolSpec{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.Schema(),
		})
	}
	return specs
}

// Filter returns a new Registry containing only the named tools that exist in
// r. Missing names are silently ignored — the caller checks Names afterwards
// if it wants to validate.
func (r *Registry) Filter(allowed ...string) *Registry {
	allow := make(map[string]struct{}, len(allowed))
	for _, n := range allowed {
		allow[n] = struct{}{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	f := NewRegistry()
	for n, t := range r.tools {
		if _, ok := allow[n]; ok {
			f.tools[n] = t
		}
	}
	return f
}
