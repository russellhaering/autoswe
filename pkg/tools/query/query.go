package query

import (
	"context"
	"fmt"

	"github.com/google/wire"
	"github.com/invopop/jsonschema"
	"github.com/russellhaering/autoswe/pkg/index"
	"github.com/russellhaering/autoswe/pkg/log"
	"go.uber.org/zap"

	_ "embed"
)

//go:embed query.md
var queryToolDescription string

// Input represents the input parameters for the Query tool
type Input struct {
	Query string `json:"query" jsonschema_description:"The query to search for in the codebase"`
}

// Output represents the output of the Query tool
type Output struct {
	Answer string `json:"answer,omitempty"`
}

// CodeExample represents a specific code example from the codebase
type CodeExample struct {
	Path      string `json:"path"`       // Path to the file
	StartLine int    `json:"start_line"` // Starting line number
	EndLine   int    `json:"end_line"`   // Ending line number
	Content   string `json:"content"`    // The actual code content
}

// QueryTool implements the Query tool
type QueryTool struct {
	Indexer *index.Indexer
}

var ProvideQueryTool = wire.Struct(new(QueryTool), "*")

// Name returns the name of the tool
func (t *QueryTool) Name() string {
	return "query_codebase"
}

// Description returns a description of the query tool
func (t *QueryTool) Description() string {
	return queryToolDescription
}

// Schema returns the JSON schema for the query tool
func (t *QueryTool) Schema() *jsonschema.Schema {
	return jsonschema.Reflect(&Input{})
}

// Execute implements the query operation
func (t *QueryTool) Execute(ctx context.Context, input Input) (Output, error) {
	log.Info("Starting codebase query operation", zap.String("query", input.Query))

	// Perform the query
	result, err := t.Indexer.Query(ctx, input.Query)
	if err != nil {
		log.Error("Failed to query codebase", zap.Error(err))
		return Output{}, fmt.Errorf("failed to query codebase: %w", err)
	}

	log.Info("Query completed successfully")

	return Output{
		Answer: result.Answer,
	}, nil
}
