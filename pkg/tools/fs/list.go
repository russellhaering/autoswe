package fs

import (
	"context"
	"fmt"
	"io/fs"
	"time"

	"github.com/google/wire"
	"github.com/invopop/jsonschema"
	"github.com/russellhaering/autoswe/pkg/log"
	"github.com/russellhaering/autoswe/pkg/repo"
	"go.uber.org/zap"
)

// ListInput represents the input parameters for the List tool
type ListInput struct {
	Path string `json:"path" jsonschema_description:"Path to list contents of"`
}

// FileInfo represents information about a file or directory
type FileInfo struct {
	Name    string      `json:"name"`
	Size    int64       `json:"size"`
	IsDir   bool        `json:"is_dir"`
	Mode    fs.FileMode `json:"mode"`
	ModTime time.Time   `json:"mod_time"`
}

// ListOutput represents the output of the List tool
type ListOutput struct {
	Files []FileInfo `json:"files,omitempty"`
}

type ListTool struct {
	FilteredFS repo.FilteredFS
}

var ProvideListTool = wire.Struct(new(ListTool), "*")

// Name returns the name of the tool
func (t *ListTool) Name() string {
	return "fs_list"
}

// Description returns a description of the list tool
func (t *ListTool) Description() string {
	return "Lists files and directories at the specified path"
}

// Schema returns the JSON schema for the list tool
func (t *ListTool) Schema() *jsonschema.Schema {
	return jsonschema.Reflect(&ListInput{})
}

// Execute implements the list operation
func (t *ListTool) Execute(ctx context.Context, input ListInput) (ListOutput, error) {
	log.Info("Starting list operation", zap.String("path", input.Path))

	// Check if path exists in the filtered FS
	_, err := fs.Stat(t.FilteredFS, input.Path)
	if err != nil {
		log.Error("Failed to access path", zap.String("path", input.Path), zap.Error(err))
		return ListOutput{}, fmt.Errorf("failed to access path: %w", err)
	}

	// Read directory entries
	entries, err := fs.ReadDir(t.FilteredFS, input.Path)
	if err != nil {
		log.Error("Failed to read directory", zap.String("path", input.Path), zap.Error(err))
		return ListOutput{}, fmt.Errorf("failed to read directory: %w", err)
	}

	// Convert entries to FileInfo
	files := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		// Get file info
		info, err := entry.Info()
		if err != nil {
			log.Warn("Failed to get file info", zap.String("name", entry.Name()), zap.Error(err))
			continue
		}

		files = append(files, FileInfo{
			Name:    info.Name(),
			Size:    info.Size(),
			IsDir:   info.IsDir(),
			Mode:    info.Mode(),
			ModTime: info.ModTime(),
		})
	}

	log.Info("List operation completed", zap.Int("files", len(files)), zap.String("path", input.Path))
	return ListOutput{
		Files: files,
	}, nil
}
