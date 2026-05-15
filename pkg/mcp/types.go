// Package mcp implements a minimal Model Context Protocol client used to
// surface tools advertised by external MCP servers into our agent's tool
// registry. Only the stdio transport is supported in this phase; an HTTP
// transport stub is reserved for a follow-up.
package mcp

import (
	"context"
	"encoding/json"

	"github.com/russellhaering/autoswe/pkg/tools"
)

// ProtocolVersion is the MCP wire version the client speaks. Servers MUST
// echo the same string in their initialize response or negotiate.
const ProtocolVersion = "2025-06-18"

// Tool describes one MCP-advertised tool plus its annotations.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	Annotations Annotations     `json:"annotations,omitzero"`
}

// Annotations are advisory hints servers attach to tools.
type Annotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

// CallResult is the response from tools/call: a list of content items and an
// optional error flag.
type CallResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ContentItem is one item in a CallResult. MCP supports text/image/resource
// content; we only surface text in Phase 3.
type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Client is the minimal MCP interface the agent depends on. Implementations
// must be safe for concurrent calls.
type Client interface {
	// Name returns the human-readable server name (from config).
	Name() string
	// Initialize performs the initialize handshake.
	Initialize(ctx context.Context) error
	// ListTools returns the server's tools.
	ListTools(ctx context.Context) ([]Tool, error)
	// CallTool invokes a tool with raw JSON arguments.
	CallTool(ctx context.Context, name string, args json.RawMessage) (CallResult, error)
	// Close releases the underlying transport.
	Close() error
}

// EffectsFor maps MCP tool annotations to our internal Effect set. Conservative
// when annotations are absent: a tool with no hints is treated as having
// FilesystemWrite + CodeExecution + Network so a strict policy denies it.
func EffectsFor(a Annotations) []tools.Effect {
	if a.ReadOnlyHint != nil && *a.ReadOnlyHint {
		return []tools.Effect{tools.EffectReadOnly}
	}
	var effects []tools.Effect
	if a.DestructiveHint != nil && *a.DestructiveHint {
		effects = append(effects, tools.EffectFilesystemWrite)
	}
	if a.OpenWorldHint != nil && *a.OpenWorldHint {
		effects = append(effects, tools.EffectNetwork)
	}
	if len(effects) == 0 {
		// No useful hints — treat as opaque/dangerous. Operator opts in via
		// Allowlist or AllowAll if they trust the server.
		return []tools.Effect{
			tools.EffectFilesystemWrite,
			tools.EffectCodeExecution,
			tools.EffectNetwork,
		}
	}
	return effects
}
