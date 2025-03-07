package repo

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRepoFS tests the basic functionality of the RepoFS type
func TestRepoFS(t *testing.T) {
	// Create some test files
	tmpDir := t.TempDir()

	// Create files in the root
	mustCreateFile(t, filepath.Join(tmpDir, "file.txt"), "test content")
	mustCreateFile(t, filepath.Join(tmpDir, "file.go"), "package main")

	// Create a RepoFS instance
	repoFS := NewRepoFS(tmpDir)

	// Test that the RepoFS can access files
	file, err := repoFS.Open("file.txt")
	assert.NoError(t, err)
	content, err := io.ReadAll(file)
	assert.NoError(t, err)
	assert.Equal(t, "test content", string(content))
	file.Close()
}

// TestFilteredFS_Filter tests creating a filtered file system
func TestFilteredFS_Filter(t *testing.T) {
	// Create some test files
	tmpDir := t.TempDir()

	// Create files in the root
	mustCreateFile(t, filepath.Join(tmpDir, "file.txt"), "test content")
	mustCreateFile(t, filepath.Join(tmpDir, "file.go"), "package main")
	mustCreateFile(t, filepath.Join(tmpDir, "file.exe"), "binary content")
	mustCreateFile(t, filepath.Join(tmpDir, "image.png"), "image content")

	// Create a node_modules directory with some files
	nodeModulesDir := filepath.Join(tmpDir, "node_modules")
	mustCreateDir(t, nodeModulesDir)
	mustCreateFile(t, filepath.Join(nodeModulesDir, "package.json"), "{}")

	// Create an ignore file
	ignoreContent := "*.exe\n*.png\nnode_modules\n"
	mustCreateFile(t, filepath.Join(tmpDir, ".auto-swe-ignore"), ignoreContent)

	// Create a RepoFS instance
	repoFS := NewRepoFS(tmpDir)

	// Create a filtered FS
	filteredFS, err := repoFS.Filter()
	assert.NoError(t, err)

	// Check that we can access non-ignored files
	_, err = filteredFS.Open("file.txt")
	assert.NoError(t, err)
	_, err = filteredFS.Open("file.go")
	assert.NoError(t, err)

	// Check that ignored files are not accessible
	_, err = filteredFS.Open("file.exe")
	assert.Error(t, err)
	_, err = filteredFS.Open("image.png")
	assert.Error(t, err)
	_, err = filteredFS.Open("node_modules/package.json")
	assert.Error(t, err)
}

// TestFilteredFS_UTF8Validation tests the filtering of files with invalid UTF-8
func TestFilteredFS_UTF8Validation(t *testing.T) {
	// Create some test files
	tmpDir := t.TempDir()

	// Create a valid UTF-8 text file
	validPath := filepath.Join(tmpDir, "valid-utf8.txt")
	mustCreateFile(t, validPath, "This is valid UTF-8 text")

	// Create a file with valid UTF-8 at the beginning but invalid later (should pass)
	validPrefixPath := filepath.Join(tmpDir, "valid-prefix.bin")
	validPrefix := "Valid UTF-8 prefix: "
	content := make([]byte, len(validPrefix)+10)
	copy(content, validPrefix)
	// Add some invalid UTF-8 bytes at the end
	content[len(validPrefix)] = 0xFF
	content[len(validPrefix)+1] = 0xFF
	err := os.WriteFile(validPrefixPath, content, 0644)
	assert.NoError(t, err)

	// Create a file with invalid UTF-8 at the beginning (should be filtered)
	invalidPath := filepath.Join(tmpDir, "invalid-utf8.bin")
	invalidContent := make([]byte, 10)
	invalidContent[0] = 0xFF
	invalidContent[1] = 0xFF
	err = os.WriteFile(invalidPath, invalidContent, 0644)
	assert.NoError(t, err)

	// Create a RepoFS instance
	repoFS := NewRepoFS(tmpDir)

	// Create a filtered FS
	filteredFS, err := repoFS.Filter()
	assert.NoError(t, err)

	// Test valid UTF-8 file
	file, err := filteredFS.Open("valid-utf8.txt")
	assert.NoError(t, err)
	if err == nil {
		file.Close()
	}

	// Test file with valid UTF-8 prefix
	file, err = filteredFS.Open("valid-prefix.bin")
	assert.NoError(t, err)
	if err == nil {
		file.Close()
	}

	// Test file with invalid UTF-8 at the beginning (should be filtered)
	_, err = filteredFS.Open("invalid-utf8.bin")
	assert.Error(t, err)

	// Test directory walking
	var visitedPaths []string
	err = fs.WalkDir(filteredFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		visitedPaths = append(visitedPaths, path)
		return nil
	})
	assert.NoError(t, err)

	// The invalid UTF-8 file should not be in the visited paths
	for _, path := range visitedPaths {
		assert.NotEqual(t, "invalid-utf8.bin", path, "Invalid UTF-8 file should not be visited")
	}
}

