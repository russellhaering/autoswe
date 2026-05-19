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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/russellhaering/autoswe/internal/tui"
	"github.com/russellhaering/autoswe/pkg/agent"
	"github.com/russellhaering/autoswe/pkg/codemode"
	"github.com/russellhaering/autoswe/pkg/config"
	"github.com/russellhaering/autoswe/pkg/secrets"
	"github.com/russellhaering/autoswe/pkg/llm"
	"github.com/russellhaering/autoswe/pkg/llm/anthropic"
	"github.com/russellhaering/autoswe/pkg/llm/bedrock"
	"github.com/russellhaering/autoswe/pkg/llm/openai"
	"github.com/russellhaering/autoswe/pkg/mcp"
	"github.com/russellhaering/autoswe/pkg/permissions"
	"github.com/russellhaering/autoswe/pkg/planmode"
	"github.com/russellhaering/autoswe/pkg/session"
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
	interactive  bool
	maxTurns     int
	jsonOutput   bool
	logLevel     string

	plan      bool
	resume    string
	mcpConfig string

	scriptTimeout time.Duration
	scriptMemMiB  uint
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "login", "logout", "whoami":
			if err := runSubcommand(os.Args[1], os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
			return
		}
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	opts, userSet := parseFlags()

	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		// Surface as warning but continue with built-in defaults.
		fmt.Fprintln(os.Stderr, "warning:", cfgErr)
		cfg = &config.Config{}
	}
	applyConfigDefaults(&opts, cfg, userSet)

	if err := rejectUnimplemented(opts); err != nil {
		return err
	}

	level, err := parseLogLevel(opts.logLevel)
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	input, tuiMode, err := readInput(opts.prompt)
	if err != nil {
		return err
	}

	logTarget, logCleanup, err := openLogTarget(tuiMode, opts.resume)
	if err != nil {
		return err
	}
	defer logCleanup()
	slog.SetDefault(slog.New(slog.NewTextHandler(logTarget, &slog.HandlerOptions{Level: level})))

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

	// Code-mode is non-optional: the only model-visible tool is run_script,
	// which exposes everything in `registry` as host functions inside a
	// QuickJS sandbox. This amortizes chained tool calls into one round-trip
	// (à la Cloudflare Code Mode / pi.dev ctx_execute) and lets the same
	// Policy gate every inner call.
	cm := codemode.New(registry, policy, codemode.Limits{
		MaxExecution: opts.scriptTimeout,
		MemoryMiB:    uint64(opts.scriptMemMiB),
	}, slog.Default())
	wrapped := tools.NewRegistry()
	if err := wrapped.Register(cm); err != nil {
		return fmt.Errorf("register run_script: %w", err)
	}
	registry = wrapped

	sessionDir, err := session.DefaultDir()
	if err != nil {
		return err
	}
	sessionID := opts.resume
	var initial []llm.Message
	if sessionID != "" {
		s, err := session.Load(sessionDir, sessionID)
		if err != nil {
			return err
		}
		initial = s.Messages
		slog.InfoContext(ctx, "resumed session", "id", sessionID, "messages", len(initial))
	} else {
		sessionID = session.NewID()
	}

	agentOpts := agent.Options{
		Provider:        provider,
		Model:           model,
		System:          system,
		Tools:           registry,
		Policy:          policy,
		MaxTurns:        opts.maxTurns,
		InitialMessages: initial,
	}

	if tuiMode {
		return tui.Run(ctx, tui.Config{
			AgentOptions: agentOpts,
			SessionDir:   sessionDir,
			SessionID:    sessionID,
		})
	}

	a, err := agent.New(agentOpts)
	if err != nil {
		return err
	}

	events, result := a.RunStream(ctx, input)
	var streamErr error
	if opts.jsonOutput {
		streamErr = streamJSON(events, os.Stdout, os.Stderr)
	} else {
		streamErr = streamPlain(events, os.Stdout, os.Stderr)
	}

	if len(result.Messages) > 0 {
		if err := session.Save(sessionDir, sessionID, result.Messages); err != nil {
			slog.WarnContext(ctx, "session save failed", "id", sessionID, "err", err)
		} else {
			fmt.Fprintf(os.Stderr, "[session %s saved to %s]\n", sessionID, session.PathFor(sessionDir, sessionID))
		}
	}
	return streamErr
}

