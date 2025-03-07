package container

import (
	"fmt"
	"os/exec"
)

// Execute executes a command in a container using the configured image
func Execute(cfg Config, command string, args ...string) ([]byte, error) {
	if cfg.Image == "" {
		cfg = DefaultConfig
	}

	// Construct docker run command
	dockerArgs := []string{
		"run",
		"--rm",           // Remove container after execution
		"-i",             // Interactive mode
		"--network=none", // No network access
	}

	// Add volume mounts
	for _, mount := range cfg.Mounts {
		dockerArgs = append(dockerArgs, "-v", mount)
	}

	// Set working directory if specified
	if cfg.WorkDir != "" {
		dockerArgs = append(dockerArgs, "-w", cfg.WorkDir)
	}

	// Add image and command
	dockerArgs = append(dockerArgs, cfg.Image, command)
	dockerArgs = append(dockerArgs, args...)

	// Execute docker command
	cmd := exec.Command("docker", dockerArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("container execution failed: %w", err)
	}

	return output, nil
}
