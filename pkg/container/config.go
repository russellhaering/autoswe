package container

// Config represents configuration for container execution
type Config struct {
	// Image is the container image to use for execution
	// Default: "go:bookworm"
	Image string `json:"image"`

	// Volume mounts in the format "source:target"
	Mounts []string `json:"mounts"`

	// WorkDir sets the working directory in the container
	WorkDir string `json:"workdir"`
}

// DefaultConfig provides standard configuration for container execution
var DefaultConfig = Config{
	Image: "go:bookworm",
}
