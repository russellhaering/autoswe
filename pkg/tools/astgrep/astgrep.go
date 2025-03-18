package astgrep

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

	_ "embed"
)

//go:embed description.md
var toolDescription string

const astGrepImage = "ghcr.io/russellhaering/ast-grep-container:latest"

// Input represents the input parameters for the ASTGrep tool
type Input struct {
	Pattern string   `json:"pattern" jsonschema_description:"ast-grep pattern to search for in the codebase, eg 'func $FUNC($$$ARGS) { $$$ }'"`
	Lang    string   `json:"lang,omitempty" jsonschema_description:"Language to search in (e.g., 'go', 'rust', 'typescript'). If not specified, will search all supported languages."`
	Paths   []string `json:"paths,omitempty" jsonschema_description:"Paths to search in (e.g., 'src', 'test'). If not specified, will default to ."`
	Rewrite string   `json:"rewrite,omitempty" jsonschema_description:"Pattern with which to rewrite matched AST nodes, eg '$PROP?.()'. If unspecified ast-grep will only search for the pattern."`
}

// Output represents the output of the ASTGrep tool
type Output struct {
	Output string `json:"output"`
}

// ASTGrepTool implements the ASTGrep tool
type ASTGrepTool struct{}

var ProvideASTGrepTool = wire.Struct(new(ASTGrepTool), "*")

// Name returns the name of the tool
func (t *ASTGrepTool) Name() string {
	return "ast_grep"
}

// Description returns a description of the astgrep tool
func (t *ASTGrepTool) Description() string {
	return toolDescription
}

// Schema returns the JSON schema for the astgrep tool
func (t *ASTGrepTool) Schema() *jsonschema.Schema {
	return jsonschema.Reflect(&Input{})
}

// Execute implements the astgrep operation
func (t *ASTGrepTool) Execute(ctx context.Context, input Input) (Output, error) {
	log.Info("Starting ast-grep operation", zap.String("pattern", input.Pattern))

	if input.Pattern == "" {
		log.Error("No pattern provided")
		return Output{}, fmt.Errorf("pattern is required")
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
		"-t",                                    // We get less verbose output with a TTY
		"-v", fmt.Sprintf("%s:/workspace", pwd), // Mount current directory
		"-w", "/workspace", // Set working directory
		astGrepImage,
		"ast-grep",
		"run",
		"--pattern", input.Pattern,
	}

	// Add language filter if specified
	if input.Lang != "" {
		dockerArgs = append(dockerArgs, "--lang", input.Lang)
	}

	if len(input.Paths) > 0 {
		dockerArgs = append(dockerArgs, input.Paths...)
	}

	// Execute docker command
	cmd := exec.Command("docker", dockerArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Error("AST grep command failed", zap.Error(err), zap.String("output", string(out)))
		return Output{}, fmt.Errorf("ast-grep command failed: %w", err)
	}

	log.Info("AST grep completed successfully")

	return Output{
		Output: strings.TrimSpace(string(out)),
	}, nil
}