// openLogTarget picks where slog output goes. In TUI mode it writes to
// ~/.autoswe/logs/<session>.log so the alt-screen UI isn't trashed; in
// CLI mode it stays on stderr.
func openLogTarget(tuiMode bool, sessionHint string) (io.Writer, func(), error) {
	if !tuiMode {
		return os.Stderr, func() {}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return io.Discard, func() {}, nil
	}
	dir := filepath.Join(home, ".autoswe", "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return io.Discard, func() {}, nil
	}
	name := sessionHint
	if name == "" {
		name = "tui"
	}
	f, err := os.OpenFile(filepath.Join(dir, name+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return io.Discard, func() {}, nil
	}
	return f, func() { _ = f.Close() }, nil
}

// parseFlags returns the parsed options plus the set of flag names that
// the user passed explicitly. applyConfigDefaults uses the set so that
// config-supplied defaults only apply where the user didn't pass a flag.
func parseFlags() (cliOptions, map[string]bool) {
	var o cliOptions
	flag.StringVar(&o.prompt, "p", "", "one-shot prompt; if empty and stdin is piped, the prompt is read from stdin")
	flag.StringVar(&o.provider, "provider", "anthropic", "LLM provider: anthropic, openai, or bedrock")
	flag.StringVar(&o.model, "model", "", "model id (defaults to a provider-specific Sonnet/GPT-5 model if unset)")
	flag.StringVar(&o.system, "system", defaultSystemPrompt, "system prompt")
	flag.StringVar(&o.allowedTools, "allowed-tools", "", "comma-separated list of built-in tools to enable (default: all)")
	flag.BoolVar(&o.denyWrite, "deny-write", false, "deny all filesystem-write tools")
	flag.BoolVar(&o.denyExec, "deny-exec", false, "deny all code-execution tools")
	flag.BoolVar(&o.interactive, "interactive", false, "prompt on stderr/stdin for every tool call (cached for the session)")
	flag.IntVar(&o.maxTurns, "max-turns", 50, "max assistant turns before the agent aborts")
	flag.BoolVar(&o.jsonOutput, "json", false, "emit JSONL event stream on stdout instead of plain text")
	flag.StringVar(&o.logLevel, "log-level", "info", "log level: debug, info, warn, error")

	flag.BoolVar(&o.plan, "plan", false, "plan mode: restrict to read-only tools; exit_plan_mode submits a structured plan")
	flag.StringVar(&o.resume, "resume", "", "resume a prior session by id (~/.autoswe/sessions/<id>.jsonl)")
	flag.StringVar(&o.mcpConfig, "mcp-config", "", "path to an MCP server config JSON file (mcpServers map)")

	flag.DurationVar(&o.scriptTimeout, "script-timeout", codemode.DefaultMaxExecution, "max execution time for a single run_script call")
	flag.UintVar(&o.scriptMemMiB, "script-mem-mib", codemode.DefaultMemoryMiB, "memory limit for the run_script sandbox, in MiB")

	flag.Parse()
	userSet := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { userSet[f.Name] = true })
	return o, userSet
}

func rejectUnimplemented(_ cliOptions) error {
	return nil
}

// applyConfigDefaults overlays config-supplied defaults onto opts wherever
// the user did not explicitly set that flag. Precedence: explicit flag >
// config > flag's built-in default (already in opts).
func applyConfigDefaults(opts *cliOptions, cfg *config.Config, userSet map[string]bool) {
	if cfg == nil {
		return
	}
	d := cfg.Defaults
	if !userSet["provider"] && d.Provider != "" {
		opts.provider = d.Provider
	}
	if !userSet["model"] {
		if p := cfg.ProviderOf(opts.provider).Model; p != "" {
			opts.model = p
		}
	}
	if !userSet["max-turns"] && d.MaxTurns > 0 {
		opts.maxTurns = d.MaxTurns
	}
	if !userSet["log-level"] && d.LogLevel != "" {
		opts.logLevel = d.LogLevel
	}
	if !userSet["script-timeout"] && d.ScriptTimeout > 0 {
		opts.scriptTimeout = d.ScriptTimeout
	}
	if !userSet["script-mem-mib"] && d.ScriptMemMiB > 0 {
		opts.scriptMemMiB = d.ScriptMemMiB
	}
}

