package fs

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/google/wire"
	"github.com/invopop/jsonschema"
	"github.com/russellhaering/autoswe/pkg/log"
	"github.com/russellhaering/autoswe/pkg/repo"
	"go.uber.org/zap"
)

// RmInput represents the input parameters for the Rm tool
type RmInput struct {
	Path      string `json:"path" jsonschema_description:"Path to the file to delete"`
	Recursive bool   `json:"recursive,omitempty" jsonschema_description:"If true, recursively remove directories and their contents"`
}

// RmOutput represents the output of the Rm tool
type RmOutput struct{}

type RmTool struct {
	FilteredFS repo.FilteredFS
}

var ProvideRmTool = wire.Struct(new(RmTool), "*")

// Name returns the name of the tool
func (t *RmTool) Name() string {
	return "fs_rm"
}

// Description returns a description of the rm tool
func (t *RmTool) Description() string {
	return "Removes a file or directory"
}

// Schema returns the JSON schema for the rm tool
func (t *RmTool) Schema() *jsonschema.Schema {
	return jsonschema.Reflect(&RmInput{})
}

// Execute implements the rm operation
func (t *RmTool) Execute(ctx context.Context, input RmInput) (RmOutput, error) {
	log.Info("Starting remove operation", zap.String("path", input.Path), zap.Bool("recursive", input.Recursive))

	var err error
	if input.Recursive {
		// Use RemoveAll for recursive removal
		err = t.FilteredFS.RemoveAll(input.Path)
	} else {
		// Use Remove for non-recursive removal
		err = t.FilteredFS.Remove(input.Path)
	}

	if err != nil {
		log.Error("Failed to remove", zap.String("path", input.Path), zap.Error(err))
		return RmOutput{}, fmt.Errorf("failed to remove: %w", err)
	}

	log.Info("Successfully removed", zap.String("path", input.Path))

	// If non-recursive and successful, check if parent directory is empty
	if !input.Recursive {
		// Get the parent directory
		parentDir := filepath.Dir(input.Path)

		// Skip the check if parent is root
		if parentDir == "." {
			return RmOutput{}, nil
		}

		log.Debug("Checking if parent directory is empty", zap.String("dir", parentDir))

		// Read the directory entries using FilteredFS
		entries, err := fs.ReadDir(t.FilteredFS, parentDir)
		if err != nil {
			log.Warn("Failed to read parent directory", zap.String("dir", parentDir), zap.Error(err))
			return RmOutput{}, nil // Return success anyway since file was deleted
		}

		// If directory is empty, remove it
		if len(entries) == 0 {
			if err := t.FilteredFS.Remove(parentDir); err != nil {
				log.Warn("Failed to remove empty parent directory", zap.String("dir", parentDir), zap.Error(err))
				return RmOutput{}, nil // Return success anyway since file was deleted
			}
			log.Info("Successfully removed empty parent directory", zap.String("dir", parentDir))
		} else {
			log.Debug("Parent directory not empty, skipping removal", zap.String("dir", parentDir))
		}
	}

	return RmOutput{}, nil
}
