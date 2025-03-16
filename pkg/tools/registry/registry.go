package registry

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/wire"
	"github.com/invopop/jsonschema"
	"github.com/russellhaering/autoswe/pkg/log"
	"github.com/russellhaering/autoswe/pkg/tools/astgrep"
	"github.com/russellhaering/autoswe/pkg/tools/build"
	"github.com/russellhaering/autoswe/pkg/tools/dependencies"
	"github.com/russellhaering/autoswe/pkg/tools/exec"
	"github.com/russellhaering/autoswe/pkg/tools/format"
	"github.com/russellhaering/autoswe/pkg/tools/fs"
	"github.com/russellhaering/autoswe/pkg/tools/git"
	"github.com/russellhaering/autoswe/pkg/tools/gopls"
	"github.com/russellhaering/autoswe/pkg/tools/lint"
	"github.com/russellhaering/autoswe/pkg/tools/query"
	"github.com/russellhaering/autoswe/pkg/tools/test"
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

func (r *ToolRegistry) getTool(name string) (*toolWrapper, bool) {
	registration, ok := r.tools[name]
	if !ok {
		return nil, false
	}

	return &toolWrapper{
		name:        registration.name,
		description: registration.description,
		schema:      registration.schema,
		execute:     registration.execute,
	}, true
}

func (r *ToolRegistry) getToolsByName() map[string]*toolWrapper {
	// Create a map of tool names to tool interfaces
	toolsByName := make(map[string]*toolWrapper)

	// Copy from our tools map to the result map
	for name := range r.tools {
		// Create a toolWrapper that satisfies the Tool interface requirements
		wrapper, ok := r.getTool(name)
		if !ok {
			panic(fmt.Sprintf("tool %s not found", name))
		}

		toolsByName[name] = wrapper
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

	for _, wrapper := range toolsByName {
		// Get the schema from the tool, then extract the actual definition
		schema := wrapper.Schema()

		if len(schema.Definitions) != 1 {
			panic(fmt.Sprintf("tool %s has %d definitions, expected 1", wrapper.Name(), len(schema.Definitions)))
		}

		for _, v := range schema.Definitions {
			schema = v
		}

		result = append(result, anthropic.ToolParam{
			Name:        anthropic.F(wrapper.Name()),
			Description: anthropic.F(wrapper.Description()),
			InputSchema: anthropic.F(interface{}(schema)),
		})
	}

	return result
}

type ToolCall struct {
	Name  string
	ID    string
	Input json.RawMessage
}

// ExecuteToolCall handles a single tool call and returns the result
func (r *ToolRegistry) ExecuteToolCall(ctx context.Context, call ToolCall) (string, error) {
	tool, ok := r.getTool(call.Name)
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", call.Name)
	}

	var input interface{}
	if err := json.Unmarshal(call.Input, &input); err != nil {
		log.Error("failed to decode tool input for logging",
			zap.String("tool", tool.Name()),
			zap.String("id", call.ID),
			zap.String("input", string(call.Input)),
			zap.Error(err),
		)

		return "", fmt.Errorf("failed to decode tool input for logging: %w", err)
	}

	// Execute the tool
	response, err := tool.Execute(ctx, input)
	if err != nil {
		log.Error("error from tool call",
			zap.String("tool", tool.Name()),
			zap.String("id", call.ID),
			zap.Any("input", input),
			zap.Any("output", response),
			zap.Error(err),
		)

		return "", err
	}

	responseJSON, err := json.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("failed to marshal response: %w", err)
	}

	return string(responseJSON), nil
}
