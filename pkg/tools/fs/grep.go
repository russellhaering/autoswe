package fs

import (
	"context"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"github.com/google/wire"
	"github.com/invopop/jsonschema"
	"github.com/russellhaering/autoswe/pkg/log"
	"github.com/russellhaering/autoswe/pkg/repo"
	"go.uber.org/zap"
)

// GrepInput represents the parameters for the grep operation
type GrepInput struct {
	Pattern string `json:"pattern" jsonschema_description:"Regular expression pattern to search for"`
	Path    string `json:"path,omitempty" jsonschema_description:"Optional path to limit the search scope (defaults to .)"`
}

// GrepMatch represents a single match found by grep
type GrepMatch struct {
	File    string   `json:"file"`
	Line    int      `json:"line"`
	Content string   `json:"content"`
	Before  []string `json:"before"`
	After   []string `json:"after"`
}

// GrepOutput represents the results of the grep operation
type GrepOutput struct {
	Matches []GrepMatch `json:"matches,omitempty"`
}

type GrepTool struct {
	FilteredFS repo.FilteredFS
}

var ProvideGrepTool = wire.Struct(new(GrepTool), "*")

// Name returns the name of the tool
func (t *GrepTool) Name() string {
	return "fs_grep"
}

// Description returns a description of the grep tool
func (t *GrepTool) Description() string {
	return "Searches files for a regular expression pattern"
}

// Schema returns the JSON schema for the grep tool
func (t *GrepTool) Schema() *jsonschema.Schema {
	return jsonschema.Reflect(&GrepInput{})
}

// Execute implements the grep operation
func (t *GrepTool) Execute(ctx context.Context, input GrepInput) (GrepOutput, error) {
	log.Info("Starting grep operation", zap.String("pattern", input.Pattern))

	if input.Pattern == "" {
		log.Error("Pattern is required")
		return GrepOutput{}, fmt.Errorf("pattern is required")
	}

	re, err := regexp.Compile(input.Pattern)
	if err != nil {
		log.Error("Invalid regex pattern", zap.Error(err))
		return GrepOutput{}, fmt.Errorf("invalid regex pattern: %w", err)
	}

	var matches []GrepMatch
	searchPath := "."
	if input.Path != "" {
		searchPath = input.Path
	}

	// Check if path exists in the filtered FS
	_, err = fs.Stat(t.FilteredFS, searchPath)
	if err != nil {
		log.Error("Failed to access path", zap.String("path", searchPath), zap.Error(err))
		return GrepOutput{}, fmt.Errorf("failed to access path: %w", err)
	}

	err = fs.WalkDir(t.FilteredFS, searchPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Warn("Error accessing path during walk", zap.String("path", path), zap.Error(err))
			return nil // Continue walking despite errors
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Read file content
		content, err := fs.ReadFile(t.FilteredFS, path)
		if err != nil {
			log.Warn("Failed to read file", zap.String("path", path), zap.Error(err))
			return nil // Skip files we can't read
		}

		// Process the file line by line to maintain line numbers
		lines := strings.Split(string(content), "\n")
		for lineNum, line := range lines {
			if re.MatchString(line) {
				// Calculate context line ranges
				const contextLines = 3

				beforeStart := lineNum - contextLines
				if beforeStart < 0 {
					beforeStart = 0
				}
				afterEnd := lineNum + contextLines + 1
				if afterEnd > len(lines) {
					afterEnd = len(lines)
				}

				// Get before context
				before := make([]string, 0, contextLines)
				for i := beforeStart; i < lineNum; i++ {
					before = append(before, lines[i])
				}

				// Get after context
				after := make([]string, 0, contextLines)
				for i := lineNum + 1; i < afterEnd; i++ {
					after = append(after, lines[i])
				}

				matches = append(matches, GrepMatch{
					File:    path,
					Line:    lineNum + 1, // Convert to 1-based line numbers
					Content: line,
					Before:  before,
					After:   after,
				})
			}
		}

		return nil
	})

	if err != nil {
		log.Error("Failed to search files", zap.Error(err))
		return GrepOutput{}, fmt.Errorf("failed to search files: %w", err)
	}

	log.Info("Grep operation completed", zap.Int("matches", len(matches)))
	return GrepOutput{
		Matches: matches,
	}, nil
}
