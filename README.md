# autoswe

A headless coding agent: Go library and CLI, multi-provider (Anthropic, OpenAI, Anthropic-on-Bedrock).

## Features

- **Providers:** Anthropic Messages API, OpenAI Chat Completions, Amazon Bedrock Converse — same canonical message/tool shape behind `pkg/llm.Provider`.
- **Built-in tools:** `read`, `write`, `edit`, `bash`, `glob`, `grep`, with `Effect` metadata (read-only / filesystem-write / code-execution / network) that policies use to gate execution.
- **Permissions:** composable policies — `AllowAll`, `DenyAll`, `AllowReadOnly`, `Allowlist`, `Chain`, `Remembered` (in-session cache), `Interactive` (TTY prompter). Decisions can `Allow`, `Deny` (reason surfaced to model), or `Modify` (rewrite args before tool runs).
- **MCP:** stdio MCP servers via `--mcp-config`; a dynamic `tool_search` meta-tool indexes server-advertised tools and promotes matches into the registry on demand. MCP annotations (`readOnlyHint`, `destructiveHint`, `openWorldHint`) map to our `Effect` set so the same policies gate them.
- **Skills:** markdown-with-frontmatter loaded from `./.autoswe/skills` and `~/.autoswe/skills`; exposed via a `skill` meta-tool.
- **Plan mode:** `--plan` swaps in a read-only-only registry plus an `exit_plan_mode` tool that submits a markdown plan and ends the run.
- **Sessions:** every run saves a JSONL transcript under `~/.autoswe/sessions/<id>.jsonl`; resume with `--resume <id>`.
- **Hooks:** `Options.PreToolHook` / `PostToolHook` for logging/transforming tool calls and results.

## CLI

```
autoswe -p "summarize README.md"
ANTHROPIC_API_KEY=…  autoswe -p "…"
OPENAI_API_KEY=…     autoswe --provider openai --model gpt-5 -p "…"
AWS_REGION=us-west-2 autoswe --provider bedrock -p "…"

autoswe --plan -p "design a feature for …"
autoswe --mcp-config ./mcp.json -p "use the filesystem MCP to list things"
autoswe --interactive --deny-write -p "investigate the codebase"
autoswe -p "first request"           # prints "[session sess_XXXX saved …]"
autoswe --resume sess_XXXX -p "now…" # continues from prior history
autoswe --json -p "…"                # JSONL event stream on stdout
```

Logs go to stderr; output to stdout. See `autoswe -h` for the full flag set.

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
pkg/session/         JSONL transcript persistence
```

## Status

Phases 1–5 of the original plan are implemented. Notable follow-ups still open:

- MCP HTTP transport (only stdio works today; `url:` entries in the config error with a clear message).
- End-to-end stdio MCP test against a real fixture server.
- "Code-mode" tool execution (model writes a small script that calls tools in a sandbox) — a deliberate future direction, not in scope.