// TestFilteredFS_Open tests opening files with the filtered file system
func TestFilteredFS_Open(t *testing.T) {
	// Create some test files
	tmpDir := t.TempDir()

	// Create files in the root
	mustCreateFile(t, filepath.Join(tmpDir, "file.txt"), "test content")
	mustCreateFile(t, filepath.Join(tmpDir, "file.go"), "package main")
	mustCreateFile(t, filepath.Join(tmpDir, "file.exe"), "binary content")
	mustCreateFile(t, filepath.Join(tmpDir, "image.png"), "image content")

	// Create a node_modules directory with some files
	nodeModulesDir := filepath.Join(tmpDir, "node_modules")
	mustCreateDir(t, nodeModulesDir)
	mustCreateFile(t, filepath.Join(nodeModulesDir, "package.json"), "{}")

	// Create an ignore file
	ignoreContent := "*.exe\n*.png\nnode_modules\n"
	mustCreateFile(t, filepath.Join(tmpDir, ".auto-swe-ignore"), ignoreContent)

	// Create a RepoFS instance
	repoFS := NewRepoFS(tmpDir)

	// Create a filtered FS
	filteredFS, err := repoFS.Filter()
	assert.NoError(t, err)

	// Test cases
	tests := []struct {
		name          string
		path          string
		shouldSucceed bool
	}{
		{"regular file", "file.txt", true},
		{"go file", "file.go", true},
		{"executable file", "file.exe", false},
		{"image file", "image.png", false},
		{"node_modules file", "node_modules/package.json", false},
		{"non-existent file", "nonexistent.txt", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file, err := filteredFS.Open(tc.path)
			if tc.shouldSucceed {
				assert.NoError(t, err)
				if err == nil {
					file.Close()
				}
			} else {
				assert.Error(t, err)
			}
		})
	}
}

// TestFilteredFS_ReadDir tests reading directories with the filtered file system
func TestFilteredFS_ReadDir(t *testing.T) {
	// Create some test files
	tmpDir := t.TempDir()

	// Create files in the root
	mustCreateFile(t, filepath.Join(tmpDir, "file.txt"), "test content")
	mustCreateFile(t, filepath.Join(tmpDir, "file.go"), "package main")
	mustCreateFile(t, filepath.Join(tmpDir, "file.exe"), "binary content")
	mustCreateFile(t, filepath.Join(tmpDir, "image.png"), "image content")

	// Create a node_modules directory with some files
	nodeModulesDir := filepath.Join(tmpDir, "node_modules")
	mustCreateDir(t, nodeModulesDir)
	mustCreateFile(t, filepath.Join(nodeModulesDir, "package.json"), "{}")

	// Create a src directory with some files
	srcDir := filepath.Join(tmpDir, "src")
	mustCreateDir(t, srcDir)
	mustCreateFile(t, filepath.Join(srcDir, "main.go"), "package main")
	mustCreateFile(t, filepath.Join(srcDir, "util.go"), "package main")

	// Create an ignore file
	ignoreContent := "*.exe\n*.png\nnode_modules\n"
	mustCreateFile(t, filepath.Join(tmpDir, ".auto-swe-ignore"), ignoreContent)

	// Create a RepoFS instance
	repoFS := NewRepoFS(tmpDir)

	// Create a filtered FS
	filteredFS, err := repoFS.Filter()
	assert.NoError(t, err)

	// Read the root directory
	entries, err := filteredFS.ReadDir(".")
	assert.NoError(t, err)

	// We should only see the non-ignored files and directories
	expectedNames := map[string]bool{
		"file.txt":         true,
		"file.go":          true,
		".auto-swe-ignore": true,
		"src":              true,
	}

	assert.Equal(t, len(expectedNames), len(entries))

	for _, entry := range entries {
		assert.True(t, expectedNames[entry.Name()], "Unexpected entry: "+entry.Name())
	}

	// Read the src directory
	entries, err = filteredFS.ReadDir("src")
	assert.NoError(t, err)
	assert.Equal(t, 2, len(entries))

	expectedNames = map[string]bool{
		"main.go": true,
		"util.go": true,
	}

	for _, entry := range entries {
		assert.True(t, expectedNames[entry.Name()], "Unexpected entry: "+entry.Name())
	}
}

