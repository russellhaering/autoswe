package index

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/generative-ai-go/genai"
	"github.com/russellhaering/auto-swe/pkg/db"
	"github.com/russellhaering/auto-swe/pkg/log"
	"github.com/russellhaering/auto-swe/pkg/repo"
	"go.uber.org/zap"
)

const (
	StoragePath = ".auto-swe"
	DBFileName  = "db"

	RepoNamespace         = "repo"
	ExtraContextNamespace = "extra"
)

// Metadata represents additional information about a document
type Metadata struct {
	Path     string    `json:"path"`
	Language string    `json:"language"`
	ModTime  time.Time `json:"mod_time"`
	Size     int64     `json:"size"`
}

// Document represents a document in the index with metadata
type Document struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Embedding []float32 `json:"embedding"`
	Metadata  Metadata  `json:"metadata"`
}

type FSContextMap map[string]repo.FilteredFS

// Indexer manages the vector-based code index
type Indexer struct {
	fss    FSContextMap
	db     *db.DocumentDB
	gemini *genai.Client
}

// NewIndexer creates a new code indexer with the given configuration
func NewIndexer(ctx context.Context, gemini *genai.Client, fss FSContextMap) (*Indexer, error) {
	// Create storage directory if it doesn't exist
	if err := os.MkdirAll(StoragePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	embeddingModel := gemini.EmbeddingModel("text-embedding-004")

	// Initialize document database
	docDB, err := db.NewDocumentDB(filepath.Join(StoragePath, "db"), func(content string) ([]float32, error) {
		embedding, err := embeddingModel.EmbedContent(ctx, genai.Text(content))
		if err != nil {
			return nil, fmt.Errorf("failed to embed text: %w", err)
		}

		return embedding.Embedding.Values, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create document database: %w", err)
	}

	indexer := &Indexer{
		fss:    fss,
		db:     docDB,
		gemini: gemini,
	}

	err = indexer.UpdateIndex(ctx)
	if err != nil {
		indexer.Close()
		return nil, fmt.Errorf("failed to update index: %w", err)
	}

	return indexer, nil
}

// Close releases resources used by the indexer
func (i *Indexer) Close() error {
	return i.db.Close()
}

// detectLanguage detects the programming language of a file based on its extension
func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "Go"
	case ".js", ".jsx":
		return "JavaScript"
	case ".ts", ".tsx":
		return "TypeScript"
	case ".py":
		return "Python"
	case ".java":
		return "Java"
	case ".rb":
		return "Ruby"
	case ".php":
		return "PHP"
	case ".rs":
		return "Rust"
	case ".c":
		return "C"
	case ".cpp", ".cc", ".cxx":
		return "C++"
	case ".h", ".hpp":
		return "C/C++ Header"
	default:
		return "Unknown"
	}
}

type FileRef struct {
	Namespace string `json:"namespace"`
	Path      string `json:"path"`
}

func ParseFileRef(id string) (FileRef, error) {
	parts := strings.Split(id, ":")
	if len(parts) != 2 {
		return FileRef{}, fmt.Errorf("invalid file ref: %s", id)
	}

	if strings.Contains(parts[1], "#") {
		parts[1] = strings.Split(parts[1], "#")[0]
	}

	return FileRef{Namespace: parts[0], Path: parts[1]}, nil
}

// deleteFileEntries removes all existing entries for a given file path from the index
func (i *Indexer) deleteFileEntries(ctx context.Context, path string) error {
	return i.db.DeleteDocumentsWithPrefix(path)
}

// GetIndexedFiles returns a sorted list of all file paths that have been indexed
func (i *Indexer) GetIndexedFiles(ctx context.Context) ([]FileRef, error) {
	// Get all documents with is_file_entry=true in metadata
	docs, err := i.db.FilterDocuments(map[string]string{"is_file_entry": "true"})
	if err != nil {
		return nil, fmt.Errorf("failed to query file entries: %w", err)
	}

	// Extract and sort paths
	paths := make([]FileRef, 0, len(docs))
	for _, doc := range docs {
		paths = append(paths, FileRef{
			Namespace: doc.Metadata["namespace"],
			Path:      doc.Metadata["path"],
		})
	}

	return paths, nil
}

