package openai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/russellhaering/autoswe/pkg/llm"
)

type chatPayload struct {
	Model               string         `json:"model"`
	Messages            []messageJSON  `json:"messages"`
	Tools               []toolJSON     `json:"tools,omitempty"`
	Stream              bool           `json:"stream"`
	StreamOptions       *streamOptions `json:"stream_options,omitempty"`
	MaxCompletionTokens int            `json:"max_completion_tokens,omitempty"`
	Temperature         *float64       `json:"temperature,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type messageJSON struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []toolCallJSON `json:"tool_calls,omitempty"`
}

type toolCallJSON struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function functionCallJSON `json:"function"`
}

type functionCallJSON struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type toolJSON struct {
	Type     string          `json:"type"`
	Function functionDecJSON `json:"function"`
}

type functionDecJSON struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

// translateMessages flattens our canonical Messages into OpenAI's message
// shape. Notable differences:
//   - Anthropic puts tool_use blocks inside the assistant message; OpenAI uses
//     a separate `tool_calls` field on the assistant message (still one
//     message).
//   - Anthropic puts tool_result blocks inside a user message; OpenAI requires
//     one message per tool result with role=tool. We split accordingly.
func translateMessages(system string, msgs []llm.Message) ([]messageJSON, error) {
	out := make([]messageJSON, 0, len(msgs)+1)
	if system != "" {
		out = append(out, messageJSON{Role: "system", Content: system})
	}
	for _, m := range msgs {
		switch m.Role {
		case llm.RoleUser:
			var text strings.Builder
			var toolResults []messageJSON
			for _, c := range m.Content {
				switch b := c.(type) {
				case llm.TextBlock:
					if text.Len() > 0 {
						text.WriteString("\n")
					}
					text.WriteString(b.Text)
				case llm.ToolResultBlock:
					toolResults = append(toolResults, messageJSON{
						Role:       "tool",
						ToolCallID: b.ToolUseID,
						Content:    b.Content,
					})
				default:
					return nil, fmt.Errorf("openai: unsupported user content block %T", c)
				}
			}
			if text.Len() > 0 {
				out = append(out, messageJSON{Role: "user", Content: text.String()})
			}
			out = append(out, toolResults...)
		case llm.RoleAssistant:
			var text strings.Builder
			var calls []toolCallJSON
			for _, c := range m.Content {
				switch b := c.(type) {
				case llm.TextBlock:
					if text.Len() > 0 {
						text.WriteString("\n")
					}
					text.WriteString(b.Text)
				case llm.ToolUseBlock:
					args := string(b.Input)
					if args == "" {
						args = "{}"
					}
					calls = append(calls, toolCallJSON{
						ID:       b.ID,
						Type:     "function",
						Function: functionCallJSON{Name: b.Name, Arguments: args},
					})
				case llm.ThinkingBlock:
					// Phase 1: drop assistant thinking blocks for OpenAI.
				default:
					return nil, fmt.Errorf("openai: unsupported assistant content block %T", c)
				}
			}
			out = append(out, messageJSON{
				Role:      "assistant",
				Content:   text.String(),
				ToolCalls: calls,
			})
		default:
			return nil, fmt.Errorf("openai: unknown role %q", m.Role)
		}
	}
	return out, nil
}

func translateTools(tools []llm.ToolSpec) []toolJSON {
	if len(tools) == 0 {
		return nil
	}
	out := make([]toolJSON, 0, len(tools))
	for _, t := range tools {
		params := t.InputSchema
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object"}`)
		}
		out = append(out, toolJSON{
			Type: "function",
			Function: functionDecJSON{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}
	return out
}

func buildRequestBody(req llm.Request) ([]byte, error) {
	if req.Model == "" {
		return nil, fmt.Errorf("openai: model is required")
	}
	msgs, err := translateMessages(req.System, req.Messages)
	if err != nil {
		return nil, err
	}
	payload := chatPayload{
		Model:               req.Model,
		Messages:            msgs,
		Tools:               translateTools(req.Tools),
		Stream:              true,
		StreamOptions:       &streamOptions{IncludeUsage: true},
		MaxCompletionTokens: req.MaxTokens,
	}
	if req.Temperature != 0 {
		t := req.Temperature
		payload.Temperature = &t
	}
	return json.Marshal(payload)
}

func mapFinishReason(s string) llm.StopReason {
	switch s {
	case "stop":
		return llm.StopEndTurn
	case "tool_calls":
		return llm.StopToolUse
	case "length":
		return llm.StopMaxTokens
	case "content_filter":
		return llm.StopReason("content_filter")
	default:
		return llm.StopReason(s)
	}
}
