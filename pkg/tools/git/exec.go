package git

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/russellhaering/autoswe/pkg/log"
	"go.uber.org/zap"
)

// Config represents configuration for git execution
type Config struct {
	// Working directory to execute git commands from
	WorkDir string
}

// ExecGit executes a git command directly on the local system
func ExecGit(cfg *Config, args ...string) (string, error) {
	// Create the command with git and the provided arguments
	cmd := exec.Command("git", args...)

	// Set the working directory
	cmd.Dir = cfg.WorkDir

	// Log the command being executed
	log.Info("Executing git command",
		zap.String("dir", cmd.Dir),
		zap.Strings("args", append([]string{"git"}, args...)),
	)

	// Execute git command
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git command failed: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}
