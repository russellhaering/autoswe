package registry

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/wire"
	"github.com/invopop/jsonschema"
	"github.com/russellhaering/auto-swe/pkg/log"
	"github.com/russellhaering/auto-swe/pkg/tools/astgrep"
	"github.com/russellhaering/auto-swe/pkg/tools/build"
	"github.com/russellhaering/auto-swe/pkg/tools/dependencies"
	"github.com/russellhaering/auto-swe/pkg/tools/exec"
	"github.com/russellhaering/auto-swe/pkg/tools/format"
	"github.com/russellhaering/auto-swe/pkg/tools/fs"
	"github.com/russellhaering/auto-swe/pkg/tools/git"
	"github.com/russellhaering/auto-swe/pkg/tools/gopls"
	"github.com/russellhaering/auto-swe/pkg/tools/lint"
	"github.com/russellhaering/auto-swe/pkg/tools/query"
	"github.com/russellhaering/auto-swe/pkg/tools/test"
	"go.uber.org/zap"
)

// To register a new tool, add it to the ToolSet and as a field in the ToolRegistry struct
var ToolSet = wire.NewSet(
	astgrep.ProvideASTGrepTool,
	build.ProvideBuildTool,
	dependencies.ProvideFetchTool,
	dependencies.ProvideListTool,
	exec.ProvideExecTool,
	format.ProvideFormatTool,
	git.ProvideCommandTool,
	git.ProvideCommitTool,
	gopls.ProvideGoplsTool,
	lint.ProvideLintTool,
	test.ProvideTestTool,
	query.ProvideQueryTool,
	fs.ProvideFetchTool,
	fs.ProvideGrepTool,
	fs.ProvideListTool,
	fs.ProvidePatchTool,
	fs.ProvidePutTool,
	fs.ProvideRmTool,
	ProvideToolRegistry,
)

// Tool represents a tool that can be invoked by the AI
type Tool[I, O any] interface {
	Name() string
	Description() string
	Schema() *jsonschema.Schema
	Execute(ctx context.Context, input I) (O, error)
}

type toolRegistration struct {
	name        string
	description string
	schema      *jsonschema.Schema
	execute     func(ctx context.Context, input json.RawMessage) (interface{}, error)
}

type ToolRegistry struct {
	tools map[string]toolRegistration
}

func ProvideToolRegistry(
	astGrepTool *astgrep.ASTGrepTool,
	buildTool *build.BuildTool,
	fetchTool *dependencies.FetchTool,
	listTool *dependencies.ListTool,
	execTool *exec.ExecTool,
	formatTool *format.FormatTool,
	gitCommandTool *git.CommandTool,
	gitCommitTool *git.CommitTool,
	goplsTool *gopls.GoplsTool,
	lintTool *lint.LintTool,
	testTool *test.TestTool,
	queryTool *query.QueryTool,
	fsFetchTool *fs.FetchTool,
	fsGrepTool *fs.GrepTool,
	fsListTool *fs.ListTool,
	fsPatchTool *fs.PatchTool,
	fsPutTool *fs.PutTool,
	fsRmTool *fs.RmTool,
) *ToolRegistry {
	registry := &ToolRegistry{
		tools: make(map[string]toolRegistration),
	}

	RegisterTool(registry, astGrepTool)
	RegisterTool(registry, buildTool)
	RegisterTool(registry, fetchTool)
	RegisterTool(registry, listTool)
	RegisterTool(registry, execTool)
	RegisterTool(registry, formatTool)
	RegisterTool(registry, gitCommandTool)
	RegisterTool(registry, gitCommitTool)
	RegisterTool(registry, goplsTool)
	RegisterTool(registry, lintTool)
	RegisterTool(registry, testTool)
	RegisterTool(registry, queryTool)
	RegisterTool(registry, fsFetchTool)
	RegisterTool(registry, fsGrepTool)
	RegisterTool(registry, fsListTool)
	RegisterTool(registry, fsPatchTool)
	RegisterTool(registry, fsPutTool)
	RegisterTool(registry, fsRmTool)

	return registry
}

// RegisterTool is a function with type parameters that registers a tool with the registry
func RegisterTool[I, O any](registry *ToolRegistry, tool Tool[I, O]) {
	registry.tools[tool.Name()] = toolRegistration{
		name:        tool.Name(),
		description: tool.Description(),
		schema:      tool.Schema(),
		execute: func(ctx context.Context, rawInput json.RawMessage) (interface{}, error) {
			var input I
			if err := json.Unmarshal(rawInput, &input); err != nil {
				return nil, fmt.Errorf("failed to unmarshal input: %w", err)
			}
			result, err := tool.Execute(ctx, input)
			return result, err
		},
	}
}

