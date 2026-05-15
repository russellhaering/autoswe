# autoswe

A headless coding agent: Go library and CLI, multi-provider (Anthropic, OpenAI, Anthropic-on-Bedrock).

## CLI

```
autoswe -p "summarize README.md"
ANTHROPIC_API_KEY=… autoswe -p "…"
OPENAI_API_KEY=… autoswe --provider openai --model gpt-5 -p "…"
```

See `autoswe -h` for the full flag set. Logs go to stderr; output goes to stdout (plain text by default, JSONL with `--json`).

## Library

```go
import (
    "context"
    "os"

    "github.com/russellhaering/autoswe/pkg/agent"
    "github.com/russellhaering/autoswe/pkg/llm/anthropic"
    "github.com/russellhaering/autoswe/pkg/permissions"
    "github.com/russellhaering/autoswe/pkg/tools"
)

a := agent.New(agent.Options{
    Provider: anthropic.New(anthropic.Config{APIKey: os.Getenv("ANTHROPIC_API_KEY")}),
    Model:    "claude-sonnet-4-6",
    Tools:    tools.DefaultRegistry(),
    Policy:   permissions.AllowAll{},
})
res, err := a.Run(context.Background(), "summarize README")
```

## Status

Phase 1 — minimum viable agent: provider abstraction (Anthropic + OpenAI), agent loop, core built-in tools, permission policies. Bedrock, MCP, skills, plan mode, sessions, and hooks land in subsequent phases.
