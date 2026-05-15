package session

import (
	"encoding/json"
	"testing"

	"github.com/russellhaering/autoswe/pkg/llm"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	id := NewID()

	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "hi"}}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			llm.TextBlock{Text: "Let me look."},
			llm.ToolUseBlock{ID: "tu_1", Name: "read", Input: json.RawMessage(`{"path":"x"}`)},
		}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{
			llm.ToolResultBlock{ToolUseID: "tu_1", Content: "ok"},
		}},
	}
	if err := Save(dir, id, msgs); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir, id)
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != id {
		t.Fatalf("id = %s", s.ID)
	}
	if len(s.Messages) != len(msgs) {
		t.Fatalf("got %d messages, want %d", len(s.Messages), len(msgs))
	}
	if tu, ok := s.Messages[1].Content[1].(llm.ToolUseBlock); !ok || tu.ID != "tu_1" {
		t.Fatalf("round trip failed for tool_use: %+v", s.Messages[1].Content[1])
	}
}

func TestLoadMissing(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(dir, "sess_nonexistent"); err == nil {
		t.Fatal("expected error loading missing session")
	}
}

func TestNewIDFormat(t *testing.T) {
	for i := 0; i < 5; i++ {
		id := NewID()
		if len(id) != len("sess_")+8 {
			t.Fatalf("id %q has unexpected length", id)
		}
	}
}
