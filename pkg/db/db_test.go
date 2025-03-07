package db

import (
	"os"
	"testing"
)

// mockEmbedding is a simple embedding function for testing
func mockEmbedding(content string) ([]float32, error) {
	switch content {
	case "hello world":
		return []float32{1.0, 0.0, 0.0}, nil
	case "hello there":
		return []float32{0.8, 0.2, 0.0}, nil
	case "goodbye world":
		return []float32{0.0, 1.0, 0.0}, nil
	default:
		return []float32{0.0, 0.0, 1.0}, nil
	}
}

func TestDocumentDB(t *testing.T) {
	dbPath := "test.db"
	defer os.Remove(dbPath)

	// Create new DB
	db, err := NewDocumentDB(dbPath, mockEmbedding)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// Test adding and retrieving documents
	doc1 := Document{
		ID:      "doc1",
		Content: "hello world",
		Metadata: map[string]string{
			"type": "greeting",
			"lang": "en",
		},
	}

	// Test AddDocument
	err = db.AddDocument(doc1)
	if err != nil {
		t.Fatalf("Failed to add document: %v", err)
	}

	// Test GetDocument
	retrieved, err := db.GetDocument("doc1")
	if err != nil {
		t.Fatalf("Failed to get document: %v", err)
	}

	if retrieved.ID != doc1.ID {
		t.Errorf("Expected ID %s, got %s", doc1.ID, retrieved.ID)
	}
	if retrieved.Content != doc1.Content {
		t.Errorf("Expected content %s, got %s", doc1.Content, retrieved.Content)
	}

	// Test similar document search
	docs := []Document{
		{ID: "doc2", Content: "hello there", Metadata: map[string]string{"type": "greeting"}},
		{ID: "doc3", Content: "goodbye world", Metadata: map[string]string{"type": "farewell"}},
	}

	for _, doc := range docs {
		if err := db.AddDocument(doc); err != nil {
			t.Fatalf("Failed to add document: %v", err)
		}
	}

	similar, err := db.SearchSimilar("hello world", 2)
	if err != nil {
		t.Fatalf("Failed to search similar documents: %v", err)
	}

	if len(similar) != 2 {
		t.Errorf("Expected 2 results, got %d", len(similar))
	}

	// "hello world" should be most similar to "hello there"
	if similar[0].ID != "doc1" {
		t.Errorf("Expected most similar document to be doc1, got %s", similar[0].ID)
	}
	if similar[1].ID != "doc2" {
		t.Errorf("Expected second most similar document to be doc2, got %s", similar[1].ID)
	}

	// Test metadata filtering
	filtered, err := db.FilterDocuments(map[string]string{"type": "greeting"})
	if err != nil {
		t.Fatalf("Failed to filter documents: %v", err)
	}

	if len(filtered) != 2 {
		t.Errorf("Expected 2 greeting documents, got %d", len(filtered))
	}

	// Test document deletion
	err = db.DeleteDocument("doc1")
	if err != nil {
		t.Fatalf("Failed to delete document: %v", err)
	}

	_, err = db.GetDocument("doc1")
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound after deletion, got %v", err)
	}
}
