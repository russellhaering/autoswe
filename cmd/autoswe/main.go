package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/russellhaering/autoswe/pkg/agent"
	"github.com/russellhaering/autoswe/pkg/llm"
	"github.com/russellhaering/autoswe/pkg/llm/anthropic"
	"github.com/russellhaering/autoswe/pkg/llm/bedrock"
	"github.com/russellhaering/autoswe/pkg/llm/openai"
	"github.com/russellhaering/autoswe/pkg/mcp"
	"github.com/russellhaering/autoswe/pkg/permissions"
	"github.com/russellhaering/autoswe/pkg/planmode"
	"github.com/russellhaering/autoswe/pkg/skills"
	"github.com/russellhaering/autoswe/pkg/tools"
	"github.com/russellhaering/autoswe/pkg/tools/builtins"
)

const defaultSystemPrompt = `You are autoswe, a headless coding agent.

You have access to tools to read, write, and edit files, run shell commands, and search code. Use them to complete the user's task. Be terse — output what's needed and stop. When the task is done, give a short summary and stop.`

type cliOptions struct {
	prompt       string
	provider     string
	model        string
	system       string
	allowedTools string
	denyWrite    bool
	denyExec     bool
	maxTurns     int
	jsonOutput   bool
	logLevel     string

	plan      bool
	resume    string
	mcpConfig string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	opts := parseFlags()

	if err := rejectUnimplemented(opts); err != nil {
		return err
	}

	level, err := parseLogLevel(opts.logLevel)
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	input, err := readInput(opts.prompt)
	if err != nil {
		return err
	}
	if input == "" {
		return errors.New("no prompt provided (use -p or pipe via stdin)")
	}

	provider, model, err := selectProvider(ctx, opts.provider, opts.model)
	if err != nil {
		return err
	}

	var registry *tools.Registry
	system := opts.system
	if opts.plan {
		registry = planmode.Registry()
		system += planmode.SystemPromptAddition
	} else {
		registry = builtins.DefaultRegistry()
	}

	if opts.allowedTools != "" {
		names := splitCSV(opts.allowedTools)
		registry = registry.Filter(names...)
		if len(registry.Names()) == 0 {
			return fmt.Errorf("no built-in tools matched --allowed-tools=%q", opts.allowedTools)
		}
	}

	loadedSkills, err := skills.Load(skills.DefaultDirs())
	if err != nil {
		return err
	}
	if len(loadedSkills) > 0 {
		if err := registry.Register(skills.NewTool(loadedSkills)); err != nil {
			return err
		}
		system += skills.SystemPromptAddition(loadedSkills)
	}

	if opts.mcpConfig != "" {
		idx, err := loadMCP(ctx, opts.mcpConfig, registry)
		if err != nil {
			return err
		}
		defer idx.Close()
	}

	policy := buildPolicy(opts, registry)

	a, err := agent.New(agent.Options{
		Provider: provider,
		Model:    model,
		System:   system,
		Tools:    registry,
		Policy:   policy,
		MaxTurns: opts.maxTurns,
	})
	if err != nil {
		return err
	}

	events := a.RunStream(ctx, input)
	if opts.jsonOutput {
		return streamJSON(events, os.Stdout, os.Stderr)
	}
	return streamPlain(events, os.Stdout, os.Stderr)
}

func parseFlags() cliOptions {
	var o cliOptions
	flag.StringVar(&o.prompt, "p", "", "one-shot prompt; if empty and stdin is piped, the prompt is read from stdin")
	flag.StringVar(&o.provider, "provider", "anthropic", "LLM provider: anthropic, openai, or bedrock")
	flag.StringVar(&o.model, "model", "", "model id (defaults to a provider-specific Sonnet/GPT-5 model if unset)")
	flag.StringVar(&o.system, "system", defaultSystemPrompt, "system prompt")
	flag.StringVar(&o.allowedTools, "allowed-tools", "", "comma-separated list of built-in tools to enable (default: all)")
	flag.BoolVar(&o.denyWrite, "deny-write", false, "deny all filesystem-write tools")
	flag.BoolVar(&o.denyExec, "deny-exec", false, "deny all code-execution tools")
	flag.IntVar(&o.maxTurns, "max-turns", 50, "max assistant turns before the agent aborts")
	flag.BoolVar(&o.jsonOutput, "json", false, "emit JSONL event stream on stdout instead of plain text")
	flag.StringVar(&o.logLevel, "log-level", "info", "log level: debug, info, warn, error")

	flag.BoolVar(&o.plan, "plan", false, "plan mode: restrict to read-only tools; exit_plan_mode submits a structured plan")
	flag.StringVar(&o.resume, "resume", "", "(reserved for Phase 5) resume a prior session by id")
	flag.StringVar(&o.mcpConfig, "mcp-config", "", "path to an MCP server config JSON file (mcpServers map)")

	flag.Parse()
	return o
}

func rejectUnimplemented(o cliOptions) error {
	if o.resume != "" {
		return errors.New("--resume is not yet implemented (Phase 5)")
	}
	return nil
}

