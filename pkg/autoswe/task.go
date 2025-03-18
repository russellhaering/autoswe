package autoswe

import (
	"context"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/russellhaering/autoswe/pkg/log"
	"github.com/russellhaering/autoswe/pkg/prompts"
	"github.com/russellhaering/autoswe/pkg/tools/registry"
	"go.uber.org/zap"
)

// Task represents a single task with its conversation context
type Task struct {
	Description string
	Messages    []anthropic.MessageParam
}

// Clone creates a copy of the task's messages for a new context
func (t *Task) Clone() []anthropic.MessageParam {
	messages := make([]anthropic.MessageParam, len(t.Messages))
	copy(messages, t.Messages)
	return messages
}

func (m *Manager) ExecuteTask(ctx context.Context, description string) (string, error) {
	task := NewTask(description, prompts.System)
	return m.processTask(ctx, task)
}

// ProcessTask handles a single task and any subtasks it creates
func (m *Manager) processTask(ctx context.Context, task *Task) (string, error) {
	log.Info("Processing task", zap.String("description", task.Description))

	toolParams := m.getToolParams()

	for {
		message, err := m.AnthropicClient.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.F(anthropic.ModelClaude3_7SonnetLatest),
			MaxTokens: anthropic.Int(8192),
			System: anthropic.F([]anthropic.TextBlockParam{
				anthropic.NewTextBlock(prompts.System),
			}),
			Messages: anthropic.F(task.Messages),
			Tools:    anthropic.F(toolParams),
		})
		if err != nil {
			return "", fmt.Errorf("failed to get message: %w", err)
		}

		// Log cost information if usage data is available
		if message.Usage.InputTokens != 0 || message.Usage.OutputTokens != 0 {
			inputTokens := float64(message.Usage.InputTokens)
			outputTokens := float64(message.Usage.OutputTokens)

			inputCost := (inputTokens / 1000.0) * 0.003
			outputCost := (outputTokens / 1000.0) * 0.015
			totalCost := inputCost + outputCost

			log.Info("Inference cost",
				zap.Int64("input_tokens", message.Usage.InputTokens),
				zap.Int64("output_tokens", message.Usage.OutputTokens),
				zap.Float64("total_cost_usd", totalCost))
		}

		if len(message.Content) == 0 {
			log.Warn("Received empty assistant response", zap.Any("message", message))
			continue
		}

		log.Debug("Received assistant response")

		task.Messages = append(task.Messages, message.ToParam())
		initialMessageCount := len(task.Messages)

		for _, block := range message.Content {
			switch block := block.AsUnion().(type) {
			case anthropic.TextBlock:
				log.Info("Assistant response", zap.String("text", block.Text))
			case anthropic.ToolUseBlock:
				responseMessage, err := m.handleToolUse(ctx, block)
				if err != nil {
					return "", fmt.Errorf("failed to handle tool use: %w", err)
				}

				task.Messages = append(task.Messages, *responseMessage)
			default:
				log.Warn("Received unexpected block type", zap.Any("block", block))
			}
		}

		// If we didn't append any new messages, the task is complete. Return the last text block.
		if len(task.Messages) == initialMessageCount {
			if len(message.Content) > 0 {
				if textBlock, ok := message.Content[len(message.Content)-1].AsUnion().(anthropic.TextBlock); ok {
					return textBlock.Text, nil
				}
			}

			log.Warn("expected a text block, but didn't get one", zap.Any("message", message))
			return "", nil
		}
	}
}

// handleToolUse handles a tool use block from the assistant's response
func (m *Manager) handleToolUse(ctx context.Context, toolUse anthropic.ToolUseBlock) (*anthropic.MessageParam, error) {
	var msg anthropic.MessageParam

	log.Debug("handling tool call",
		zap.String("tool", toolUse.Name),
		zap.String("id", toolUse.ID),
		zap.Any("input", toolUse.Input),
	)

	result, err := m.executeToolCall(ctx, registry.ToolCall{
		Name:  toolUse.Name,
		ID:    toolUse.ID,
		Input: toolUse.Input,
	})
	if err != nil {
		log.Error("error executing tool call",
			zap.String("tool", toolUse.Name),
			zap.String("id", toolUse.ID),
			zap.Any("input", toolUse.Input),
			zap.Error(err),
		)

		msg = anthropic.NewUserMessage(anthropic.NewToolResultBlock(toolUse.ID, fmt.Sprintf("Error: %s", err), true))
	} else {
		log.Debug("tool call result",
			zap.String("tool", toolUse.Name),
			zap.String("id", toolUse.ID),
			zap.Any("result", result),
		)

		msg = anthropic.NewUserMessage(anthropic.NewToolResultBlock(toolUse.ID, result, false))
	}

	return &msg, nil
}

// executeToolCall executes a tool call using either a built-in tool or a tool from the registry
func (m *Manager) executeToolCall(ctx context.Context, toolCall registry.ToolCall) (string, error) {
	switch toolCall.Name {
	case "delegate_task":
		return m.delegateTask(ctx, toolCall)

	default:
		return m.ToolRegistry.ExecuteToolCall(ctx, toolCall)
	}
}

// NewTask creates a new task with the given description and system prompt
func NewTask(description string, systemPrompt string) *Task {
	return &Task{
		Description: description,
		Messages: []anthropic.MessageParam{
			//anthropic.NewUserMessage(anthropic.NewTextBlock(systemPrompt)),
			anthropic.NewUserMessage(anthropic.NewTextBlock(description)),
		},
	}
}
