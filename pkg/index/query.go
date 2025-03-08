package index

import (
	"context"
	"fmt"
	iofs "io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"github.com/russellhaering/autoswe/pkg/db"
	"github.com/russellhaering/autoswe/pkg/log"
	"go.uber.org/zap"
)

// QueryResult represents the result of a semantic query with AI analysis
type QueryResult struct {
	Answer string `json:"answer"` // The AI-generated answer
}

// CodeExample represents a specific code example from the codebase
type CodeExample struct {
	Path      string `json:"path"`       // Path to the file
	StartLine int    `json:"start_line"` // Starting line number
	EndLine   int    `json:"end_line"`   // Ending line number
	Content   string `json:"content"`    // The actual code content
	Namespace string `json:"namespace"`  // The namespace of the code example
}

const (
	contextLines   = 5  // Number of context lines to add before and after snippets
	mergeThreshold = 10 // Maximum number of lines between snippets to trigger merging
)

// snippetRange represents a range of lines in a file
type snippetRange struct {
	startLine int
	endLine   int
	filePath  string
	path      string
	namespace string
}

// filterResults filters search results by similarity and returns up to 10 results
func filterResults(results []db.SearchResult) []db.SearchResult {
	var goodResults, lowSimilarityResults []db.SearchResult
	for _, result := range results {
		if result.Similarity >= 0.4 {
			goodResults = append(goodResults, result)
		} else {
			lowSimilarityResults = append(lowSimilarityResults, result)
		}

		log.Debug("potential query result",
			zap.String("path", result.Document.ID),
			zap.Float64("similarity", result.Similarity))
	}

	filtered := goodResults

	// If we have less than 3 results, add lower similarity results
	if len(filtered) < 3 && len(lowSimilarityResults) > 0 {
		needed := 3 - len(filtered)
		if needed > len(lowSimilarityResults) {
			needed = len(lowSimilarityResults)
		}
		filtered = append(filtered, lowSimilarityResults[:needed]...)
	}

	// Limit to 20 total results
	if len(filtered) > 20 {
		filtered = filtered[:20]
	}

	return filtered
}

// mergeRanges merges overlapping or nearby snippet ranges
func mergeRanges(ranges []snippetRange) []snippetRange {
	if len(ranges) == 0 {
		return nil
	}

	// Sort ranges by start line
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].startLine < ranges[j].startLine
	})

	var mergedRanges []snippetRange
	current := ranges[0]

	for i := 1; i < len(ranges); i++ {
		r := ranges[i]
		// If this range is close to the current one, merge them
		if r.startLine <= current.endLine+mergeThreshold {
			current.endLine = r.endLine
		} else {
			mergedRanges = append(mergedRanges, current)
			current = r
		}
	}
	mergedRanges = append(mergedRanges, current)

	return mergedRanges
}

// shouldIncludeWholeFile determines if more than half the lines in a file
// would be included by the given ranges
func shouldIncludeWholeFile(ranges []snippetRange, totalLines int) bool {
	if len(ranges) == 0 {
		return false
	}

	// Create a set of included lines
	includedLines := make(map[int]bool)
	for _, r := range ranges {
		for line := r.startLine; line <= r.endLine; line++ {
			includedLines[line] = true
		}
	}

	// Check if more than half the lines would be included
	includedLineCount := len(includedLines)
	log.Debug("checking if should include whole file",
		zap.Int("includedLines", includedLineCount),
		zap.Int("totalLines", totalLines))
	return includedLineCount > totalLines/2
}

// extractSnippet extracts a snippet from file content with context
func extractSnippet(lines []string, r snippetRange) (CodeExample, int, error) {
	// Add context lines, but don't exceed file boundaries
	contextStart := r.startLine - contextLines
	if contextStart < 1 {
		contextStart = 1
	}
	contextEnd := r.endLine + contextLines
	if contextEnd > len(lines) {
		contextEnd = len(lines)
	}

	// Get the code snippet
	snippet := strings.Join(lines[contextStart-1:contextEnd], "\n")

	// Rough token estimation (4 chars per token)
	tokenEstimate := len(snippet) / 4

	return CodeExample{
		Path:      r.path,
		StartLine: contextStart,
		EndLine:   contextEnd,
		Content:   snippet,
		Namespace: r.namespace,
	}, tokenEstimate, nil
}

