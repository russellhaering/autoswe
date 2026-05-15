package bedrock

import (
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/russellhaering/autoswe/pkg/llm"
)

func TestBuildConverseInput(t *testing.T) {
	req := llm.Request{
		Model:     "anthropic.claude-sonnet-4-5-v1:0",
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
			{Name: "read", Description: "Read a file.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
	}

	in, err := buildConverseInput(req)
	if err != nil {
		t.Fatal(err)
	}
	if aws.ToString(in.ModelId) != req.Model {
		t.Fatalf("ModelId = %s", aws.ToString(in.ModelId))
	}
	if len(in.Messages) != 3 {
		t.Fatalf("want 3 messages, got %d", len(in.Messages))
	}
	if in.Messages[0].Role != types.ConversationRoleUser {
		t.Fatalf("msg 0 role = %v", in.Messages[0].Role)
	}
	if in.Messages[1].Role != types.ConversationRoleAssistant {
		t.Fatalf("msg 1 role = %v", in.Messages[1].Role)
	}

	// Assistant message should have 2 content blocks: text + toolUse.
	if got := len(in.Messages[1].Content); got != 2 {
		t.Fatalf("assistant content blocks = %d, want 2", got)
	}
	if _, ok := in.Messages[1].Content[0].(*types.ContentBlockMemberText); !ok {
		t.Fatalf("assistant block 0 not text: %T", in.Messages[1].Content[0])
	}
	tu, ok := in.Messages[1].Content[1].(*types.ContentBlockMemberToolUse)
	if !ok {
		t.Fatalf("assistant block 1 not toolUse: %T", in.Messages[1].Content[1])
	}
	if aws.ToString(tu.Value.ToolUseId) != "toolu_1" || aws.ToString(tu.Value.Name) != "read" {
		t.Fatalf("toolUse fields = id=%s name=%s", aws.ToString(tu.Value.ToolUseId), aws.ToString(tu.Value.Name))
	}

	// User-2 message should carry one toolResult.
	tr, ok := in.Messages[2].Content[0].(*types.ContentBlockMemberToolResult)
	if !ok {
		t.Fatalf("user-2 block 0 not toolResult: %T", in.Messages[2].Content[0])
	}
	if aws.ToString(tr.Value.ToolUseId) != "toolu_1" {
		t.Fatalf("toolResult id = %s", aws.ToString(tr.Value.ToolUseId))
	}

	// System / inference / tool config wiring.
	if len(in.System) != 1 {
		t.Fatalf("want 1 system block, got %d", len(in.System))
	}
	if in.InferenceConfig == nil || aws.ToInt32(in.InferenceConfig.MaxTokens) != 1024 {
		t.Fatalf("InferenceConfig = %+v", in.InferenceConfig)
	}
	if in.ToolConfig == nil || len(in.ToolConfig.Tools) != 1 {
		t.Fatalf("ToolConfig = %+v", in.ToolConfig)
	}
}

func TestMapStopReason(t *testing.T) {
	cases := map[types.StopReason]llm.StopReason{
		types.StopReasonEndTurn:      llm.StopEndTurn,
		types.StopReasonToolUse:      llm.StopToolUse,
		types.StopReasonMaxTokens:    llm.StopMaxTokens,
		types.StopReasonStopSequence: llm.StopStopSequence,
	}
	for in, want := range cases {
		if got := mapStopReason(in); got != want {
			t.Errorf("mapStopReason(%v) = %v, want %v", in, got, want)
		}
	}
}
