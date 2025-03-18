package fs

import (
	"context"
	"fmt"

	"github.com/google/wire"
	"github.com/invopop/jsonschema"
	"github.com/russellhaering/autoswe/pkg/log"
	"github.com/russellhaering/autoswe/pkg/repo"
	"go.uber.org/zap"

	_ "embed"
)

//go:embed put.md
var putToolDescription string

// PutInput represents the input parameters for the Put tool
type PutInput struct {
	Path    string `json:"path" jsonschema_description:"Path to the file to write"`
	Content string `json:"content" jsonschema_description:"Content to write to the file"`
}

// PutOutput represents the output of the Put tool
type PutOutput struct{}

type PutTool struct {
	FilteredFS repo.FilteredFS
}

var ProvidePutTool = wire.Struct(new(PutTool), "*")

// Name returns the name of the tool
func (t *PutTool) Name() string {
	return "fs_put"
}

// Description returns a description of the put tool
func (t *PutTool) Description() string {
	return putToolDescription
}

// Schema returns the JSON schema for the put tool
func (t *PutTool) Schema() *jsonschema.Schema {
	return jsonschema.Reflect(&PutInput{})
}

// Execute implements the put operation
func (t *PutTool) Execute(ctx context.Context, input PutInput) (PutOutput, error) {
	log.Debug("Starting put operation",
		zap.String("path", input.Path),
		zap.Int("contentLength", len(input.Content)))

	if input.Content == "" {
		log.Error("Empty content provided", zap.String("path", input.Path))
		return PutOutput{}, fmt.Errorf("content is required")
	}

	// Write the file using FilteredFS
	err := t.FilteredFS.WriteFile(input.Path, []byte(input.Content), 0644)
	if err != nil {
		log.Error("Failed to write file", zap.String("path", input.Path), zap.Error(err))
		return PutOutput{}, fmt.Errorf("failed to write file: %w", err)
	}

	log.Info("Successfully wrote file",
		zap.String("path", input.Path),
		zap.Int("bytes", len(input.Content)))

	return PutOutput{}, nil
}
