package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/russellhaering/autoswe/pkg/llm"
)

func TestBuildRequestBodyGolden(t *testing.T) {
	req := llm.Request{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 1024,
		System:    "You are concise.",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "read README"}}},
			{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
				llm.TextBlock{Text: "ok"},
				llm.ToolUseBlock{ID: "toolu_1", Name: "read", Input: json.RawMessage(`{"path":"README.md"}`)},
			}},
			{Role: llm.RoleUser, Content: []llm.ContentBlock{
				llm.ToolResultBlock{ToolUseID: "toolu_1", Content: "hello"},
			}},
		},
		Tools: []llm.ToolSpec{
			{Name: "read", Description: "Read a file.", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)},
		},
	}
	body, err := buildRequestBody(req)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}

	want := map[string]any{
		"model":      "claude-sonnet-4-5",
		"max_tokens": float64(1024),
		"stream":     true,
		"system":     "You are concise.",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "read README"},
				},
			},
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "text", "text": "ok"},
					map[string]any{
						"type":  "tool_use",
						"id":    "toolu_1",
						"name":  "read",
						"input": map[string]any{"path": "README.md"},
					},
				},
			},
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":         "tool_result",
						"tool_use_id":  "toolu_1",
						"content":      "hello",
					},
				},
			},
		},
		"tools": []any{
			map[string]any{
				"name":        "read",
				"description": "Read a file.",
				"input_schema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"path": map[string]any{"type": "string"}},
					"required":   []any{"path"},
				},
			},
			// web_search is always appended; see CLAUDE.md (no toggle).
			map[string]any{
				"type":     "web_search_20250305",
				"name":     "web_search",
				"max_uses": float64(10),
			},
		},
	}
	if !equalJSON(got, want) {
		gotPretty, _ := json.MarshalIndent(got, "", "  ")
		wantPretty, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("request body mismatch\nwant:\n%s\n\ngot:\n%s", wantPretty, gotPretty)
	}
}

func TestParseStreamHappyPath(t *testing.T) {
	body := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"m1","role":"assistant","content":[],"usage":{"input_tokens":12,"output_tokens":1}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_2","name":"read","input":{}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"README.md\"}"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":1}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":48}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	ch := make(chan llm.Event, 32)
	parseStream(io.NopCloser(strings.NewReader(body)), ch)

	var got []llm.Event
	for ev := range ch {
		got = append(got, ev)
	}

	// Expected sequence:
	// TextDelta("Hello"), TextDelta(" world"),
	// ToolUseStart{toolu_2, read}, ToolUseDelta x2, ToolUseStop,
	// MessageStop{tool_use}
	if len(got) != 7 {
		t.Fatalf("want 7 events, got %d: %+v", len(got), got)
	}
	if td, ok := got[0].(llm.TextDelta); !ok || td.Text != "Hello" {
		t.Fatalf("event 0: %+v", got[0])
	}
	if td, ok := got[1].(llm.TextDelta); !ok || td.Text != " world" {
		t.Fatalf("event 1: %+v", got[1])
	}
	if s, ok := got[2].(llm.ToolUseStart); !ok || s.ID != "toolu_2" || s.Name != "read" {
		t.Fatalf("event 2: %+v", got[2])
	}
	if _, ok := got[3].(llm.ToolUseDelta); !ok {
		t.Fatalf("event 3 is not ToolUseDelta: %+v", got[3])
	}
	if _, ok := got[4].(llm.ToolUseDelta); !ok {
		t.Fatalf("event 4 is not ToolUseDelta: %+v", got[4])
	}
	if s, ok := got[5].(llm.ToolUseStop); !ok || s.ID != "toolu_2" || string(s.Input) != `{"path":"README.md"}` {
		t.Fatalf("event 5: %+v (input=%s)", got[5], func() string {
			if s, ok := got[5].(llm.ToolUseStop); ok {
				return string(s.Input)
			}
			return ""
		}())
	}
	stop, ok := got[6].(llm.MessageStop)
	if !ok {
		t.Fatalf("event 6 is not MessageStop: %+v", got[6])
	}
	if stop.Reason != llm.StopToolUse {
		t.Fatalf("stop reason = %v, want tool_use", stop.Reason)
	}
	if stop.Usage.InputTokens != 12 || stop.Usage.OutputTokens != 48 {
		t.Fatalf("usage = %+v", stop.Usage)
	}
}

func TestParseStreamServerTools(t *testing.T) {
	body := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"m1","role":"assistant","content":[],"usage":{"input_tokens":5,"output_tokens":0}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Let me search."}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"server_tool_use","id":"srv_1","name":"web_search"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"foo\"}"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":1}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":2,"content_block":{"type":"web_search_tool_result","tool_use_id":"srv_1","content":[{"type":"web_search_result","url":"https://x","title":"X"}]}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":2}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	ch := make(chan llm.Event, 32)
	parseStream(io.NopCloser(strings.NewReader(body)), ch)

	var sts []llm.ServerToolStop
	for ev := range ch {
		if s, ok := ev.(llm.ServerToolStop); ok {
			sts = append(sts, s)
		}
	}
	if len(sts) != 2 {
		t.Fatalf("want 2 ServerToolStop events; got %d", len(sts))
	}
	if sts[0].BlockType != "server_tool_use" || sts[0].Name != "web_search" {
		t.Errorf("stop[0] = %+v", sts[0])
	}
	// Verify the input was merged into the start JSON for server_tool_use.
	var stu map[string]any
	if err := json.Unmarshal(sts[0].Raw, &stu); err != nil {
		t.Fatalf("server_tool_use raw not JSON: %v", err)
	}
	if input, ok := stu["input"].(map[string]any); !ok || input["query"] != "foo" {
		t.Errorf("server_tool_use missing input.query=foo; got %+v", stu)
	}
	if sts[1].BlockType != "web_search_tool_result" {
		t.Errorf("stop[1] = %+v", sts[1])
	}
}

func TestBuildRequestBodyServerToolRoundTrip(t *testing.T) {
	raw := json.RawMessage(`{"type":"server_tool_use","id":"srv_1","name":"web_search","input":{"query":"x"}}`)
	req := llm.Request{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 256,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "hi"}}},
			{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
				llm.TextBlock{Text: "let me search"},
				llm.ServerToolBlock{Provider: ProviderID, BlockType: "server_tool_use", Raw: raw},
			}},
			{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "go on"}}},
		},
	}
	body, err := buildRequestBody(req)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	asst := got["messages"].([]any)[1].(map[string]any)
	content := asst["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("expected 2 content blocks; got %d", len(content))
	}
	b1 := content[1].(map[string]any)
	if b1["type"] != "server_tool_use" || b1["id"] != "srv_1" {
		t.Errorf("server_tool_use not preserved: %+v", b1)
	}
}

func TestStreamHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request","message":"bad"}}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Stream(context.Background(), llm.Request{Model: "x", MaxTokens: 1, Messages: []llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "hi"}}},
	}})
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	if !strings.Contains(err.Error(), "HTTP 400") {
		t.Fatalf("error did not mention HTTP 400: %v", err)
	}
}

// equalJSON normalizes nil vs missing slice/map and compares structurally.
func equalJSON(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	var an, bn any
	_ = json.Unmarshal(ab, &an)
	_ = json.Unmarshal(bb, &bn)
	ab2, _ := json.Marshal(an)
	bb2, _ := json.Marshal(bn)
	return string(ab2) == string(bb2)
}