func readInput(prompt string) (string, error) {
	if prompt != "" {
		return prompt, nil
	}
	info, err := os.Stdin.Stat()
	if err != nil {
		return "", fmt.Errorf("stat stdin: %w", err)
	}
	if info.Mode()&os.ModeCharDevice != 0 {
		return "", nil
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	return strings.TrimRight(string(b), "\n"), nil
}

func parseLogLevel(s string) (slog.Level, error) {
	switch s {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level: %q", s)
	}
}

func selectProvider(ctx context.Context, name, model string) (llm.Provider, string, error) {
	switch name {
	case "anthropic":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, "", errors.New("ANTHROPIC_API_KEY is required for --provider=anthropic")
		}
		if model == "" {
			model = anthropic.DefaultModel
		}
		return anthropic.New(anthropic.Config{APIKey: key}), model, nil
	case "openai":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, "", errors.New("OPENAI_API_KEY is required for --provider=openai")
		}
		if model == "" {
			model = openai.DefaultModel
		}
		return openai.New(openai.Config{
			APIKey:       key,
			Organization: os.Getenv("OPENAI_ORG_ID"),
			Project:      os.Getenv("OPENAI_PROJECT_ID"),
		}), model, nil
	case "bedrock":
		if model == "" {
			model = bedrock.DefaultModel
		}
		p, err := bedrock.New(ctx, bedrock.Config{
			Region:  os.Getenv("AWS_REGION"),
			Profile: os.Getenv("AWS_PROFILE"),
		})
		if err != nil {
			return nil, "", err
		}
		return p, model, nil
	default:
		return nil, "", fmt.Errorf("unknown provider %q (want anthropic, openai, or bedrock)", name)
	}
}

func buildPolicy(o cliOptions, _ *tools.Registry) permissions.Policy {
	if !o.denyWrite && !o.denyExec {
		return permissions.AllowAll{}
	}
	denied := map[tools.Effect]struct{}{}
	if o.denyWrite {
		denied[tools.EffectFilesystemWrite] = struct{}{}
	}
	if o.denyExec {
		denied[tools.EffectCodeExecution] = struct{}{}
	}
	return effectDenyPolicy{denied: denied}
}

// effectDenyPolicy denies any tool whose effects intersect the configured set.
type effectDenyPolicy struct {
	denied map[tools.Effect]struct{}
}

func (e effectDenyPolicy) Check(_ context.Context, call permissions.ToolCall) (permissions.Decision, error) {
	if call.Tool == nil {
		return permissions.Decision{Action: permissions.Allow}, nil
	}
	for _, eff := range call.Tool.Effects() {
		if _, ok := e.denied[eff]; ok {
			return permissions.Decision{
				Action: permissions.Deny,
				Reason: fmt.Sprintf("tool %q has %s effect and is denied by policy flag", call.Name, eff),
			}, nil
		}
	}
	return permissions.Decision{Action: permissions.Allow}, nil
}

// loadMCP reads the MCP server config at path, builds the cross-server tool
// index, and registers a tool_search meta-tool into reg. Returns the index so
// the caller can Close() it on shutdown.
func loadMCP(ctx context.Context, path string, reg *tools.Registry) (*mcp.Index, error) {
	cfg, err := mcp.LoadConfigFile(path)
	if err != nil {
		return nil, err
	}
	idx, idxErr := mcp.NewIndex(ctx, cfg, true)
	if idxErr != nil {
		slog.WarnContext(ctx, "mcp partial init", "err", idxErr)
	}
	if err := reg.Register(mcp.NewToolSearch(idx, reg)); err != nil {
		return nil, fmt.Errorf("register tool_search: %w", err)
	}
	return idx, nil
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// streamPlain prints text deltas to stdout and tool activity to stderr.
func streamPlain(events <-chan agent.Event, stdout, stderr io.Writer) error {
	for ev := range events {
		switch e := ev.(type) {
		case agent.TextDelta:
			_, _ = fmt.Fprint(stdout, e.Text)
		case agent.ToolUse:
			_, _ = fmt.Fprintf(stderr, "→ %s%s\n", e.Name, summarizeArgs(e.Input))
		case agent.ToolResult:
			if e.IsError {
				_, _ = fmt.Fprintf(stderr, "← %s (error): %s\n", e.Name, oneLine(e.Content))
			} else {
				_, _ = fmt.Fprintf(stderr, "← %s: %d bytes\n", e.Name, len(e.Content))
			}
		case agent.Stop:
			_, _ = fmt.Fprintln(stdout)
			_, _ = fmt.Fprintf(stderr, "[stop reason=%s in=%d out=%d]\n", e.Reason, e.Usage.InputTokens, e.Usage.OutputTokens)
		case agent.Error:
			return e.Err
		}
	}
	return nil
}

// streamJSON emits one JSON object per line on stdout.
func streamJSON(events <-chan agent.Event, stdout, _ io.Writer) error {
	enc := json.NewEncoder(stdout)
	for ev := range events {
		var payload any
		switch e := ev.(type) {
		case agent.TextDelta:
			payload = map[string]any{"type": "text", "text": e.Text}
		case agent.ToolUse:
			payload = map[string]any{"type": "tool_use", "id": e.ID, "name": e.Name, "input": json.RawMessage(e.Input)}
		case agent.ToolResult:
			payload = map[string]any{"type": "tool_result", "id": e.ToolUseID, "name": e.Name, "content": e.Content, "is_error": e.IsError}
		case agent.Stop:
			payload = map[string]any{"type": "stop", "reason": string(e.Reason), "usage": map[string]int{"input_tokens": e.Usage.InputTokens, "output_tokens": e.Usage.OutputTokens}}
		case agent.Error:
			payload = map[string]any{"type": "error", "message": e.Err.Error()}
			_ = enc.Encode(payload)
			return e.Err
		default:
			continue
		}
		if err := enc.Encode(payload); err != nil {
			return err
		}
	}
	return nil
}

func summarizeArgs(b []byte) string {
	const max = 80
	s := string(b)
	if len(s) > max {
		return "(" + s[:max] + "…)"
	}
	return "(" + s + ")"
}

func oneLine(s string) string {
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return before + "…"
	}
	return s
}
