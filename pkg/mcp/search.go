package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/russellhaering/autoswe/pkg/tools"
)

const toolSearchMaxResults = 10

// ToolSearch is a meta-tool that searches the MCP Index by name/description
// and promotes matching tools into the agent's registry so subsequent turns
// can invoke them directly.
type ToolSearch struct {
	index    *Index
	registry *tools.Registry
}

func NewToolSearch(index *Index, registry *tools.Registry) *ToolSearch {
	return &ToolSearch{index: index, registry: registry}
}

func (*ToolSearch) Name() string { return "tool_search" }

func (*ToolSearch) Description() string {
	return "Search the catalog of MCP-server-provided tools by keyword(s). " +
		"Matching tools (up to 10) are loaded into the conversation and become callable on the next turn. " +
		"Each result shows the tool's exposed name, description, and full input schema."
}

func (*ToolSearch) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Space-separated keywords matched against tool names and descriptions."}
  },
  "required": ["query"]
}`)
}

func (*ToolSearch) Effects() []tools.Effect { return []tools.Effect{tools.EffectReadOnly} }

func (s *ToolSearch) Run(_ context.Context, raw json.RawMessage) (tools.Result, error) {
	var a struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("invalid args: %v", err)}, nil
	}
	matches := s.index.Search(a.Query)
	if len(matches) == 0 {
		return tools.Result{Content: "(no matching MCP tools)"}, nil
	}
	if len(matches) > toolSearchMaxResults {
		matches = matches[:toolSearchMaxResults]
	}
	promoted := s.index.Promote(s.registry, matches)

	var b strings.Builder
	for i, m := range matches {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "name: %s\n", m.ExposedName)
		if m.Tool.Description != "" {
			fmt.Fprintf(&b, "description: %s\n", m.Tool.Description)
		}
		fmt.Fprintf(&b, "schema: %s", string(m.Tool.InputSchema))
	}
	if len(promoted) > 0 {
		fmt.Fprintf(&b, "\n\nLoaded into registry: %s", strings.Join(promoted, ", "))
	}
	return tools.Result{Content: b.String()}, nil
}
