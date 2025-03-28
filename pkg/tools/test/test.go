package test

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/google/wire"
	"github.com/invopop/jsonschema"
	"github.com/russellhaering/autoswe/pkg/log"
	"go.uber.org/zap"
)

// Input represents the input parameters for the Test tool
type Input struct {
	// No parameters needed
}

// Output represents the output of the Test tool
type Output struct {
	Output string `json:"output"`
}

// Tool implements the Test tool
type Tool struct{}

var ProvideTestTool = wire.Struct(new(Tool), "*")

// Name returns the name of the tool
func (t *Tool) Name() string {
	return "test"
}

// Description returns a description of the test tool
func (t *Tool) Description() string {
	return "Runs project tests using 'go test -v ./...'"
}

// Schema returns the JSON schema for the test tool
func (t *Tool) Schema() *jsonschema.Schema {
	return jsonschema.Reflect(&Input{})
}

// Execute implements the test operation
func (t *Tool) Execute(_ context.Context, _ Input) (Output, error) {
	log.Info("Starting test operation")

	cmd := exec.Command("go", "test", "-v", "./...")
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Error("Tests failed", zap.Error(err), zap.String("output", string(out)))
		return Output{}, fmt.Errorf("tests failed: %w", err)
	}

	log.Info("Tests completed successfully")

	return Output{
		Output: string(out),
	}, nil
}
