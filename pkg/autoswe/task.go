package autoswe

import (
	"context"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/russellhaering/auto-swe/pkg/log"
	"go.uber.org/zap"
)

const BaseSystemPrompt = `You are an expert software engineer. You will be given tasks to complete, and you will need to complete them using the tools available to you.

Best practices for task handling:
- Break down complex problems into focused subtasks when needed
- For each subtask, invoke the 'task' tool with a clear, specific description
- Be sure to lint, test, and format your code as needed
- Complete the current task by responding with text and no tool invocation
- If you need to make a large number of small modifications to a file, use fs_put to write the entire file at once
- Explain your reasoning as you go, and only modify files once you have a clear plan of action
- Don't make changes unrelated to the task at hand
- Do not add tools or scripts to the codebase unless you are asked to do so

Be sure to use the tools at your disposal to examine the existing codebase to discover and emulate existing patterns.

Remember to think creatively about how to accomplish each task using the tools at your disposal. You are always encouraged to walk through your reasoning.`

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

// Result represents the result of processing a task
type Result struct {
	Response string
	Error    error
}

func (m *Manager) ExecuteTask(ctx context.Context, description string) (string, error) {
	task := NewTask(description, BaseSystemPrompt)
	return m.processTask(ctx, task)
}

// ProcessTask handles a single task and any subtasks it creates
func (m *Manager) processTask(ctx context.Context, task *Task) (string, error) {
	log.Info("Processing task", zap.String("description", task.Description))

	toolParams := m.ToolRegistry.GetToolParams()

	for {
		message, err := m.AnthropicClient.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.F(anthropic.ModelClaude3_7SonnetLatest),
			MaxTokens: anthropic.Int(8192),
			Messages:  anthropic.F(task.Messages),
			Tools:     anthropic.F(toolParams),
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

		log.Debug("Received assistant response")
		for _, block := range message.Content {
			switch block := block.AsUnion().(type) {
			case anthropic.TextBlock:
				log.Info("Assistant response", zap.String("text", block.Text))
			}
		}

		task.Messages = append(task.Messages, message.ToParam())

		// Process tool calls and collect results
		toolResults, err := m.ToolRegistry.ProcessToolCalls(ctx, message)
		if err != nil {
			return "", fmt.Errorf("failed to process tool calls: %w", err)
		}

		if len(toolResults) == 0 {
			// No more tool calls, task is complete
			if len(message.Content) > 0 {
				return message.Content[len(message.Content)-1].AsUnion().(anthropic.TextBlock).Text, nil
			}
			return "", nil
		}

		task.Messages = append(task.Messages, anthropic.NewUserMessage(toolResults...))
	}
}

// NewTask creates a new task with the given description and system prompt
func NewTask(description string, systemPrompt string) *Task {
	return &Task{
		Description: description,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(systemPrompt)),
			anthropic.NewUserMessage(anthropic.NewTextBlock(description)),
		},
	}
}