func (r *ToolRegistry) getToolsByName() map[string]interface{} {
	// Create a map of tool names to tool interfaces
	toolsByName := make(map[string]interface{})

	// Copy from our tools map to the result map
	for name, registration := range r.tools {
		// Create a toolWrapper that satisfies the Tool interface requirements
		toolsByName[name] = &toolWrapper{
			name:        registration.name,
			description: registration.description,
			schema:      registration.schema,
			execute:     registration.execute,
		}
	}

	return toolsByName
}

// toolWrapper implements the Tool interface for any tool
type toolWrapper struct {
	name        string
	description string
	schema      *jsonschema.Schema
	execute     func(ctx context.Context, input json.RawMessage) (interface{}, error)
}

func (t *toolWrapper) Name() string {
	return t.name
}

func (t *toolWrapper) Description() string {
	return t.description
}

func (t *toolWrapper) Schema() *jsonschema.Schema {
	return t.schema
}

func (t *toolWrapper) Execute(ctx context.Context, input any) (any, error) {
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal input: %w", err)
	}

	return t.execute(ctx, inputJSON)
}

func (r *ToolRegistry) GetToolParams() []anthropic.ToolUnionUnionParam {
	toolsByName := r.getToolsByName()

	result := []anthropic.ToolUnionUnionParam{}

	for _, tool := range toolsByName {
		wrapper, ok := tool.(*toolWrapper)
		if !ok {
			continue
		}

		// Get the schema from the tool
		schema := wrapper.Schema()

		// Ensure schema has required fields for Anthropic API
		schemaMap := make(map[string]interface{})

		// Convert the schema to a map
		schemaBytes, err := json.Marshal(schema)
		if err != nil {
			log.Error("failed to marshal schema", zap.Error(err))
			continue
		}

		if err := json.Unmarshal(schemaBytes, &schemaMap); err != nil {
			log.Error("failed to unmarshal schema", zap.Error(err))
			continue
		}

		// Ensure the schema has a type field (required by Anthropic)
		if _, ok := schemaMap["type"]; !ok {
			schemaMap["type"] = "object"
		}

		result = append(result, anthropic.ToolParam{
			Name:        anthropic.F(wrapper.Name()),
			Description: anthropic.F(wrapper.Description()),
			InputSchema: anthropic.F(interface{}(schemaMap)),
		})
	}

	return result
}

// ProcessToolCalls handles a sequence of tool calls and returns the results
func (r *ToolRegistry) ProcessToolCalls(ctx context.Context, message *anthropic.Message) ([]anthropic.ContentBlockParamUnion, error) {
	toolsByName := r.getToolsByName()

	results := []anthropic.ContentBlockParamUnion{}

	for _, block := range message.Content {
		if toolUse, ok := block.AsUnion().(anthropic.ToolUseBlock); ok {
			// Get the tool
			toolInterface, ok := toolsByName[toolUse.Name]
			if !ok {
				log.Error("unknown tool",
					zap.String("tool", toolUse.Name),
					zap.String("id", toolUse.ID),
				)

				return nil, fmt.Errorf("unknown tool: %s", toolUse.Name)
			}

			tool, ok := toolInterface.(*toolWrapper)
			if !ok {
				return nil, fmt.Errorf("invalid tool type: %s", toolUse.Name)
			}

			var input interface{}
			if err := json.Unmarshal(toolUse.Input, &input); err != nil {
				log.Error("failed to decode tool input for logging",
					zap.String("tool", toolUse.Name),
					zap.String("id", toolUse.ID),
					zap.String("input", string(toolUse.Input)),
					zap.Error(err),
				)

				return nil, fmt.Errorf("failed to decode tool input for logging: %w", err)
			}

			log.Debug("handling tool call",
				zap.String("tool", toolUse.Name),
				zap.String("id", toolUse.ID),
				zap.Any("input", input),
			)

			// Execute the tool
			response, err := tool.Execute(ctx, input)
			if err != nil {
				log.Error("error from tool call",
					zap.String("tool", toolUse.Name),
					zap.String("id", toolUse.ID),
					zap.Any("input", input),
					zap.Any("output", response),
					zap.Error(err),
				)

				results = append(results, anthropic.NewToolResultBlock(toolUse.ID, "Error: "+err.Error(), true))
			} else {
				responseJSON, err := json.Marshal(response)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal response: %w", err)
				}

				log.Debug("tool call response",
					zap.String("tool", toolUse.Name),
					zap.String("id", toolUse.ID),
					zap.Any("response", response),
				)

				results = append(results, anthropic.NewToolResultBlock(toolUse.ID, string(responseJSON), false))
			}
		}
	}

	return results, nil
}
