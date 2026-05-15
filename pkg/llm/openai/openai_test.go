package openai

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
		Model:     "gpt-5",
		MaxTokens: 1024,
		System:    "You are concise.",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "read README"}}},
			{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
				llm.TextBlock{Text: "ok"},
				llm.ToolUseBlock{ID: "call_1", Name: "read", Input: json.RawMessage(`{"path":"README.md"}`)},
			}},
			{Role: llm.RoleUser, Content: []llm.ContentBlock{
				llm.ToolResultBlock{ToolUseID: "call_1", Content: "hello"},
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
		"model":                 "gpt-5",
		"stream":                true,
		"stream_options":        map[string]any{"include_usage": true},
		"max_completion_tokens": float64(1024),
		"messages": []any{
			map[string]any{"role": "system", "content": "You are concise."},
			map[string]any{"role": "user", "content": "read README"},
			map[string]any{
				"role":    "assistant",
				"content": "ok",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "read",
							"arguments": `{"path":"README.md"}`,
						},
					},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "hello"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "read",
					"description": "Read a file.",
					"parameters": map[string]any{
						"type":       "object",
						"properties": map[string]any{"path": map[string]any{"type": "string"}},
						"required":   []any{"path"},
					},
				},
			},
		},
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	var gn, wn any
	_ = json.Unmarshal(gotJSON, &gn)
	_ = json.Unmarshal(wantJSON, &wn)
	g2, _ := json.Marshal(gn)
	w2, _ := json.Marshal(wn)
	if string(g2) != string(w2) {
		gp, _ := json.MarshalIndent(got, "", "  ")
		wp, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("mismatch\nwant:\n%s\n\ngot:\n%s", wp, gp)
	}
}

func TestParseStreamText(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	ch := make(chan llm.Event, 32)
	parseStream(io.NopCloser(strings.NewReader(body)), ch)
	var got []llm.Event
	for ev := range ch {
		got = append(got, ev)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 events, got %d: %+v", len(got), got)
	}
	if td, ok := got[0].(llm.TextDelta); !ok || td.Text != "Hello" {
		t.Fatalf("0: %+v", got[0])
	}
	if td, ok := got[1].(llm.TextDelta); !ok || td.Text != " world" {
		t.Fatalf("1: %+v", got[1])
	}
	stop, ok := got[2].(llm.MessageStop)
	if !ok {
		t.Fatalf("2 not MessageStop: %+v", got[2])
	}
	if stop.Reason != llm.StopEndTurn {
		t.Fatalf("stop reason = %v", stop.Reason)
	}
	if stop.Usage.InputTokens != 10 || stop.Usage.OutputTokens != 3 {
		t.Fatalf("usage = %+v", stop.Usage)
	}
}

func TestParseStreamToolCall(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read","arguments":""}}]},"finish_reason":null}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]},"finish_reason":null}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"README.md\"}"}}]},"finish_reason":null}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	ch := make(chan llm.Event, 32)
	parseStream(io.NopCloser(strings.NewReader(body)), ch)
	var got []llm.Event
	for ev := range ch {
		got = append(got, ev)
	}
	// Expected: ToolUseStart, ToolUseDelta, ToolUseDelta, ToolUseStop, MessageStop
	if len(got) != 5 {
		t.Fatalf("want 5 events, got %d: %+v", len(got), got)
	}
	if s, ok := got[0].(llm.ToolUseStart); !ok || s.ID != "call_1" || s.Name != "read" {
		t.Fatalf("0: %+v", got[0])
	}
	if _, ok := got[1].(llm.ToolUseDelta); !ok {
		t.Fatalf("1 not ToolUseDelta: %+v", got[1])
	}
	if _, ok := got[2].(llm.ToolUseDelta); !ok {
		t.Fatalf("2 not ToolUseDelta: %+v", got[2])
	}
	stop, ok := got[3].(llm.ToolUseStop)
	if !ok || stop.ID != "call_1" || string(stop.Input) != `{"path":"README.md"}` {
		t.Fatalf("3: %+v", got[3])
	}
	ms, ok := got[4].(llm.MessageStop)
	if !ok || ms.Reason != llm.StopToolUse {
		t.Fatalf("4: %+v", got[4])
	}
}

func TestStreamHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Stream(context.Background(), llm.Request{Model: "x", Messages: []llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock{Text: "hi"}}},
	}})
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("expected HTTP 401 error, got %v", err)
	}
}
