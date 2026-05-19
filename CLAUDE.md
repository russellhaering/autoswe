# autoswe — design principles

A short list of the opinions that shape this codebase. When in doubt, default to *fewer knobs*.

## Minimize configuration surface

Configuration is a tax on the user and the codebase. We add a flag/option **only when it stands in for a real, irreducible constraint** — usually something external we can't normalize over. We do *not* add flags for "what if someone wanted to turn this off."

**Things that justify configuration** (and why):

- Provider selection (Anthropic / OpenAI / Bedrock) — different APIs, different credentials.
- Per-provider model — provider model menus differ.
- Plan mode (`--plan`) — distinct behavior, not just a tool gate.
- Resume by session id (`--resume`) — references prior state.
- MCP server config (`--mcp-config`) — external resource location.
- Script sandbox bounds (`--script-timeout`, `--script-mem-mib`) — operational safety knob for the rare bad case.

**Things we deliberately do not expose** (and why):

- A `--code-mode` switch. Code-mode is *how* the agent works, not a feature flag.
- A `--web-search` switch. Web search is always on (provider-native where available). Costs accrue against the existing API key; that's fine.
- A toggle to disable streaming, alt-screen TUI, session saving, etc. The right thing is the right thing.
- Per-tool enable/disable flags. The agent gets the tools it gets. The legacy `--allowed-tools` flag is a smell; future removal.

When you catch yourself reaching for a flag, first ask: is there an external constraint making this necessary, or am I just hedging? If it's hedging, ship the better default and let people complain if it's wrong.

## Other rules of thumb

- **No dead toggles.** If a flag exists, the off path must do something useful. Otherwise delete the flag.
- **Library API == CLI API discipline.** Same principle applies in `pkg/agent.Options`: only fields with a real reason. Don't add `EnableX bool` because someone might want to turn off X.
- **Provider-native first.** Where a provider exposes a capability (web search, structured output, prompt caching), prefer the native path over a hand-rolled abstraction. Cross-provider abstraction comes after we have it working on each one.
- **Backwards-compat doesn't override design.** `autoswe` is pre-1.0; we break interfaces when the new shape is better.
