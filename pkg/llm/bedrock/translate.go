package bedrock

import (
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/russellhaering/autoswe/pkg/llm"
)

func buildConverseInput(req llm.Request) (*bedrockruntime.ConverseStreamInput, error) {
	if req.Model == "" {
		return nil, fmt.Errorf("bedrock: model is required")
	}
	msgs, err := translateMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	in := &bedrockruntime.ConverseStreamInput{
		ModelId:  aws.String(req.Model),
		Messages: msgs,
	}
	if req.System != "" {
		in.System = []types.SystemContentBlock{
			&types.SystemContentBlockMemberText{Value: req.System},
		}
	}
	if len(req.Tools) > 0 {
		toolList, err := translateTools(req.Tools)
		if err != nil {
			return nil, err
		}
		in.ToolConfig = &types.ToolConfiguration{Tools: toolList}
	}
	cfg := &types.InferenceConfiguration{}
	hasCfg := false
	if req.MaxTokens > 0 {
		cfg.MaxTokens = aws.Int32(int32(req.MaxTokens))
		hasCfg = true
	}
	if req.Temperature != 0 {
		cfg.Temperature = aws.Float32(float32(req.Temperature))
		hasCfg = true
	}
	if hasCfg {
		in.InferenceConfig = cfg
	}
	return in, nil
}

func translateMessages(msgs []llm.Message) ([]types.Message, error) {
	out := make([]types.Message, 0, len(msgs))
	for _, m := range msgs {
		role := types.ConversationRoleUser
		if m.Role == llm.RoleAssistant {
			role = types.ConversationRoleAssistant
		}
		blocks := make([]types.ContentBlock, 0, len(m.Content))
		for _, c := range m.Content {
			switch b := c.(type) {
			case llm.TextBlock:
				blocks = append(blocks, &types.ContentBlockMemberText{Value: b.Text})
			case llm.ToolUseBlock:
				input := b.Input
				if len(input) == 0 {
					input = json.RawMessage(`{}`)
				}
				var asMap any
				if err := json.Unmarshal(input, &asMap); err != nil {
					return nil, fmt.Errorf("bedrock: tool_use %q has invalid JSON input: %w", b.ID, err)
				}
				blocks = append(blocks, &types.ContentBlockMemberToolUse{Value: types.ToolUseBlock{
					ToolUseId: aws.String(b.ID),
					Name:      aws.String(b.Name),
					Input:     document.NewLazyDocument(asMap),
				}})
			case llm.ToolResultBlock:
				status := types.ToolResultStatusSuccess
				if b.IsError {
					status = types.ToolResultStatusError
				}
				blocks = append(blocks, &types.ContentBlockMemberToolResult{Value: types.ToolResultBlock{
					ToolUseId: aws.String(b.ToolUseID),
					Status:    status,
					Content: []types.ToolResultContentBlock{
						&types.ToolResultContentBlockMemberText{Value: b.Content},
					},
				}})
			case llm.ThinkingBlock:
				// Phase 2: drop thinking blocks; Bedrock has its own reasoning shape we don't surface yet.
			default:
				return nil, fmt.Errorf("bedrock: unsupported content block %T", c)
			}
		}
		out = append(out, types.Message{Role: role, Content: blocks})
	}
	return out, nil
}

func translateTools(specs []llm.ToolSpec) ([]types.Tool, error) {
	out := make([]types.Tool, 0, len(specs))
	for _, t := range specs {
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		var asMap any
		if err := json.Unmarshal(schema, &asMap); err != nil {
			return nil, fmt.Errorf("bedrock: tool %q has invalid input_schema JSON: %w", t.Name, err)
		}
		out = append(out, &types.ToolMemberToolSpec{Value: types.ToolSpecification{
			Name:        aws.String(t.Name),
			Description: aws.String(t.Description),
			InputSchema: &types.ToolInputSchemaMemberJson{Value: document.NewLazyDocument(asMap)},
		}})
	}
	return out, nil
}

func mapStopReason(s types.StopReason) llm.StopReason {
	switch s {
	case types.StopReasonEndTurn:
		return llm.StopEndTurn
	case types.StopReasonToolUse:
		return llm.StopToolUse
	case types.StopReasonMaxTokens:
		return llm.StopMaxTokens
	case types.StopReasonStopSequence:
		return llm.StopStopSequence
	default:
		return llm.StopReason(s)
	}
}
