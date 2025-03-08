package build

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/google/wire"
	"github.com/invopop/jsonschema"
	"github.com/russellhaering/autoswe/pkg/log"
	"go.uber.org/zap"
)

// Input represents the input parameters for the Build tool
type Input struct {
	// No parameters needed
}

// Output represents the output of the Build tool
type Output struct {
	Output string `json:"output"`
}

// BuildTool implements the Build tool
type BuildTool struct{}

var ProvideBuildTool = wire.Struct(new(BuildTool), "*")

// Name returns the name of the tool
func (t *BuildTool) Name() string {
	return "build"
}

// Description returns a description of the build tool
func (t *BuildTool) Description() string {
	return "Compiles the project using 'go build ./...'"
}

// Schema returns the JSON schema for the build tool
func (t *BuildTool) Schema() *jsonschema.Schema {
	return jsonschema.Reflect(&Input{})
}

// Execute implements the build operation
func (t *BuildTool) Execute(ctx context.Context, _ Input) (Output, error) {
	log.Info("Starting build operation")

	cmd := exec.Command("go", "build", "./...")
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Error("Build failed", zap.Error(err), zap.String("output", string(out)))
		return Output{}, fmt.Errorf("build failed: %w", err)
	}

	log.Info("Build completed successfully")

	return Output{
		Output: string(out),
	}, nil
}
