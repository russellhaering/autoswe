package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/russellhaering/autoswe/pkg/log"
	"github.com/russellhaering/autoswe/pkg/tools/fs/simplediff"
)

// Create a mock version of the PatchTool that doesn't use Gemini
type MockPatchTool struct {
	// Keep track of called methods
	ApplyPatchCalled bool
}

// Mock Execute method that directly uses simplediff
func (m *MockPatchTool) Execute(_ context.Context, input PatchInput) (PatchOutput, error) {
	m.ApplyPatchCalled = true

	if input.Diff == "" {
		return PatchOutput{}, ErrDiffRequired
	}

	// Read the original file
	content, err := os.ReadFile(input.Path)
	if err != nil {
		return PatchOutput{}, err
	}
	originalContent := string(content)

	// Apply the patch using simplediff directly
	result, err := simplediff.ApplyDiff(originalContent, input.Diff)
	if err != nil {
		return PatchOutput{}, err
	}

	// Write the modified content back to the file
	err = os.WriteFile(input.Path, []byte(result), 0644)
	if err != nil {
		return PatchOutput{}, err
	}

	return PatchOutput{}, nil
}

// Error for missing diff
var ErrDiffRequired = fmt.Errorf("diff is required")

func TestPatchWithSimplediff(t *testing.T) {
	// Initialize logger
	if err := log.Init(true); err != nil {
		t.Fatalf("Failed to initialize logger: %v", err)
	}

	// Create a temporary directory for testing
	tempDir := "testdata/temp"
	err := os.MkdirAll(tempDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a test file with relative path
	relativeFilePath := filepath.Join("testdata/temp", "test.txt")
	initialContent := "This is line 1\nThis is line 2\nThis is line 3\n"
	err = os.WriteFile(relativeFilePath, []byte(initialContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create a diff in simplediff format
	diff := `<<<<<<< SEARCH
This is line 2
=======
This is updated line 2
>>>>>>> REPLACE`

	// Create a mock PatchTool instance
	mockPatchTool := &MockPatchTool{}

	// Apply the patch
	ctx := context.Background()
	_, err = mockPatchTool.Execute(ctx, PatchInput{
		Path: relativeFilePath,
		Diff: diff,
	})

	// Verify the patch succeeded
	if err != nil {
		t.Fatalf("Patch failed: %s", err)
	}

	// Read the patched file content
	patchedContent, err := os.ReadFile(relativeFilePath)
	if err != nil {
		t.Fatalf("Failed to read patched file: %v", err)
	}

	// Verify the content was updated correctly
	expectedContent := "This is line 1\nThis is updated line 2\nThis is line 3\n"
	if string(patchedContent) != expectedContent {
		t.Errorf("Patched content doesn't match expected content.\nGot: %q\nWant: %q", string(patchedContent), expectedContent)
	}
}

func TestPatchWithInvalidDiff(t *testing.T) {
	// Initialize logger
	if err := log.Init(true); err != nil {
		t.Fatalf("Failed to initialize logger: %v", err)
	}

	// Create a temporary directory for testing
	tempDir := "testdata/temp"
	err := os.MkdirAll(tempDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	// Create a test file with relative path
	relativeFilePath := filepath.Join("testdata/temp", "invalid.txt")
	initialContent := "This is line 1\nThis is line 2\nThis is line 3\n"
	err = os.WriteFile(relativeFilePath, []byte(initialContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create an invalid diff (missing markers)
	diff := `This doesn't have the right format`

	// Create a mock PatchTool instance
	mockPatchTool := &MockPatchTool{}

	// Apply the patch
	ctx := context.Background()
	_, err = mockPatchTool.Execute(ctx, PatchInput{
		Path: relativeFilePath,
		Diff: diff,
	})

	// Verify the patch failed as expected
	if err == nil {
		t.Errorf("Patch should have failed with invalid diff format")
	}
}

func TestPatchWithNonExistentContent(t *testing.T) {
	// Initialize logger
	if err := log.Init(true); err != nil {
		t.Fatalf("Failed to initialize logger: %v", err)
	}

	// Create a temporary directory for testing
	tempDir := "testdata/temp"
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		err := os.MkdirAll(tempDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create temp directory: %v", err)
		}
	}

	// Create a test file with relative path
	relativeFilePath := filepath.Join("testdata/temp", "nonexistent.txt")
	initialContent := "This is line 1\nThis is line 2\nThis is line 3\n"
	err := os.WriteFile(relativeFilePath, []byte(initialContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create a diff with content that doesn't exist in the file
	diff := `<<<<<<< SEARCH
This content doesn't exist in the file
=======
Replacement content
>>>>>>> REPLACE`

	// Create a mock PatchTool instance
	mockPatchTool := &MockPatchTool{}

	// Apply the patch
	ctx := context.Background()
	_, err = mockPatchTool.Execute(ctx, PatchInput{
		Path: relativeFilePath,
		Diff: diff,
	})

	// Verify the patch failed as expected
	if err == nil {
		t.Errorf("Patch should have failed with non-existent content")
	}

	// Clean up at the end of all tests
	defer os.RemoveAll("testdata/temp")
}
