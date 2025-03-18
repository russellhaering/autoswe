package fs

import (
	"context"
	"fmt"
	iofs "io/fs"

	"github.com/google/wire"
	"github.com/invopop/jsonschema"
	"github.com/russellhaering/autoswe/pkg/log"
	"github.com/russellhaering/autoswe/pkg/repo"
	"go.uber.org/zap"

	_ "embed"
)

//go:embed fetch.md
var fetchToolDescription string

// FetchInput represents the input parameters for the Fetch tool
type FetchInput struct {
	Path string `json:"path" jsonschema_description:"Path to the file to fetch"`
}

// FetchOutput represents the output of the Fetch tool
type FetchOutput struct {
	Content string `json:"content"`
}

type FetchTool struct {
	FilteredFS repo.FilteredFS
}

var ProvideFetchTool = wire.Struct(new(FetchTool), "*")

// Name returns the name of the tool
func (t *FetchTool) Name() string {
	return "fs_fetch"
}

// Description returns a description of the fetch tool
func (t *FetchTool) Description() string {
	return fetchToolDescription
}

// Schema returns the JSON schema for the fetch tool
func (t *FetchTool) Schema() *jsonschema.Schema {
	return jsonschema.Reflect(&FetchInput{})
}

// Execute implements the fetch operation
func (t *FetchTool) Execute(ctx context.Context, input FetchInput) (FetchOutput, error) {
	log.Debug("Starting fetch operation", zap.String("path", input.Path))

	// Read the file
	content, err := iofs.ReadFile(t.FilteredFS, input.Path)
	if err != nil {
		log.Error("Failed to read file", zap.String("path", input.Path), zap.Error(err))
		return FetchOutput{}, fmt.Errorf("failed to read file: %w", err)
	}

	log.Debug("Successfully read file", zap.String("path", input.Path), zap.Int("bytes", len(content)))

	return FetchOutput{
		Content: string(content),
	}, nil
}
