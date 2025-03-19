package format

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/google/wire"
	"github.com/invopop/jsonschema"
	"github.com/russellhaering/autoswe/pkg/log"
	"go.uber.org/zap"
)

// Input represents the input parameters for the Format tool
type Input struct {
	// No parameters needed
}

// Output represents the output of the Format tool
type Output struct {
	Output string `json:"output"`
}

// Tool implements the Format tool
type Tool struct{}

var ProvideFormatTool = wire.Struct(new(Tool), "*")

// Name returns the name of the tool
func (t *Tool) Name() string {
	return "format"
}

// Description returns a description of the format tool
func (t *Tool) Description() string {
	return "Runs goimports on all Go files in the project"
}

// Schema returns the JSON schema for the format tool
func (t *Tool) Schema() *jsonschema.Schema {
	return jsonschema.Reflect(&Input{})
}

// Execute implements the format operation
func (t *Tool) Execute(_ context.Context, _ Input) (Output, error) {
	log.Info("Starting format operation")

	cmd := exec.Command("goimports", "-w", ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Error("Formatting failed", zap.Error(err), zap.String("output", string(out)))
		return Output{}, fmt.Errorf("formatting failed: %w", err)
	}

	log.Info("Formatting completed successfully")

	return Output{
		Output: string(out),
	}, nil
}
