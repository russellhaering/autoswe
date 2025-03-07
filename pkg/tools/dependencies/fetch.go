package dependencies

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/google/wire"
	"github.com/invopop/jsonschema"
	"github.com/russellhaering/auto-swe/pkg/log"
	"go.uber.org/zap"
)

// FetchInput represents the input parameters for the Fetch tool
type FetchInput struct {
	// No parameters needed
}

// FetchOutput represents the output of the Fetch tool
type FetchOutput struct {
	Output string `json:"output,omitempty"`
}

// FetchTool implements the Fetch tool
type FetchTool struct{}

var ProvideFetchTool = wire.Struct(new(FetchTool), "*")

// Name returns the name of the tool
func (t *FetchTool) Name() string {
	return "dependencies_fetch"
}

// Description returns a description of the fetch tool
func (t *FetchTool) Description() string {
	return "Fetches Go module dependencies using go mod download"
}

// Schema returns the JSON schema for the fetch tool
func (t *FetchTool) Schema() *jsonschema.Schema {
	return jsonschema.Reflect(&FetchInput{})
}

// Execute implements the fetch operation
func (t *FetchTool) Execute(ctx context.Context, _ FetchInput) (FetchOutput, error) {
	log.Info("Starting go mod download")

	cmd := exec.Command("go", "mod", "download")
	output, err := cmd.CombinedOutput()

	if err != nil {
		log.Error("Failed to download dependencies", zap.Error(err), zap.String("output", string(output)))
		return FetchOutput{}, fmt.Errorf("failed to download dependencies: %w", err)
	}

	log.Info("Successfully downloaded dependencies", zap.String("output", string(output)))
	return FetchOutput{
		Output: string(output),
	}, nil
}
