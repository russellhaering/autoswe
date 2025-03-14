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

// LintTool implements the Lint tool
type LintTool struct{}

var ProvideLintTool = wire.Struct(new(LintTool), "*")

// Name returns the name of the tool
func (t *LintTool) Name() string {
	return "lint"
}

// Description returns a description of the lint tool
func (t *LintTool) Description() string {
	return "Runs golangci-lint on the project"
}

// Schema returns the JSON schema for the lint tool
func (t *LintTool) Schema() *jsonschema.Schema {
	return jsonschema.Reflect(&Input{})
}

// Execute implements the lint operation
func (t *LintTool) Execute(ctx context.Context, _ Input) (Output, error) {
	log.Info("Starting lint operation")

	cmd := exec.Command("golangci-lint", "run")
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Error("Lint failed", zap.Error(err), zap.String("output", string(out)))
		return Output{}, fmt.Errorf("linting failed: %w", err)
	}

	log.Info("Lint completed successfully")

	return Output{
		Output: string(out),
	}, nil
}