// indexFile indexes a single file and adds it to the collection
func (i *Indexer) indexFile(ctx context.Context, path string) error {
	// Get file info
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to get info for %s: %w", path, err)
	}

	// Get metadata
	language := detectLanguage(path)

	log.Info("Indexing file",
		zap.String("path", path))

	// Delete any existing entries for this file
	prefix := ComputeID("repo", path, -1)
	if err := i.db.DeleteDocumentsWithPrefix(prefix); err != nil {
		return fmt.Errorf("failed to delete existing entries: %w", err)
	}

	// Create a file-level entry to track indexing state
	fileDoc := db.Document{
		ID:      prefix,
		Content: "", // Empty content for file-level entries
		Metadata: map[string]string{
			"path":          path,
			"language":      language,
			"mod_time":      info.ModTime().Format(time.RFC3339),
			"size":          fmt.Sprintf("%d", info.Size()),
			"is_file_entry": "true",
			"namespace":     "repo",
		},
	}

	if err := i.db.AddDocument(fileDoc); err != nil {
		return fmt.Errorf("failed to add file-level entry: %w", err)
	}

	// Extract semantic summaries
	summaries, err := i.ExtractFileSummaries(ctx, path)
	if err != nil {
		return fmt.Errorf("failed to extract summaries from file: %w", err)
	}

	// Create documents for each summary
	var docs []db.Document
	for idx, summary := range summaries {
		doc := db.Document{
			ID:      ComputeID("repo", path, idx),
			Content: summary.Summary,
			Metadata: map[string]string{
				"path":          path,
				"language":      language,
				"mod_time":      info.ModTime().Format(time.RFC3339),
				"size":          fmt.Sprintf("%d", info.Size()),
				"start_line":    fmt.Sprintf("%d", summary.ContentSpan.StartLine),
				"end_line":      fmt.Sprintf("%d", summary.ContentSpan.EndLine),
				"is_file_entry": "false",
				"namespace":     "repo",
			},
		}
		docs = append(docs, doc)
	}

	// Batch add all summary documents
	if len(docs) > 0 {
		if err := i.db.BatchAddDocuments(docs); err != nil {
			return fmt.Errorf("failed to add summary documents: %w", err)
		}
	}

	return nil
}

// ComputeID generates a consistent ID for indexing. If idx is < 0, it generates a file-level ID.
// If idx is >= 0, it generates a summary-level ID with the index appended.
func ComputeID(namespace string, path string, idx int) string {
	if idx < 0 {
		return fmt.Sprintf("%s:%s", namespace, path)
	}
	return fmt.Sprintf("%s:%s#%d", namespace, path, idx)
}

// needsReindexing checks if a file needs to be re-indexed by comparing its mod time
// with the last indexed time stored in the metadata
func (i *Indexer) needsReindexing(ctx context.Context, namespace, path string, info fs.FileInfo) (bool, error) {
	// Get the file-level entry
	fileID := ComputeID(namespace, path, -1)
	doc, err := i.db.GetDocument(fileID)
	if err != nil {
		log.Debug("File needs indexing - no existing file-level entry found",
			zap.String("path", path),
			zap.String("namespace", namespace))
		return true, nil // If no file-level entry exists, needs indexing
	}

	// Get the mod time from metadata
	lastModTime, err := time.Parse(time.RFC3339, doc.Metadata["mod_time"])
	if err != nil {
		log.Debug("File needs indexing - failed to parse last mod time",
			zap.String("path", path),
			zap.String("namespace", namespace),
			zap.Error(err))
		return true, fmt.Errorf("failed to parse last mod time: %w", err)
	}

	fileModTime := info.ModTime()

	// Compare modification times using Unix timestamps to avoid precision issues
	needsUpdate := fileModTime.Unix() > lastModTime.Unix()

	if needsUpdate {
		log.Debug("File needs update",
			zap.String("path", path),
			zap.String("namespace", namespace),
			zap.Time("file_mod_time", fileModTime),
			zap.Int64("file_mod_time_nano", fileModTime.UnixNano()),
			zap.Time("last_indexed", lastModTime),
			zap.Int64("last_indexed_nano", lastModTime.UnixNano()))
	}

	return needsUpdate, nil
}

// CleanupDeletedFiles removes index entries for files that no longer exist
func (i *Indexer) CleanupDeletedFiles(ctx context.Context) error {
	// Get all indexed files
	indexedFiles, err := i.GetIndexedFiles(ctx)
	if err != nil {
		return fmt.Errorf("failed to get indexed files: %w", err)
	}

	for _, ref := range indexedFiles {
		fsys, ok := i.fss[ref.Namespace]
		if !ok {
			continue
		}

		// Check if file still exists
		if _, err := iofs.Stat(fsys, ref.Path); os.IsNotExist(err) {
			log.Info("Removing index entries for deleted file", zap.String("path", ref.Path))
			prefix := ComputeID(ref.Namespace, ref.Path, -1)
			if err := i.deleteFileEntries(ctx, prefix); err != nil {
				log.Warn("Failed to delete entries for deleted file", zap.String("path", prefix), zap.Error(err))
			}
		}
	}

	return nil
}

// UpdateIndex updates the index with changes since the last indexing
func (i *Indexer) UpdateIndex(ctx context.Context) error {
	for namespace, fsys := range i.fss {
		err := iofs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			// Get file info for mod time check
			info, err := d.Info()
			if err != nil {
				log.Warn("Failed to get info for file",
					zap.String("path", path),
					zap.Error(err))
				return nil
			}

			// Skip indexing directories
			if info.IsDir() {
				return nil
			}

			// Check if file needs re-indexing
			needsUpdate, err := i.needsReindexing(ctx, namespace, path, info)
			if err != nil {
				log.Warn("Failed to check if file needs re-indexing",
					zap.String("path", path),
					zap.Error(err))
				return nil
			}

			if !needsUpdate {
				return nil
			}

			if err := i.indexFile(ctx, path); err != nil {
				log.Warn("Failed to index file",
					zap.String("path", path),
					zap.Error(err))
			}

			return nil
		})

		if err != nil {
			return fmt.Errorf("failed to walk directory: %w", err)
		}
	}

	// Clean up entries for deleted files
	if err := i.CleanupDeletedFiles(ctx); err != nil {
		log.Error("Failed to cleanup deleted files", zap.Error(err))
		// Continue anyway as this is not a fatal error
	}

	return nil
}

