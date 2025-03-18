package fs

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/russellhaering/autoswe/pkg/repo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrepToolStringOutput(t *testing.T) {
	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", "grep-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create a test file
	testFile := filepath.Join(tempDir, "test.txt")
	content := "Line 1\nLine 2\nLine 3 with pattern\nLine 4\nLine 5\n"
	err = os.WriteFile(testFile, []byte(content), 0644)
	require.NoError(t, err)

	// Create the filtered filesystem
	fs, err := repo.NewFilesystem(tempDir)
	require.NoError(t, err)
	filteredFS := repo.NewFilteredFS(fs, func(path string) bool {
		return true // Allow all files
	})

	// Create a grep tool instance
	tool := GrepTool{
		FilteredFS: filteredFS,
	}

	// Test with a pattern
	input := GrepInput{
		Pattern: "pattern",
	}
	result, err := tool.Execute(context.Background(), input)
	require.NoError(t, err)

	// Print the result for debugging
	t.Logf("Result: %+v", result)

	// Verify output is a string and contains expected content
	assert.Contains(t, result.Result, "Line 3 with pattern")
	assert.Contains(t, result.Result, "Line 1")
	assert.Contains(t, result.Result, "Line 2")
	assert.Contains(t, result.Result, "Line 4")
	assert.Contains(t, result.Result, "Line 5")
	
	// Test with no matches
	input = GrepInput{
		Pattern: "nonexistent",
	}
	result, err = tool.Execute(context.Background(), input)
	require.NoError(t, err)
	
	assert.Contains(t, result.Result, "No matches found for pattern: nonexistent")
}