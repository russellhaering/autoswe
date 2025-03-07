package dependencies

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/wire"
	"github.com/invopop/jsonschema"
	"go.uber.org/zap"
	"golang.org/x/tools/go/packages"

	"github.com/russellhaering/auto-swe/pkg/log"
)

// ListInput represents the input parameters for the List tool
type ListInput struct {
	// No parameters needed
}

// Dependency represents a single Go module dependency
type Dependency struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Direct  bool   `json:"direct"`
}

// ListOutput represents the output of the List tool
type ListOutput struct {
	Dependencies []Dependency `json:"dependencies,omitempty"`
}

// ListTool implements the List tool
type ListTool struct{}

var ProvideListTool = wire.Struct(new(ListTool), "*")

// Name returns the name of the tool
func (t *ListTool) Name() string {
	return "dependencies_list"
}

// Description returns a description of the list tool
func (t *ListTool) Description() string {
	return "Lists all Go module dependencies"
}

// Schema returns the JSON schema for the list tool
func (t *ListTool) Schema() *jsonschema.Schema {
	return jsonschema.Reflect(&ListInput{})
}

// Execute implements the list operation
func (t *ListTool) Execute(ctx context.Context, _ ListInput) (ListOutput, error) {
	log.Info("Starting package analysis")

	cfg := &packages.Config{
		Mode: packages.NeedImports | packages.NeedDeps | packages.NeedModule,
	}

	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		log.Error("Failed to load packages", zap.Error(err))
		return ListOutput{}, fmt.Errorf("failed to load packages: %w", err)
	}

	// Use a map to deduplicate dependencies
	depMap := make(map[string]Dependency)

	// Traverse all packages and their dependencies
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		if pkg.Module == nil || pkg.Module.Path == "" {
			return
		}

		// Skip the main module
		if pkg.Module.Main {
			return
		}

		depMap[pkg.Module.Path] = Dependency{
			Path:    pkg.Module.Path,
			Version: pkg.Module.Version,
			Direct:  strings.Contains(pkg.Module.GoMod, "go.mod"), // If it's in go.mod, it's direct
		}
	})

	// Convert map to slice
	dependencies := make([]Dependency, 0, len(depMap))
	for _, dep := range depMap {
		dependencies = append(dependencies, dep)
	}

	log.Info("Successfully found dependencies", zap.Int("count", len(dependencies)))
	return ListOutput{
		Dependencies: dependencies,
	}, nil
}
