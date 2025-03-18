package git

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

//go:embed commit.md
var commitToolDescription string

// CommitInput represents the input parameters for the Commit tool
type CommitInput struct {
	Message string `json:"message" jsonschema_description:"Commit message"`
}

// CommitOutput represents the output of the Commit tool
type CommitOutput struct {
	Output string `json:"output"`
}

// CommitTool implements the git commit tool
type CommitTool struct {
	RepoFS *repo.RepoFS
}

var ProvideCommitTool = wire.Struct(new(CommitTool), "*")

// Name returns the name of the tool
func (t *CommitTool) Name() string {
	return "git_commit"
}

// Description returns a description of the git commit tool
func (t *CommitTool) Description() string {
	return commitToolDescription
}

// Schema returns the JSON schema for the git commit tool
func (t *CommitTool) Schema() *jsonschema.Schema {
	return jsonschema.Reflect(&CommitInput{})
}

// Execute implements the git commit operation
func (t *CommitTool) Execute(ctx context.Context, input CommitInput) (CommitOutput, error) {
	log.Info("Starting git commit operation", zap.String("message", input.Message))

	cfg := &Config{
		WorkDir: t.RepoFS.Path(),
	}

	// First stage all changes using direct git execution
	out, err := ExecGit(cfg, "add", ".")
	if err != nil {
		log.Error("Failed to stage changes", zap.Error(err), zap.String("output", out))
		return CommitOutput{}, fmt.Errorf("failed to stage changes: %w", err)
	}

	// Then create the commit using direct git execution
	out, err = ExecGit(cfg, "commit", "-m", input.Message)
	if err != nil {
		log.Error("Commit failed", zap.Error(err), zap.String("output", out))
		return CommitOutput{}, fmt.Errorf("commit failed: %w", err)
	}

	log.Info("Commit completed successfully")

	return CommitOutput{
		Output: out,
	}, nil
}
