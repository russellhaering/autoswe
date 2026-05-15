package llm

import (
	"encoding/json"
	"fmt"
)

// contentBlockJSON is the on-the-wire shape we use for round-tripping content
// blocks through encoding/json. The shape mirrors Anthropic's tool_result /
// tool_use / text block format, which is the most expressive of the three
// providers we support.
type contentBlockJSON struct {
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

	// thinking
	Thinking string `json:"thinking,omitempty"`
}

// MarshalJSON encodes the message using a stable, type-tagged content-block
// format that round-trips through UnmarshalJSON.
func (m Message) MarshalJSON() ([]byte, error) {
	out := struct {
		Role    string             `json:"role"`
		Content []contentBlockJSON `json:"content"`
	}{Role: string(m.Role)}
	for _, c := range m.Content {
		switch b := c.(type) {
		case TextBlock:
			out.Content = append(out.Content, contentBlockJSON{Type: "text", Text: b.Text})
		case ToolUseBlock:
			out.Content = append(out.Content, contentBlockJSON{Type: "tool_use", ID: b.ID, Name: b.Name, Input: b.Input})
		case ToolResultBlock:
			out.Content = append(out.Content, contentBlockJSON{Type: "tool_result", ToolUseID: b.ToolUseID, ResultContent: b.Content, IsError: b.IsError})
		case ThinkingBlock:
			out.Content = append(out.Content, contentBlockJSON{Type: "thinking", Thinking: b.Thinking})
		default:
			return nil, fmt.Errorf("llm: unknown content block type %T", c)
		}
	}
	return json.Marshal(out)
}

// UnmarshalJSON parses the format produced by MarshalJSON.
func (m *Message) UnmarshalJSON(data []byte) error {
	var raw struct {
		Role    string             `json:"role"`
		Content []contentBlockJSON `json:"content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Role = Role(raw.Role)
	m.Content = nil
	for _, c := range raw.Content {
		switch c.Type {
		case "text":
			m.Content = append(m.Content, TextBlock{Text: c.Text})
		case "tool_use":
			input := c.Input
			if len(input) == 0 {
				input = json.RawMessage("{}")
			}
			m.Content = append(m.Content, ToolUseBlock{ID: c.ID, Name: c.Name, Input: input})
		case "tool_result":
			m.Content = append(m.Content, ToolResultBlock{ToolUseID: c.ToolUseID, Content: c.ResultContent, IsError: c.IsError})
		case "thinking":
			m.Content = append(m.Content, ThinkingBlock{Thinking: c.Thinking})
		default:
			return fmt.Errorf("llm: unknown content block type %q", c.Type)
		}
	}
	return nil
}
