package anthropic

import (
	"encoding/json"
	"fmt"

	"github.com/russellhaering/autoswe/pkg/llm"
)

type messagePayload struct {
	Model       string            `json:"model"`
	MaxTokens   int               `json:"max_tokens"`
	Temperature *float64          `json:"temperature,omitempty"`
	Stream      bool              `json:"stream"`
	System      string            `json:"system,omitempty"`
	Messages    []messageJSON     `json:"messages"`
	Tools       []toolJSON        `json:"tools,omitempty"`
	ToolChoice  *toolChoiceJSON   `json:"tool_choice,omitempty"`
}

type messageJSON struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
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
		blocks := make([]contentBlock, 0, len(m.Content))
		for _, c := range m.Content {
			switch b := c.(type) {
			case llm.TextBlock:
				blocks = append(blocks, contentBlock{Type: "text", Text: b.Text})
			case llm.ToolUseBlock:
				input := b.Input
				if len(input) == 0 {
					input = json.RawMessage("{}")
				}
				blocks = append(blocks, contentBlock{
					Type:  "tool_use",
					ID:    b.ID,
					Name:  b.Name,
					Input: input,
				})
			case llm.ToolResultBlock:
				blocks = append(blocks, contentBlock{
					Type:          "tool_result",
					ToolUseID:     b.ToolUseID,
					ResultContent: b.Content,
					IsError:       b.IsError,
				})
			case llm.ThinkingBlock:
				// Anthropic thinking blocks have their own shape; Phase 1 does not
				// round-trip them. Skip silently — the provider does not emit
				// thinking blocks itself in Phase 1 either.
			default:
				return nil, fmt.Errorf("anthropic: unsupported content block %T", c)
			}
		}
		msgs = append(msgs, messageJSON{Role: string(m.Role), Content: blocks})
	}

	tools := make([]toolJSON, 0, len(req.Tools))
	for _, t := range req.Tools {
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		tools = append(tools, toolJSON{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}

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
