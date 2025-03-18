package lint

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/google/wire"
	"github.com/invopop/jsonschema"
	"github.com/russellhaering/autoswe/pkg/log"
	"go.uber.org/zap"
)

// Input represents the input parameters for the Lint tool
type Input struct {
	// No parameters needed
}

// Output represents the output of the Lint tool
type Output struct {
	Output string `json:"output"`
}

// Tool implements the Lint tool
type Tool struct{}

var ProvideLintTool = wire.Struct(new(Tool), "*")

// Name returns the name of the tool
func (t *Tool) Name() string {
	return "lint"
}

// Description returns a description of the lint tool
func (t *Tool) Description() string {
	return "Runs golangci-lint on the project"
}

// Schema returns the JSON schema for the lint tool
func (t *Tool) Schema() *jsonschema.Schema {
	return jsonschema.Reflect(&Input{})
}

// Execute implements the lint operation
func (t *Tool) Execute(_ context.Context, _ Input) (Output, error) {
	log.Info("Starting lint operation")

	cmd := exec.Command("golangci-lint", "run")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			// This is expected
			return Output{
				Output: string(out),
			}, nil
		}

		log.Error("error executing lint", zap.Error(err), zap.String("output", string(out)))
		return Output{}, fmt.Errorf("linting failed: %w", err)
	}

	log.Info("Lint completed successfully")

	return Output{
		Output: string(out),
	}, nil
}
