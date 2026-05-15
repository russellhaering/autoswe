package llm

import (
	"encoding/json"
	"testing"
)

func TestMessageRoundTrip(t *testing.T) {
	in := Message{
		Role: RoleAssistant,
		Content: []ContentBlock{
			TextBlock{Text: "Reading."},
			ToolUseBlock{ID: "tu_1", Name: "read", Input: json.RawMessage(`{"path":"README.md"}`)},
			ThinkingBlock{Thinking: "secret"},
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Message
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Role != RoleAssistant {
		t.Fatalf("role = %v", out.Role)
	}
	if len(out.Content) != 3 {
		t.Fatalf("content count = %d", len(out.Content))
	}
	if tb, ok := out.Content[0].(TextBlock); !ok || tb.Text != "Reading." {
		t.Fatalf("block 0 = %+v", out.Content[0])
	}
	if tu, ok := out.Content[1].(ToolUseBlock); !ok || tu.ID != "tu_1" || tu.Name != "read" || string(tu.Input) != `{"path":"README.md"}` {
		t.Fatalf("block 1 = %+v", out.Content[1])
	}
	if th, ok := out.Content[2].(ThinkingBlock); !ok || th.Thinking != "secret" {
		t.Fatalf("block 2 = %+v", out.Content[2])
	}
}

func TestMessageToolResultRoundTrip(t *testing.T) {
	in := Message{
		Role: RoleUser,
		Content: []ContentBlock{
			ToolResultBlock{ToolUseID: "tu_1", Content: "ok", IsError: true},
		},
	}
	b, _ := json.Marshal(in)
	var out Message
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	tr, ok := out.Content[0].(ToolResultBlock)
	if !ok || tr.ToolUseID != "tu_1" || tr.Content != "ok" || !tr.IsError {
		t.Fatalf("got %+v", out.Content[0])
	}
}
