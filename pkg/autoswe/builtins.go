package autoswe

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/invopop/jsonschema"
	"github.com/russellhaering/autoswe/pkg/log"
	"github.com/russellhaering/autoswe/pkg/tools/registry"
	"go.uber.org/zap"
)

type DelegateTaskInput struct {
	Task string `json:"task" jsonschema_description:"Detailed description of the task to delegate"`
}

func (m *Manager) delegateTask(ctx context.Context, toolCall registry.ToolCall) (string, error) {
	var input DelegateTaskInput

	if err := json.Unmarshal(toolCall.Input, &input); err != nil {
		return "", fmt.Errorf("failed to unmarshal delegate task input: %w", err)
	}

	log.Info("delegating task", zap.String("task", input.Task))

	return m.ExecuteTask(ctx, input.Task)
}

func (m *Manager) getToolParams() []anthropic.ToolUnionUnionParam {
	toolParams := m.ToolRegistry.GetToolParams()

	reflector := jsonschema.Reflector{
		DoNotReference: true, // Embed the schema directly instead of using $defs
	}

	toolParams = append(toolParams, anthropic.ToolParam{
		Name:        anthropic.String("delegate_task"),
		Description: anthropic.String("Delegate a task to an expert assistant"),
		InputSchema: anthropic.F(interface{}(reflector.Reflect(DelegateTaskInput{}))),
	})

	return toolParams
}