// readInput resolves the one-shot prompt. It returns tuiMode=true only when
// -p is unset and both stdin and stdout are TTYs — meaning the user invoked
// autoswe interactively. Piped stdin is read as the prompt; an empty pipe
// (or no input + redirected stdio) is a usage error rather than a TUI launch.
func readInput(prompt string) (string, bool, error) {
	if prompt != "" {
		return prompt, false, nil
	}
	stdinTTY := term.IsTerminal(int(os.Stdin.Fd()))
	stdoutTTY := term.IsTerminal(int(os.Stdout.Fd()))
	if stdinTTY && stdoutTTY {
		return "", true, nil
	}
	if !stdinTTY {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", false, fmt.Errorf("read stdin: %w", err)
		}
		s := strings.TrimRight(string(b), "\n")
		if s != "" {
			return s, false, nil
		}
	}
	return "", false, errors.New("no prompt provided (use -p, pipe via stdin, or invoke without args in a terminal for the TUI)")
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

// resolveToken returns the API token for a provider, preferring an env var
// over the OS keychain so CI/CD workflows are unchanged. Returns ("", nil)
// on a clean miss; backend errors are surfaced.
func resolveToken(provider, envVar string) (string, error) {
	if v := os.Getenv(envVar); v != "" {
		return v, nil
	}
	tok, ok, err := secrets.Get(provider)
	if err != nil {
		return "", fmt.Errorf("keychain lookup for %s: %w", provider, err)
	}
	if ok {
		return tok, nil
	}
	return "", nil
}

func selectProvider(ctx context.Context, name, model string) (llm.Provider, string, error) {
	switch name {
	case "anthropic":
		key, err := resolveToken("anthropic", "ANTHROPIC_API_KEY")
		if err != nil {
			return nil, "", err
		}
		if key == "" {
			return nil, "", errors.New("no anthropic API key: set ANTHROPIC_API_KEY or run `autoswe login --provider anthropic`")
		}
		if model == "" {
			model = anthropic.DefaultModel
		}
		return anthropic.New(anthropic.Config{APIKey: key}), model, nil
	case "openai":
		key, err := resolveToken("openai", "OPENAI_API_KEY")
		if err != nil {
			return nil, "", err
		}
		if key == "" {
			return nil, "", errors.New("no openai API key: set OPENAI_API_KEY or run `autoswe login --provider openai`")
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
	var policies []permissions.Policy
	if o.denyWrite || o.denyExec {
		denied := map[tools.Effect]struct{}{}
		if o.denyWrite {
			denied[tools.EffectFilesystemWrite] = struct{}{}
		}
		if o.denyExec {
			denied[tools.EffectCodeExecution] = struct{}{}
		}
		policies = append(policies, effectDenyPolicy{denied: denied})
	}
	if o.interactive {
		policies = append(policies, permissions.NewRemembered(permissions.Interactive{
			Prompt: permissions.TTYPrompter(os.Stderr, os.Stdin),
		}))
	}
	if len(policies) == 0 {
		return permissions.AllowAll{}
	}
	return permissions.NewChain(policies...)
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
		case agent.ServerToolUse:
			_, _ = fmt.Fprintf(stderr, "⟳ %s/%s%s\n", e.Provider, e.Name, summarizeServerInput(e.Raw))
		case agent.Stop:
			_, _ = fmt.Fprintln(stdout)
			_, _ = fmt.Fprintf(stderr, "[stop reason=%s in=%d out=%d]\n", e.Reason, e.Usage.InputTokens, e.Usage.OutputTokens)
		case agent.Error:
			return e.Err
		}
	}
	return nil
}

// summarizeServerInput extracts the `input` field from a server_tool_use
// block so we can show "web_search(query: foo)" rather than the whole
// raw JSON.
func summarizeServerInput(raw json.RawMessage) string {
	var m struct {
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &m); err != nil || len(m.Input) == 0 {
		return ""
	}
	return summarizeArgs(m.Input)
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
		case agent.ServerToolUse:
			payload = map[string]any{"type": "server_tool_use", "provider": e.Provider, "name": e.Name, "raw": json.RawMessage(e.Raw)}
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