// TestFilteredFS_WalkDir tests walking the file tree with the filtered file system
func TestFilteredFS_WalkDir(t *testing.T) {
	// Create some test files
	tmpDir := t.TempDir()

	// Create files in the root
	mustCreateFile(t, filepath.Join(tmpDir, "file.txt"), "test content")
	mustCreateFile(t, filepath.Join(tmpDir, "file.go"), "package main")
	mustCreateFile(t, filepath.Join(tmpDir, "file.exe"), "binary content")
	mustCreateFile(t, filepath.Join(tmpDir, "image.png"), "image content")

	// Create a node_modules directory with some files
	nodeModulesDir := filepath.Join(tmpDir, "node_modules")
	mustCreateDir(t, nodeModulesDir)
	mustCreateFile(t, filepath.Join(nodeModulesDir, "package.json"), "{}")

	// Create a src directory with some files
	srcDir := filepath.Join(tmpDir, "src")
	mustCreateDir(t, srcDir)
	mustCreateFile(t, filepath.Join(srcDir, "main.go"), "package main")
	mustCreateFile(t, filepath.Join(srcDir, "util.go"), "package main")

	// Create assets directory with image files
	assetsDir := filepath.Join(srcDir, "assets")
	mustCreateDir(t, assetsDir)
	mustCreateFile(t, filepath.Join(assetsDir, "logo.png"), "PNG image")
	mustCreateFile(t, filepath.Join(assetsDir, "logo.jpg"), "JPG image")

	// Create an ignore file
	ignoreContent := "*.exe\n*.png\nnode_modules\n"
	mustCreateFile(t, filepath.Join(tmpDir, ".auto-swe-ignore"), ignoreContent)

	// Create a RepoFS instance
	repoFS := NewRepoFS(tmpDir)

	// Create a filtered FS
	filteredFS, err := repoFS.Filter()
	assert.NoError(t, err)

	// Walk the directory tree
	visitedPaths := make(map[string]bool)
	err = fs.WalkDir(filteredFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		visitedPaths[path] = true
		return nil
	})
	assert.NoError(t, err)

	// Check that we visited the expected paths
	expectedPaths := []string{
		".",
		"file.txt",
		"file.go",
		".auto-swe-ignore",
		"src",
		"src/main.go",
		"src/util.go",
		"src/assets",
		"src/assets/logo.jpg",
	}

	for _, path := range expectedPaths {
		assert.True(t, visitedPaths[path], "Did not visit: "+path)
	}

	// Check that we did not visit ignored paths
	ignoredPaths := []string{
		"file.exe",
		"image.png",
		"node_modules",
		"node_modules/package.json",
		"src/assets/logo.png",
	}

	for _, path := range ignoredPaths {
		assert.False(t, visitedPaths[path], "Should not have visited: "+path)
	}
}

// Helper functions

// mustCreateFile creates a file with the given content.
// It fails the test if the file cannot be created.
func mustCreateFile(t *testing.T, path, content string) {
	// Create parent directories if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("Failed to create directory %s: %v", dir, err)
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create file %s: %v", path, err)
	}
}

// mustCreateDir creates a directory.
// It fails the test if the directory cannot be created.
func mustCreateDir(t *testing.T, path string) {
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("Failed to create directory %s: %v", path, err)
	}
}
