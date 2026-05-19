package anthropic

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/russellhaering/autoswe/pkg/llm"
)

type toolUseAccumulator struct {
	id    string
	name  string
	input strings.Builder
}

// serverBlockAccumulator captures a complete server-side content block
// (server_tool_use, web_search_tool_result) so it can be round-tripped
// verbatim in subsequent turns and surfaced to the loop for visibility.
type serverBlockAccumulator struct {
	blockType string
	name      string // for server_tool_use blocks
	// startJSON is the content_block JSON from the content_block_start
	// event (everything except the streamed input for server_tool_use).
	startJSON json.RawMessage
	// inputBuf accumulates the partial_json deltas for server_tool_use.
	inputBuf strings.Builder
}

// parseStream consumes an Anthropic SSE response body and emits canonical
// llm.Events. The events channel is always closed on return.
func parseStream(body io.ReadCloser, events chan<- llm.Event) {
	defer close(events)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var (
		eventType string
		dataLines []string
		toolAcc   = map[int]*toolUseAccumulator{}
		serverAcc = map[int]*serverBlockAccumulator{}
		usage     llm.Usage
		stopped   bool
	)

	flush := func() {
		defer func() {
			eventType = ""
			dataLines = nil
		}()
		if eventType == "" {
			return
		}
		data := strings.Join(dataLines, "\n")
		switch eventType {
		case "message_start":
			var p struct {
				Message struct {
					Usage struct {
						InputTokens  int `json:"input_tokens"`
						OutputTokens int `json:"output_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(data), &p); err == nil {
				usage.InputTokens = p.Message.Usage.InputTokens
				usage.OutputTokens = p.Message.Usage.OutputTokens
			}
		case "content_block_start":
			var p struct {
				Index        int             `json:"index"`
				ContentBlock json.RawMessage `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(data), &p); err != nil {
				return
			}
			var meta struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			_ = json.Unmarshal(p.ContentBlock, &meta)
			switch meta.Type {
			case "tool_use":
				toolAcc[p.Index] = &toolUseAccumulator{id: meta.ID, name: meta.Name}
				events <- llm.ToolUseStart{ID: meta.ID, Name: meta.Name}
			case "server_tool_use":
				// Input streams in via input_json_delta; full JSON assembled on stop.
				serverAcc[p.Index] = &serverBlockAccumulator{
					blockType: meta.Type,
					name:      meta.Name,
					startJSON: cloneRaw(p.ContentBlock),
				}
			case "web_search_tool_result":
				// Result is fully inlined in the start event — no deltas follow.
				serverAcc[p.Index] = &serverBlockAccumulator{
					blockType: meta.Type,
					startJSON: cloneRaw(p.ContentBlock),
				}
			}
		case "content_block_delta":
			var p struct {
				Index int `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &p); err != nil {
				return
			}
			switch p.Delta.Type {
			case "text_delta":
				events <- llm.TextDelta{Text: p.Delta.Text}
			case "input_json_delta":
				if acc, ok := toolAcc[p.Index]; ok {
					acc.input.WriteString(p.Delta.PartialJSON)
					events <- llm.ToolUseDelta{ID: acc.id, PartialJSON: p.Delta.PartialJSON}
				} else if acc, ok := serverAcc[p.Index]; ok {
					acc.inputBuf.WriteString(p.Delta.PartialJSON)
				}
			}
		case "content_block_stop":
			var p struct {
				Index int `json:"index"`
			}
			if err := json.Unmarshal([]byte(data), &p); err == nil {
				if acc, ok := toolAcc[p.Index]; ok {
					raw := acc.input.String()
					if raw == "" {
						raw = "{}"
					}
					events <- llm.ToolUseStop{ID: acc.id, Name: acc.name, Input: json.RawMessage(raw)}
					delete(toolAcc, p.Index)
				} else if acc, ok := serverAcc[p.Index]; ok {
					raw := finalizeServerBlock(acc)
					events <- llm.ServerToolStop{
						Provider:  ProviderID,
						BlockType: acc.blockType,
						Name:      acc.name,
						Raw:       raw,
					}
					delete(serverAcc, p.Index)
				}
			}
		case "message_delta":
			var p struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &p); err == nil {
				if p.Usage.OutputTokens > 0 {
					usage.OutputTokens = p.Usage.OutputTokens
				}
				if p.Delta.StopReason != "" {
					events <- llm.MessageStop{Reason: mapStopReason(p.Delta.StopReason), Usage: usage}
					stopped = true
				}
			}
		case "message_stop":
			// Already emitted MessageStop on message_delta when stop_reason arrived.
		case "ping":
			// keep-alive
		case "error":
			var p struct {
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(data), &p); err == nil {
				events <- llm.ErrorEvent{Err: fmt.Errorf("anthropic %s: %s", p.Error.Type, p.Error.Message)}
			}
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		case strings.HasPrefix(line, ":"):
			// SSE comment
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		events <- llm.ErrorEvent{Err: err}
		return
	}
	if !stopped {
		events <- llm.ErrorEvent{Err: fmt.Errorf("anthropic: stream ended without message_delta stop_reason")}
	}
}

func cloneRaw(r json.RawMessage) json.RawMessage {
	out := make([]byte, len(r))
	copy(out, r)
	return out
}

// finalizeServerBlock returns the full block JSON. For server_tool_use,
// the streamed input is merged into the start JSON's `input` field. For
// web_search_tool_result the start JSON is already complete.
func finalizeServerBlock(acc *serverBlockAccumulator) json.RawMessage {
	if acc.blockType != "server_tool_use" || acc.inputBuf.Len() == 0 {
		return acc.startJSON
	}
	// Parse start as a generic map, splice in `input`, re-marshal.
	var m map[string]any
	if err := json.Unmarshal(acc.startJSON, &m); err != nil {
		return acc.startJSON
	}
	var inp any
	if err := json.Unmarshal([]byte(acc.inputBuf.String()), &inp); err != nil {
		// keep as a string if it doesn't parse
		inp = acc.inputBuf.String()
	}
	m["input"] = inp
	out, err := json.Marshal(m)
	if err != nil {
		return acc.startJSON
	}
	return out
}