// Search performs a semantic search over the indexed codebase
func (i *Indexer) Search(ctx context.Context, query string, queryLimit int) ([]db.SearchResult, error) {

	// Get total document count
	count, err := i.db.Count()
	if err != nil {
		return nil, fmt.Errorf("failed to get document count: %w", err)
	}

	if queryLimit > count {
		queryLimit = count
	}

	// Search for similar documents with metadata filter
	searchResults, err := i.db.Query(query, queryLimit, map[string]string{
		"is_file_entry": "false",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to search documents: %w", err)
	}

	return searchResults, nil
}

// ContentSpan represents a range of lines in a file
type ContentSpan struct {
	StartLine int `json:"start_line"`
	EndLine   int `json:"end_line"`
}

// ContentSummary represents a semantic summary of a portion of a file
type ContentSummary struct {
	Summary     string      `json:"summary"`
	ContentSpan ContentSpan `json:"span"`
}

// ExtractFileSummaries uses Gemini to generate semantic summaries of file contents
func (i *Indexer) ExtractFileSummaries(ctx context.Context, path string) ([]ContentSummary, error) {
	// Read the file
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Add line numbers to the content
	lines := strings.Split(string(content), "\n")
	numberedContent := strings.Builder{}
	for idx, line := range lines {
		numberedContent.WriteString(fmt.Sprintf("%4d | %s\n", idx+1, line))
	}

	model := i.gemini.GenerativeModel("gemini-2.0-flash-lite")
	model.SetTemperature(0.1)       // Lower temperature for more consistent output
	model.SetMaxOutputTokens(32768) // Set maximum token limit to 32k

	// Configure structured output
	model.ResponseMIMEType = "application/json"
	model.ResponseSchema = &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"summaries": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"summary": {
							Type:        genai.TypeString,
							Description: "A clear, concise description in plain English of what this code element or section does",
						},
						"start_line": {
							Type:        genai.TypeInteger,
							Description: "The starting line number of this element",
						},
						"end_line": {
							Type:        genai.TypeInteger,
							Description: "The ending line number of this element",
						},
					},
					Required: []string{"summary", "start_line", "end_line"},
				},
			},
		},
		Required: []string{"summaries"},
	}

	prompt := fmt.Sprintf(`Analyze this file and create semantic summaries of its contents.

For code files:
- Identify and describe each important element (functions, structs, types, etc)
- Explain what each element does in clear, concise English
- Include the exact line ranges for each element
- NEVER use placeholders like "[previous code remains the same]" - always provide exact code
- When showing code changes or examples, include sufficient context to make it clear where they belong

For documentation files:
- Break down the content into logical sections
- Summarize the key points of each section
- Include the exact line ranges for each section
- NEVER use placeholders or summaries - always quote exact text

File contents:

%s`, numberedContent.String())

	// Generate the content
	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return nil, fmt.Errorf("failed to generate content: %w", err)
	}

	if len(resp.Candidates) == 0 {
		return nil, fmt.Errorf("no content generated")
	}

	candidate := resp.Candidates[0]
	if len(candidate.Content.Parts) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	// Extract the text response
	text, ok := candidate.Content.Parts[0].(genai.Text)
	if !ok {
		return nil, fmt.Errorf("expected text response, got %T", candidate.Content.Parts[0])
	}

	// Parse the JSON response
	var result struct {
		Summaries []struct {
			Summary   string `json:"summary"`
			StartLine int    `json:"start_line"`
			EndLine   int    `json:"end_line"`
		} `json:"summaries"`
	}

	if err := json.Unmarshal([]byte(string(text)), &result); err != nil {
		return nil, fmt.Errorf("failed to parse response as JSON: %w", err)
	}

	if len(result.Summaries) == 0 {
		return nil, fmt.Errorf("no summaries found in response")
	}

	// Convert to ContentSummary objects and validate
	var summaries []ContentSummary
	for i, s := range result.Summaries {
		// Validate line numbers
		if s.StartLine < 1 || s.EndLine < s.StartLine || s.EndLine > len(lines) {
			return nil, fmt.Errorf("invalid line range %d-%d at index %d", s.StartLine, s.EndLine, i)
		}

		summaries = append(summaries, ContentSummary{
			Summary: s.Summary,
			ContentSpan: ContentSpan{
				StartLine: s.StartLine,
				EndLine:   s.EndLine,
			},
		})
	}

	return summaries, nil
}