// buildPrompt builds the prompt for the AI model
func buildPrompt(query string, examples []CodeExample) string {
	var promptBuilder strings.Builder
	promptBuilder.WriteString("Here are some snippets from a codebase and supporting documentation:\n\n")

	for _, example := range examples {
		promptBuilder.WriteString(fmt.Sprintf("File: %s (lines %d-%d, namespace = %s)\n```\n%s\n```\n\n",
			example.Path, example.StartLine, example.EndLine, example.Namespace, example.Content))
	}

	promptBuilder.WriteString(fmt.Sprintf("Query: %s\n\n", query))

	promptBuilder.WriteString(`Please extract the snippets most relevant to the query,
	and return them verbatim. When referencing code in your answer:
1. Prefix each snippet with a path, namespace and line range
2. Reproduce relevant snippets verbatim, wrapped in triple-backtick quotes
3. Do not include any additional text or commentary

If you cannot find any relevant snippets, return "No relevant code found in the codebase for this query."
DO NOT MAKE UP CODE, ONLY RETURN EXACTLY WHAT IS PROVIDED.`)

	return promptBuilder.String()
}

// generateAnswer uses the Gemini API to generate an answer from the prompt
func (i *Indexer) generateAnswer(ctx context.Context, prompt string) (string, error) {
	model := i.gemini.GenerativeModel("gemini-2.0-flash-lite")
	model.SetTemperature(0.1) // Lower temperature for more consistent output

	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return "", fmt.Errorf("failed to generate content: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no content generated")
	}

	text, ok := resp.Candidates[0].Content.Parts[0].(genai.Text)
	if !ok {
		return "", fmt.Errorf("unexpected response type")
	}

	return string(text), nil
}

// collectSnippets processes search results and collects code snippets
func (i *Indexer) collectSnippets(results []db.SearchResult) ([]CodeExample, error) {
	fileRanges := make(map[string]map[string][]snippetRange)

	// Group results by file
	for _, result := range results {
		parts := strings.Split(result.Document.ID, "#")
		if len(parts) != 2 {
			continue
		}

		path := result.Document.Metadata["path"]
		namespace := result.Document.Metadata["namespace"]

		startLine, err := strconv.Atoi(result.Document.Metadata["start_line"])
		if err != nil {
			log.Error("failed to convert start line to int", zap.Error(err))
			continue
		}

		endLine, err := strconv.Atoi(result.Document.Metadata["end_line"])
		if err != nil {
			log.Error("failed to convert end line to int", zap.Error(err))
			continue
		}

		// Initialize inner map if it doesn't exist
		if fileRanges[namespace] == nil {
			fileRanges[namespace] = make(map[string][]snippetRange)
		}

		fileRanges[namespace][path] = append(fileRanges[namespace][path], snippetRange{
			startLine: startLine,
			endLine:   endLine,
			filePath:  path,
			path:      path,
			namespace: namespace,
		})
	}

	var examples []CodeExample
	var totalTokens int

	// Process each file's ranges
	for namespace, files := range fileRanges {
		if i.fss[namespace] == nil {
			log.Warn("namespace not found in fss", zap.String("namespace", namespace))
			continue
		}

		for filePath, ranges := range files {
			content, err := iofs.ReadFile(i.fss[namespace], filePath)
			if err != nil {
				log.Error("failed to read file", zap.Error(err), zap.String("path", filePath))
				continue
			}
			lines := strings.Split(string(content), "\n")

			// Get merged ranges first
			mergedRanges := mergeRanges(ranges)

			// Check if we should include the whole file
			if shouldIncludeWholeFile(mergedRanges, len(lines)) {
				// Include the whole file
				example, tokenEstimate, err := extractSnippet(lines, snippetRange{
					startLine: 1,
					endLine:   len(lines),
					filePath:  filePath,
					path:      ranges[0].path,      // Use the path from the first range
					namespace: ranges[0].namespace, // And its namespace
				})
				if err == nil && totalTokens+tokenEstimate <= 32000 {
					examples = append(examples, example)
					totalTokens += tokenEstimate
				}
			} else {
				// Process individual ranges
				for _, r := range mergedRanges {
					example, tokenEstimate, err := extractSnippet(lines, r)
					if err != nil {
						log.Error("failed to extract snippet", zap.Error(err))
						continue
					}

					if totalTokens+tokenEstimate > 32000 {
						log.Info("exceeded max tokens", zap.Int("totalTokens", totalTokens))
						break
					}
					totalTokens += tokenEstimate

					examples = append(examples, example)
				}
			}
		}
	}

	return examples, nil
}

// Query performs a semantic search and uses Gemini to analyze the results
func (i *Indexer) Query(ctx context.Context, query string) (*QueryResult, error) {
	// Get and filter search results
	results, err := i.Search(ctx, query, 30)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	filteredResults := filterResults(results)
	if len(filteredResults) == 0 {
		return &QueryResult{
			Answer: "No relevant code found in the codebase for this query.",
		}, nil
	}

	// Collect code snippets
	examples, err := i.collectSnippets(filteredResults)
	if err != nil {
		return nil, fmt.Errorf("failed to collect snippets: %w", err)
	}

	// Build prompt and generate answer
	prompt := buildPrompt(query, examples)

	answer, err := i.generateAnswer(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("failed to generate answer: %w", err)
	}

	return &QueryResult{
		Answer: answer,
	}, nil
}
