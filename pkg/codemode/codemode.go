// Package codemode implements a "run_script" meta-tool: the model emits one
// JavaScript snippet that calls registered tools as host-provided functions
// (e.g. `await read({path: "README.md"})`); the snippet runs in an embedded
// QuickJS sandbox and the captured stdout flows back as one tool_result.
// This amortizes many tool round-trips into a single one, following the
// Cloudflare "Code Mode" and pi.dev `ctx_execute` design.
package codemode

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/russellhaering/autoswe/pkg/permissions"
	"github.com/russellhaering/autoswe/pkg/tools"
)

// ToolName is the meta-tool's registered name. Scripts cannot recursively
// invoke it (skipped from the binding set).
const ToolName = "run_script"

// DefaultMaxExecution and DefaultMemoryMiB are the limits applied per script
// invocation unless overridden via Limits.
const (
	DefaultMaxExecution = 60 * time.Second
	DefaultMemoryMiB    = 64
	// DefaultMaxToolsInDecl caps how many tools we describe in the meta-tool's
	// Description() — when 100+ MCP tools are loaded the system prompt would
	// otherwise balloon. Sorted-by-name and truncation-marked.
	DefaultMaxToolsInDecl = 50
)

// Limits configures the per-call sandbox bounds.
type Limits struct {
	MaxExecution      time.Duration
	MemoryMiB         uint64
	MaxToolsInDecl    int
}

func (l Limits) withDefaults() Limits {
	if l.MaxExecution <= 0 {
		l.MaxExecution = DefaultMaxExecution
	}
	if l.MemoryMiB == 0 {
		l.MemoryMiB = DefaultMemoryMiB
	}
	if l.MaxToolsInDecl == 0 {
		l.MaxToolsInDecl = DefaultMaxToolsInDecl
	}
	return l
}

// binding is one registered tool bound into the JS global namespace.
type binding struct {
	jsName   string
	toolName string
	tool     tools.Tool
}

// RunScript implements tools.Tool. It is constructed against a registry +
// policy + logger and computes its TS API surface lazily on the first
// Description() call.
type RunScript struct {
	registry *tools.Registry
	policy   permissions.Policy
	logger   *slog.Logger
	limits   Limits

	bindings []binding
	tsDecl   string
	desc     string
}

// New builds a RunScript meta-tool. The registry passed here is the source
// of truth for what scripts can call; the meta-tool itself is excluded from
// its own binding set. Pass the same Policy the agent uses so in-script
// host calls remain subject to --deny-write/--deny-exec/Interactive prompts.
func New(reg *tools.Registry, pol permissions.Policy, lim Limits, log *slog.Logger) *RunScript {
	if log == nil {
		log = slog.Default()
	}
	if pol == nil {
		pol = permissions.AllowAll{}
	}
	r := &RunScript{
		registry: reg,
		policy:   pol,
		logger:   log,
		limits:   lim.withDefaults(),
	}
	r.computeBindings()
	r.tsDecl = r.renderTSSurface()
	r.desc = r.renderDescription()
	return r
}

// computeBindings walks the registry and produces one binding per tool with
// a valid JS identifier name. Conflicts (rare; shouldn't happen with current
// built-ins) skip the second tool and log a warning.
func (r *RunScript) computeBindings() {
	names := r.registry.Names()
	sort.Strings(names)
	seen := map[string]struct{}{}
	for _, n := range names {
		if n == ToolName {
			continue
		}
		if !isValidJSIdent(n) {
			r.logger.Debug("codemode: skipping tool with non-identifier name", "tool", n)
			continue
		}
		if _, dup := seen[n]; dup {
			r.logger.Warn("codemode: skipping duplicate binding", "tool", n)
			continue
		}
		t, ok := r.registry.Get(n)
		if !ok {
			continue
		}
		seen[n] = struct{}{}
		r.bindings = append(r.bindings, binding{jsName: n, toolName: n, tool: t})
	}
}

func (r *RunScript) renderTSSurface() string {
	max := r.limits.MaxToolsInDecl
	var b strings.Builder
	count := 0
	for _, bind := range r.bindings {
		if count >= max {
			fmt.Fprintf(&b, "// … %d more tool(s) omitted; refine your task or call them by name to discover the schema\n",
				len(r.bindings)-count)
			break
		}
		schema := bind.tool.Schema()
		fmt.Fprintln(&b, toTSDeclaration(bind.jsName, bind.tool.Description(), schema))
		count++
	}
	return b.String()
}

func (r *RunScript) renderDescription() string {
	const header = "Run a JavaScript snippet that calls registered tools as host functions, instead of emitting one tool_use per call.\n\n" +
		"Use this to execute JavaScript code that can call multiple tools in sequence. Note: The tools listed below are ONLY available within JavaScript - they cannot be invoked directly as separate tool calls.\n\n" +
		"Rules:\n" +
		"- The script supports top-level await; each bound tool returns a Promise<string>.\n" +
		"- Use try/catch — denied or failing tool calls throw a JS Error whose message is the deny reason or tool error.\n" +
		"- Print results with console.log; only its output flows back to the conversation. The script's final expression value is appended too if non-undefined.\n" +
		"- No fetch, no network, no filesystem except via the bound tools. No recursion into run_script.\n\n" +
		"Available tools (JavaScript/TypeScript only - these are NOT directly invokable, use only within this script):\n\n"
	return header + r.tsDecl
}

// Name returns the meta-tool's registered name.
func (r *RunScript) Name() string { return ToolName }

// Description returns the static rules block followed by the TS API surface
// for every bound tool.
func (r *RunScript) Description() string { return r.desc }

// Schema returns the input schema: a single required `script` string.
func (r *RunScript) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "script": {
      "type": "string",
      "description": "JavaScript source. Wrapped in an async IIFE before execution; use top-level await."
    }
  },
  "required": ["script"]
}`)
}

// Effects reports ReadOnly — the QuickJS sandbox itself can only compute;
// it has no filesystem, network, or process access except via bound host
// functions. Every host call re-runs the configured Policy with that
// tool's own effects, so AllowReadOnly / --deny-exec / --deny-write still
// gate dangerous operations at the inner-call boundary.
func (r *RunScript) Effects() []tools.Effect {
	return []tools.Effect{tools.EffectReadOnly}
}

// Run unmarshals the script arg and executes it in a fresh QuickJS runtime.
func (r *RunScript) Run(ctx context.Context, args json.RawMessage) (tools.Result, error) {
	var in struct {
		Script string `json:"script"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("invalid args: %v", err)}, nil
	}
	if strings.TrimSpace(in.Script) == "" {
		return tools.Result{IsError: true, Content: "empty script"}, nil
	}
	return r.execute(ctx, in.Script), nil
}
