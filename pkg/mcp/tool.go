package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/russellhaering/autoswe/pkg/tools"
)

// mcpTool adapts an MCP-advertised tool to the tools.Tool interface.
type mcpTool struct {
	client      Client
	name        string
	exposedName string
	description string
	schema      json.RawMessage
	effects     []tools.Effect
}

// NewTool wraps a discovered MCP Tool as a tools.Tool. If serverPrefix is
// true, the exposed name is "<server>__<tool>" to avoid collisions across
// multiple servers.
func NewTool(client Client, t Tool, serverPrefix bool) tools.Tool {
	exposed := t.Name
	if serverPrefix && client.Name() != "" {
		exposed = client.Name() + "__" + t.Name
	}
	return &mcpTool{
		client:      client,
		name:        t.Name,
		exposedName: exposed,
		description: t.Description,
		schema:      ensureSchema(t.InputSchema),
		effects:     EffectsFor(t.Annotations),
	}
}

func ensureSchema(s json.RawMessage) json.RawMessage {
	s = json.RawMessage(strings.TrimSpace(string(s)))
	if len(s) == 0 {
		return json.RawMessage(`{"type":"object"}`)
	}
	return s
}

func (m *mcpTool) Name() string                 { return m.exposedName }
func (m *mcpTool) Description() string          { return m.description }
func (m *mcpTool) Schema() json.RawMessage      { return m.schema }
func (m *mcpTool) Effects() []tools.Effect      { return m.effects }

func (m *mcpTool) Run(ctx context.Context, args json.RawMessage) (tools.Result, error) {
	res, err := m.client.CallTool(ctx, m.name, args)
	if err != nil {
		return tools.Result{IsError: true, Content: err.Error()}, nil
	}
	var b strings.Builder
	for i, item := range res.Content {
		if i > 0 {
			b.WriteString("\n")
		}
		switch item.Type {
		case "text", "":
			b.WriteString(item.Text)
		default:
			fmt.Fprintf(&b, "(non-text content of type %q omitted)", item.Type)
		}
	}
	return tools.Result{Content: b.String(), IsError: res.IsError}, nil
}
