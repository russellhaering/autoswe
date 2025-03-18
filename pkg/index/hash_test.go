package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComputeFileHash(t *testing.T) {
	// Create a temporary file with known content
	tempDir := t.TempDir()
	testFilePath := filepath.Join(tempDir, "testfile.txt")
	testContent := "This is a test file for hashing."

	err := os.WriteFile(testFilePath, []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Compute hash
	hash1, err := ComputeFileHash(testFilePath)
	if err != nil {
		t.Fatalf("Failed to compute hash: %v", err)
	}

	if hash1 == "" {
		t.Fatal("Hash should not be empty")
	}

	// Compute hash again to verify consistency
	hash2, err := ComputeFileHash(testFilePath)
	if err != nil {
		t.Fatalf("Failed to compute hash second time: %v", err)
	}

	if hash1 != hash2 {
		t.Fatalf("Hashes should be identical for the same file. Got %s and %s", hash1, hash2)
	}

	// Modify the file and check that hash changes
	newContent := "This is a modified test file for hashing."
	err = os.WriteFile(testFilePath, []byte(newContent), 0644)
	if err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	hash3, err := ComputeFileHash(testFilePath)
	if err != nil {
		t.Fatalf("Failed to compute hash after modification: %v", err)
	}

	if hash1 == hash3 {
		t.Fatal("Hash should change when file content changes")
	}
}
