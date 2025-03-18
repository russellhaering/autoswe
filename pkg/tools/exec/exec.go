package exec

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/google/wire"
	"github.com/invopop/jsonschema"
	"github.com/russellhaering/autoswe/pkg/log"
	"go.uber.org/zap"
)

const (
	DockerImage = "golang:bookworm"
)

// Input represents the input for the Exec tool
type Input struct {
	Command []string `json:"command" jsonschema_description:"The command to execute."`
}

// Output represents the output of the Exec tool
type Output struct {
	Output string `json:"output" description:"The output of the command"`
}

// ExecTool implements the Exec tool
type ExecTool struct{}

var ProvideExecTool = wire.Struct(new(ExecTool), "*")

// Name returns the name of the tool
func (t *ExecTool) Name() string {
	return "exec"
}

// Description returns a description of the exec tool
func (t *ExecTool) Description() string {
	return fmt.Sprintf("Executes a shell command with the project as the working directory. Commands are executed in a container running the '%s' Docker image with a bash shell.", DockerImage)
}

// Schema returns the JSON schema for the exec tool
func (t *ExecTool) Schema() *jsonschema.Schema {
	return jsonschema.Reflect(&Input{})
}

// Execute implements the exec operation
func (t *ExecTool) Execute(ctx context.Context, input Input) (Output, error) {
	log.Info("Starting exec operation", zap.Strings("command", input.Command))

	if len(input.Command) == 0 {
		log.Error("No command provided")
		return Output{}, fmt.Errorf("no command provided")
	}

	// Get current working directory for mounting
	pwd, err := os.Getwd()
	if err != nil {
		log.Error("Failed to get working directory", zap.Error(err))
		return Output{}, fmt.Errorf("failed to get working directory: %w", err)
	}

	// Construct docker run command
	dockerArgs := []string{
		"run",
		"--rm",                                  // Remove container after execution
		"-v", fmt.Sprintf("%s:/workspace", pwd), // Mount current directory
		"-w", "/workspace", // Set working directory
		DockerImage, // Use the configured image
	}
	dockerArgs = append(dockerArgs, input.Command...)

	// Execute docker command
	cmd := exec.Command("docker", dockerArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Error("Command failed", zap.Error(err), zap.String("output", string(out)))

		// Check if this is an ExitError (command executed but returned non-zero exit code)
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode := exitErr.ExitCode()
			// Return the output with a prefix indicating failure, but don't return an error
			// This makes the output available to the user even when the command fails
			return Output{
				Output: fmt.Sprintf("Command exited with non-zero status code %d\n\n%s",
					exitCode, strings.TrimSpace(string(out))),
			}, nil
		}

		return Output{}, fmt.Errorf("command failed: %w", err)
	}

	log.Info("Command completed successfully")

	return Output{
		Output: strings.TrimSpace(string(out)),
	}, nil
}
