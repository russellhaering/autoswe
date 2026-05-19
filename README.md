# autoswe

A headless coding agent: Go library and CLI, multi-provider (Anthropic, OpenAI, Anthropic-on-Bedrock).

## Features

- **Providers:** Anthropic Messages API, OpenAI Chat Completions, Amazon Bedrock Converse — same canonical message/tool shape behind `pkg/llm.Provider`.
- **Built-in tools:** `read`, `write`, `edit`, `bash`, `glob`, `grep`, `web_fetch`, with `Effect` metadata (read-only / filesystem-write / code-execution / network) that policies use to gate execution.
- **Web search (Anthropic only, always on):** the Anthropic provider unconditionally advertises Claude's `web_search_20250305` server tool; Claude decides when to use it and results come back inline with citations. No toggle (see `CLAUDE.md`). OpenAI's Chat Completions API doesn't surface web search as a plain tool — that's a follow-up via the Responses API. Bedrock has no native web search.
- **Permissions:** composable policies — `AllowAll`, `DenyAll`, `AllowReadOnly`, `Allowlist`, `Chain`, `Remembered` (in-session cache), `Interactive` (TTY prompter). Decisions can `Allow`, `Deny` (reason surfaced to model), or `Modify` (rewrite args before tool runs).
- **MCP:** stdio MCP servers via `--mcp-config`; a dynamic `tool_search` meta-tool indexes server-advertised tools and promotes matches into the registry on demand. MCP annotations (`readOnlyHint`, `destructiveHint`, `openWorldHint`) map to our `Effect` set so the same policies gate them.
- **Skills:** markdown-with-frontmatter loaded from `./.autoswe/skills` and `~/.autoswe/skills`; exposed via a `skill` meta-tool.
- **Plan mode:** `--plan` swaps in a read-only-only registry plus an `exit_plan_mode` tool that submits a markdown plan and ends the run.
- **Code mode (non-optional):** the only model-visible tool is `run_script`. The LLM writes JS that calls registered tools as host functions (e.g. `await read({path: "README.md"})`); only `console.log` output and the final expression flow back. Amortizes many tool round-trips into one, à la Cloudflare's Code Mode / pi.dev `ctx_execute`. Backed by [`fastschema/qjs`](https://github.com/fastschema/qjs) (pure-Go QuickJS via Wazero, no CGO). Sandbox bounds configurable via `--script-timeout` and `--script-mem-mib`. Inner host calls re-run the configured policy so `--deny-write`/`--deny-exec`/`Interactive` still gate per-tool.
- **Sessions:** every run saves a JSONL transcript under `~/.autoswe/sessions/<id>.jsonl`; resume with `--resume <id>`.
- **Tokens & config:** API tokens stored in the OS keychain via `autoswe login` (env vars still take precedence). Optional `~/.autoswe/config.toml` for non-secret defaults (provider, model, script bounds, log level). See [Tokens & config](#tokens--config) below.
- **Hooks:** `Options.PreToolHook` / `PostToolHook` for logging/transforming tool calls and results.

## CLI

```
autoswe login --provider anthropic    # paste token; stored in OS keychain
autoswe whoami                        # check which providers have tokens (never echoes them)
autoswe logout --provider anthropic

autoswe -p "summarize README.md"      # uses keychain or env
autoswe --provider openai --model gpt-5 -p "…"
AWS_REGION=us-west-2 autoswe --provider bedrock -p "…"

autoswe --plan -p "design a feature for …"
autoswe -p "list every .go file under pkg and report their sizes"   # one run_script turn
autoswe --mcp-config ./mcp.json -p "use the filesystem MCP to list things"
autoswe --interactive --deny-write -p "investigate the codebase"
autoswe -p "first request"           # prints "[session sess_XXXX saved …]"
autoswe --resume sess_XXXX -p "now…" # continues from prior history
autoswe --json -p "…"                # JSONL event stream on stdout
```

Logs go to stderr; output to stdout. See `autoswe -h` for the full flag set.

## Tokens & config

API tokens live in the OS keychain (macOS Keychain, Linux Secret Service, Windows Credential Manager) via [`zalando/go-keyring`](https://github.com/zalando/go-keyring). The lookup order for each provider is:

1. **Env var** (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`) — for CI/CD or one-off sessions.
2. **Keychain** entry under service `autoswe`, account = provider name.
3. Otherwise an error pointing you to `autoswe login --provider <name>`.

Bedrock uses the AWS SDK credential chain instead (env / shared config / IAM role).

Non-secret defaults live in `~/.autoswe/config.toml` (TOML; created on demand). Flags always override config. Example:

```toml
[defaults]
provider       = "anthropic"
max_turns      = 25
script_timeout = "60s"
script_mem_mib = 64
log_level      = "info"

[provider.anthropic]
model = "claude-opus-4-7"

[provider.openai]
model = "gpt-5"

[provider.bedrock]
region = "us-west-2"
```

### Security notes

- Tokens are encrypted at rest by the OS keychain and require an unlocked user session to read.
- On macOS, `go-keyring` shells out to `/usr/bin/security`, so the keychain item is accessible to any process the logged-in user can run — there is **no per-binary ACL**. This is on par with `gh auth login`, `aws-vault`, etc. Stronger per-binary ACL (CGO into `Security.framework` + a code-signed binary with a stable identity) is a deliberate follow-up.
- Env vars still work and take precedence. CI/CD workflows are unchanged.

## Library

```go
import (
    "context"
    "os"

    "github.com/russellhaering/autoswe/pkg/agent"
    "github.com/russellhaering/autoswe/pkg/llm/anthropic"
    "github.com/russellhaering/autoswe/pkg/permissions"
    "github.com/russellhaering/autoswe/pkg/tools/builtins"
)

a, err := agent.New(agent.Options{
    Provider: anthropic.New(anthropic.Config{APIKey: os.Getenv("ANTHROPIC_API_KEY")}),
    Model:    "claude-sonnet-4-5",
    Tools:    builtins.DefaultRegistry(),
    Policy:   permissions.AllowAll{},
})
if err != nil { /* … */ }
res, err := a.Run(context.Background(), "summarize README")
```

`agent.RunStream` returns `(<-chan agent.Event, *agent.Result)` for streaming UIs — drain the channel, then read `*Result` for the final messages/usage.

## Package layout

```
cmd/autoswe/         CLI
pkg/agent/           Core loop + Run/RunStream + hooks
pkg/llm/             Canonical types + Provider interface
  anthropic/         Messages API (HTTP+SSE)
  openai/            Chat Completions (HTTP+SSE)
  bedrock/           Converse (AWS SDK)
pkg/tools/           Tool/Registry/Effect
  builtins/          read/write/edit/bash/glob/grep
pkg/permissions/     Policy + Allow/Deny/AllowReadOnly/Allowlist/Chain/Remembered/Interactive
pkg/mcp/             stdio JSON-RPC client + tool_search meta-tool
pkg/skills/          Discovery + skill meta-tool
pkg/planmode/        exit_plan_mode tool + read-only registry helper
pkg/codemode/        run_script meta-tool (QuickJS sandbox via fastschema/qjs)
pkg/session/         JSONL transcript persistence
pkg/config/          ~/.autoswe/config.toml load/save
pkg/secrets/         OS keychain wrapper (zalando/go-keyring)
internal/tui/        Bubble Tea TUI
```

## Status

Phases 1–5 of the original plan are implemented, plus a Bubble Tea TUI and code-mode. Notable follow-ups still open:

- MCP HTTP transport (only stdio works today; `url:` entries in the config error with a clear message).
- End-to-end stdio MCP test against a real fixture server.
- OpenAI web search: requires migrating the OpenAI provider from Chat Completions to the Responses API. Currently web search is Anthropic-only.
