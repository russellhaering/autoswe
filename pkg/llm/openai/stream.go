package openai

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/russellhaering/autoswe/pkg/llm"
)

type toolCallAcc struct {
	id       string
	name     string
	args     strings.Builder
	started  bool
	deltaSent bool
}

type streamChunk struct {
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// parseStream consumes an OpenAI Chat Completions SSE response and emits
// canonical llm.Events. The events channel is always closed on return.
func parseStream(body io.ReadCloser, events chan<- llm.Event) {
	defer close(events)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var (
		toolAccs     = map[int]*toolCallAcc{}
		usage        llm.Usage
		finishReason string
		stopped      bool
	)

	emitToolStops := func() {
		// Emit ToolUseStop for any accumulators that haven't been flushed yet.
		// Ordered by index for determinism.
		for i := 0; ; i++ {
			acc, ok := toolAccs[i]
			if !ok {
				break
			}
			input := acc.args.String()
			if input == "" {
				input = "{}"
			}
			events <- llm.ToolUseStop{ID: acc.id, Name: acc.name, Input: json.RawMessage(input)}
			delete(toolAccs, i)
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			events <- llm.ErrorEvent{Err: fmt.Errorf("openai: parse chunk: %w", err)}
			return
		}
		if chunk.Usage != nil {
			usage.InputTokens = chunk.Usage.PromptTokens
			usage.OutputTokens = chunk.Usage.CompletionTokens
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				events <- llm.TextDelta{Text: choice.Delta.Content}
			}
			for _, tc := range choice.Delta.ToolCalls {
				acc := toolAccs[tc.Index]
				if acc == nil {
					acc = &toolCallAcc{}
					toolAccs[tc.Index] = acc
				}
				if tc.ID != "" {
					acc.id = tc.ID
				}
				if tc.Function.Name != "" {
					acc.name = tc.Function.Name
				}
				if !acc.started && acc.id != "" && acc.name != "" {
					events <- llm.ToolUseStart{ID: acc.id, Name: acc.name}
					acc.started = true
				}
				if tc.Function.Arguments != "" {
					acc.args.WriteString(tc.Function.Arguments)
					if acc.started {
						events <- llm.ToolUseDelta{ID: acc.id, PartialJSON: tc.Function.Arguments}
					}
				}
			}
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
		}
	}
	if err := scanner.Err(); err != nil {
		events <- llm.ErrorEvent{Err: err}
		return
	}

	emitToolStops()

	if finishReason != "" {
		events <- llm.MessageStop{Reason: mapFinishReason(finishReason), Usage: usage}
		stopped = true
	}
	if !stopped {
		events <- llm.ErrorEvent{Err: fmt.Errorf("openai: stream ended without a finish_reason")}
	}
}
