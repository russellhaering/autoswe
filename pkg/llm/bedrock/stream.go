package bedrock

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/russellhaering/autoswe/pkg/llm"
)

type toolUseAcc struct {
	id    string
	name  string
	input strings.Builder
}

// parseStream consumes a Bedrock Converse event stream and emits canonical
// llm.Events. It always closes the events channel and the underlying stream.
func parseStream(es *bedrockruntime.ConverseStreamEventStream, events chan<- llm.Event) {
	defer close(events)
	defer es.Close()

	toolAcc := map[int32]*toolUseAcc{}
	var (
		usage      llm.Usage
		stopReason llm.StopReason
		stopped    bool
	)

	for ev := range es.Events() {
		switch v := ev.(type) {
		case *types.ConverseStreamOutputMemberMessageStart:
			// role only; nothing to emit
		case *types.ConverseStreamOutputMemberContentBlockStart:
			if v.Value.Start == nil || v.Value.ContentBlockIndex == nil {
				continue
			}
			if start, ok := v.Value.Start.(*types.ContentBlockStartMemberToolUse); ok {
				id, name := derefStr(start.Value.ToolUseId), derefStr(start.Value.Name)
				toolAcc[*v.Value.ContentBlockIndex] = &toolUseAcc{id: id, name: name}
				events <- llm.ToolUseStart{ID: id, Name: name}
			}
		case *types.ConverseStreamOutputMemberContentBlockDelta:
			if v.Value.Delta == nil || v.Value.ContentBlockIndex == nil {
				continue
			}
			switch d := v.Value.Delta.(type) {
			case *types.ContentBlockDeltaMemberText:
				events <- llm.TextDelta{Text: d.Value}
			case *types.ContentBlockDeltaMemberToolUse:
				partial := derefStr(d.Value.Input)
				if acc, ok := toolAcc[*v.Value.ContentBlockIndex]; ok && partial != "" {
					acc.input.WriteString(partial)
					events <- llm.ToolUseDelta{ID: acc.id, PartialJSON: partial}
				}
			}
		case *types.ConverseStreamOutputMemberContentBlockStop:
			if v.Value.ContentBlockIndex == nil {
				continue
			}
			idx := *v.Value.ContentBlockIndex
			if acc, ok := toolAcc[idx]; ok {
				raw := acc.input.String()
				if raw == "" {
					raw = "{}"
				}
				events <- llm.ToolUseStop{ID: acc.id, Name: acc.name, Input: json.RawMessage(raw)}
				delete(toolAcc, idx)
			}
		case *types.ConverseStreamOutputMemberMessageStop:
			stopReason = mapStopReason(v.Value.StopReason)
		case *types.ConverseStreamOutputMemberMetadata:
			if v.Value.Usage != nil {
				usage.InputTokens = derefInt32(v.Value.Usage.InputTokens)
				usage.OutputTokens = derefInt32(v.Value.Usage.OutputTokens)
			}
			if stopReason != "" {
				events <- llm.MessageStop{Reason: stopReason, Usage: usage}
				stopped = true
			}
		}
	}

	if err := es.Err(); err != nil {
		events <- llm.ErrorEvent{Err: fmt.Errorf("bedrock stream: %w", err)}
		return
	}
	if !stopped {
		if stopReason != "" {
			events <- llm.MessageStop{Reason: stopReason, Usage: usage}
		} else {
			events <- llm.ErrorEvent{Err: fmt.Errorf("bedrock: stream ended without stop event")}
		}
	}
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefInt32(p *int32) int {
	if p == nil {
		return 0
	}
	return int(*p)
}
