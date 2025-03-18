package db

import (
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"

	bolt "go.etcd.io/bbolt"
)

var (
	documentsBucket = []byte("documents")
	ErrNotFound     = errors.New("document not found")
)

// EmbeddingFunc is a function that converts document contents into a vector
type EmbeddingFunc func(content string) ([]float32, error)

// Document represents a document with content, metadata, and its vector embedding
type Document struct {
	ID       string            `json:"id"`
	Content  string            `json:"content"`
	Metadata map[string]string `json:"metadata"`
	Vector   []float32         `json:"vector,omitempty"`
}

// SearchResult represents a document with its similarity score
type SearchResult struct {
	Document   Document
	Similarity float64
}

// DocumentDB represents a document-oriented vector database
type DocumentDB struct {
	db            *bolt.DB
	embedDocument EmbeddingFunc
}

// NewDocumentDB creates a new document database with the specified embedding function
func NewDocumentDB(path string, embedFn EmbeddingFunc) (*DocumentDB, error) {
	// Open bolt database
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		return nil, err
	}

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(documentsBucket)
		return err
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	return &DocumentDB{
		db:            db,
		embedDocument: embedFn,
	}, nil
}

// Close closes the database
func (ddb *DocumentDB) Close() error {
	return ddb.db.Close()
}

// AddDocument adds a new document to the database
func (ddb *DocumentDB) AddDocument(doc Document) error {
	if doc.ID == "" {
		return errors.New("document ID cannot be empty")
	}

	// Generate embedding for the document
	vector, err := ddb.embedDocument(doc.Content)
	if err != nil {
		return err
	}
	doc.Vector = vector

	return ddb.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(documentsBucket)
		data, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		return b.Put([]byte(doc.ID), data)
	})
}

// GetDocument retrieves a document by ID
func (ddb *DocumentDB) GetDocument(id string) (Document, error) {
	var doc Document
	err := ddb.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(documentsBucket)
		data := b.Get([]byte(id))
		if data == nil {
			return ErrNotFound
		}
		return json.Unmarshal(data, &doc)
	})
	return doc, err
}

// DeleteDocument removes a document from the database
func (ddb *DocumentDB) DeleteDocument(id string) error {
	return ddb.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(documentsBucket)
		return b.Delete([]byte(id))
	})
}

// SearchSimilar finds k documents most similar to the query content
func (ddb *DocumentDB) SearchSimilar(queryContent string, k int) ([]Document, error) {
	queryVector, err := ddb.embedDocument(queryContent)
	if err != nil {
		return nil, err
	}

	type docDistance struct {
		doc      Document
		distance float64
	}
	var results []docDistance

	err = ddb.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(documentsBucket)
		return b.ForEach(func(_, value []byte) error {
			var doc Document
			if err := json.Unmarshal(value, &doc); err != nil {
				return err
			}
			distance := cosineSimilarity(queryVector, doc.Vector)
			results = append(results, docDistance{doc: doc, distance: distance})
			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	// Sort by similarity (higher is better)
	sort.Slice(results, func(i, j int) bool {
		return results[i].distance > results[j].distance
	})

	if k > len(results) {
		k = len(results)
	}

	docs := make([]Document, k)
	for i := 0; i < k; i++ {
		docs[i] = results[i].doc
	}
	return docs, nil
}

// FilterDocuments returns documents that match the given metadata filters
func (ddb *DocumentDB) FilterDocuments(filters map[string]string) ([]Document, error) {
	var matches []Document

	err := ddb.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(documentsBucket)
		return b.ForEach(func(_, value []byte) error {
			var doc Document
			if err := json.Unmarshal(value, &doc); err != nil {
				return err
			}

			// Check if document matches all filters
			match := true
			for k, v := range filters {
				if doc.Metadata[k] != v {
					match = false
					break
				}
			}
			if match {
				matches = append(matches, doc)
			}
			return nil
		})
	})

	return matches, err
}

// cosineSimilarity calculates the cosine similarity between two vectors
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return -1
	}

	var dotProduct float64
	var normA float64
	var normB float64

	for i := 0; i < len(a); i++ {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// ListDocuments returns all documents in the database
func (ddb *DocumentDB) ListDocuments() ([]Document, error) {
	var docs []Document

	err := ddb.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(documentsBucket)
		return b.ForEach(func(k, v []byte) error {
			var doc Document
			if err := json.Unmarshal(v, &doc); err != nil {
				return err
			}
			docs = append(docs, doc)
			return nil
		})
	})

	return docs, err
}

// BatchAddDocuments adds multiple documents in a single transaction
func (ddb *DocumentDB) BatchAddDocuments(docs []Document) error {
	return ddb.db.Batch(func(tx *bolt.Tx) error {
		b := tx.Bucket(documentsBucket)
		for _, doc := range docs {
			if doc.ID == "" {
				return errors.New("document ID cannot be empty")
			}

			// Generate embedding for the document
			vector, err := ddb.embedDocument(doc.Content)
			if err != nil {
				return err
			}
			doc.Vector = vector

			data, err := json.Marshal(doc)
			if err != nil {
				return err
			}

			if err := b.Put([]byte(doc.ID), data); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteDocumentsWithPrefix deletes all documents whose IDs start with the given prefix
func (ddb *DocumentDB) DeleteDocumentsWithPrefix(prefix string) error {
	return ddb.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(documentsBucket)
		c := b.Cursor()

		prefixBytes := []byte(prefix)
		for k, _ := c.Seek(prefixBytes); k != nil && strings.HasPrefix(string(k), prefix); k, _ = c.Next() {
			if err := b.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}

// GetDocumentsWithPrefix returns all documents whose IDs start with the given prefix
func (ddb *DocumentDB) GetDocumentsWithPrefix(prefix string) ([]Document, error) {
	var docs []Document

	err := ddb.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(documentsBucket)
		c := b.Cursor()

		prefixBytes := []byte(prefix)
		for k, v := c.Seek(prefixBytes); k != nil && strings.HasPrefix(string(k), prefix); k, v = c.Next() {
			var doc Document
			if err := json.Unmarshal(v, &doc); err != nil {
				return err
			}
			docs = append(docs, doc)
		}
		return nil
	})

	return docs, err
}

// Count returns the total number of documents in the database
func (ddb *DocumentDB) Count() (int, error) {
	var count int
	err := ddb.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(documentsBucket)
		count = b.Stats().KeyN
		return nil
	})
	return count, err
}

// Query finds documents matching the metadata filters and ranks them by similarity to the query content
func (ddb *DocumentDB) Query(queryContent string, limit int, filters map[string]string) ([]SearchResult, error) {
	queryVector, err := ddb.embedDocument(queryContent)
	if err != nil {
		return nil, err
	}

	var results []SearchResult

	err = ddb.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(documentsBucket)
		return b.ForEach(func(key, value []byte) error {
			var doc Document
			if err := json.Unmarshal(value, &doc); err != nil {
				return err
			}

			// Check if document matches all filters
			match := true
			for k, v := range filters {
				if doc.Metadata[k] != v {
					match = false
					break
				}
			}
			if !match {
				return nil
			}

			similarity := cosineSimilarity(queryVector, doc.Vector)
			results = append(results, SearchResult{
				Document:   doc,
				Similarity: similarity,
			})
			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	// Sort by similarity (higher is better)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})

	if limit > len(results) {
		limit = len(results)
	}

	return results[:limit], nil
}
