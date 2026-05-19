package anthropic

import (
	"encoding/json"
	"fmt"

	"github.com/russellhaering/autoswe/pkg/llm"
)

// ProviderID is the value we stamp into ServerToolBlock.Provider for blocks
// originating from this package, so other providers' translators know not
// to emit them.
const ProviderID = "anthropic"

// WebSearchToolType is the server-tool spec type we add to every request.
// The newer web_search_20260209 ("dynamic filtering") requires Anthropic's
// server-side code_execution tool, which we don't currently enable.
const WebSearchToolType = "web_search_20250305"

type messagePayload struct {
	Model       string            `json:"model"`
	MaxTokens   int               `json:"max_tokens"`
	Temperature *float64          `json:"temperature,omitempty"`
	Stream      bool              `json:"stream"`
	System      string            `json:"system,omitempty"`
	Messages    []messageJSON     `json:"messages"`
	Tools       []json.RawMessage `json:"tools,omitempty"`
	ToolChoice  *toolChoiceJSON   `json:"tool_choice,omitempty"`
}

type messageJSON struct {
	Role    string            `json:"role"`
	Content []json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result
	ToolUseID     string `json:"tool_use_id,omitempty"`
	ResultContent string `json:"content,omitempty"`
	IsError       bool   `json:"is_error,omitempty"`
}

type toolJSON struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type toolChoiceJSON struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

func buildRequestBody(req llm.Request) ([]byte, error) {
	if req.Model == "" {
		return nil, fmt.Errorf("anthropic: model is required")
	}
	if req.MaxTokens <= 0 {
		return nil, fmt.Errorf("anthropic: max_tokens must be > 0")
	}
	msgs := make([]messageJSON, 0, len(req.Messages))
	for _, m := range req.Messages {
		blocks := make([]json.RawMessage, 0, len(m.Content))
		for _, c := range m.Content {
			raw, err := marshalBlock(c)
			if err != nil {
				return nil, err
			}
			if raw != nil {
				blocks = append(blocks, raw)
			}
		}
		msgs = append(msgs, messageJSON{Role: string(m.Role), Content: blocks})
	}

	tools := make([]json.RawMessage, 0, len(req.Tools)+1)
	for _, t := range req.Tools {
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		raw, err := json.Marshal(toolJSON{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
		if err != nil {
			return nil, fmt.Errorf("anthropic: marshal tool %s: %w", t.Name, err)
		}
		tools = append(tools, raw)
	}
	// Always advertise the server-side web_search tool. Per project design
	// principle (CLAUDE.md): no toggle — web search is always on.
	tools = append(tools, json.RawMessage(`{"type":"`+WebSearchToolType+`","name":"web_search","max_uses":10}`))

	payload := messagePayload{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Stream:    true,
		System:    req.System,
		Messages:  msgs,
		Tools:     tools,
	}
	if req.Temperature != 0 {
		t := req.Temperature
		payload.Temperature = &t
	}
	return json.Marshal(payload)
}

// marshalBlock encodes one canonical ContentBlock for the Anthropic API.
// Returns (nil, nil) to silently skip a block (e.g. thinking blocks we
// can't round-trip).
func marshalBlock(c llm.ContentBlock) (json.RawMessage, error) {
	switch b := c.(type) {
	case llm.TextBlock:
		return json.Marshal(contentBlock{Type: "text", Text: b.Text})
	case llm.ToolUseBlock:
		input := b.Input
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		return json.Marshal(contentBlock{
			Type:  "tool_use",
			ID:    b.ID,
			Name:  b.Name,
			Input: input,
		})
	case llm.ToolResultBlock:
		return json.Marshal(contentBlock{
			Type:          "tool_result",
			ToolUseID:     b.ToolUseID,
			ResultContent: b.Content,
			IsError:       b.IsError,
		})
	case llm.ThinkingBlock:
		// We can't round-trip thinking blocks; skip silently — Phase 1
		// providers don't emit them either.
		return nil, nil
	case llm.ServerToolBlock:
		if b.Provider != ProviderID {
			// Provider-foreign server block; cannot round-trip.
			return nil, nil
		}
		if len(b.Raw) == 0 {
			return nil, nil
		}
		return b.Raw, nil
	default:
		return nil, fmt.Errorf("anthropic: unsupported content block %T", c)
	}
}

func mapStopReason(s string) llm.StopReason {
	switch s {
	case "end_turn":
		return llm.StopEndTurn
	case "tool_use":
		return llm.StopToolUse
	case "max_tokens":
		return llm.StopMaxTokens
	case "stop_sequence":
		return llm.StopStopSequence
	default:
		return llm.StopReason(s)
	}
}
