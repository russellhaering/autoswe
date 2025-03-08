package git

import (
	"context"
	"fmt"

	"github.com/google/wire"
	"github.com/invopop/jsonschema"
	"github.com/russellhaering/autoswe/pkg/log"
	"github.com/russellhaering/autoswe/pkg/repo"
	"go.uber.org/zap"
)

// CommandInput represents the input parameters for the Command tool
type CommandInput struct {
	Args []string `json:"args" jsonschema_description:"Git command arguments (e.g. ['status', '-s'])"`
}

// CommandOutput represents the output of the Command tool
type CommandOutput struct {
	Output string `json:"output"`
}

// CommandTool implements the git command tool
type CommandTool struct {
	RepoFS *repo.RepoFS
}

var ProvideCommandTool = wire.Struct(new(CommandTool), "*")

// Name returns the name of the tool
func (t *CommandTool) Name() string {
	return "git_command"
}

// Description returns a description of the git command tool
func (t *CommandTool) Description() string {
	return "Runs arbitrary git sub-commands"
}

// Schema returns the JSON schema for the git command tool
func (t *CommandTool) Schema() *jsonschema.Schema {
	return jsonschema.Reflect(&CommandInput{})
}

// Execute implements the git command operation
func (t *CommandTool) Execute(ctx context.Context, input CommandInput) (CommandOutput, error) {
	log.Info("Starting git command operation", zap.Any("args", input.Args))

	if len(input.Args) == 0 {
		log.Error("No git command arguments provided")
		return CommandOutput{}, fmt.Errorf("no git command arguments provided")
	}

	cfg := &Config{
		WorkDir: t.RepoFS.Path(),
	}

	// Execute git command directly
	out, err := ExecGit(cfg, input.Args...)
	if err != nil {
		log.Error("Git command failed", zap.Error(err), zap.String("output", out))
		return CommandOutput{}, fmt.Errorf("git command failed: %w", err)
	}

	log.Info("Git command completed successfully")

	return CommandOutput{
		Output: out,
	}, nil
}
