// Package simplediff provides a simple implementation for applying diffs
// in a search and replace format.
package simplediff

import (
	"errors"
	"fmt"
	"strings"
)

const (
	// SearchMarker is the marker that indicates the start of content to search for
	SearchMarker = "<<<<<<< SEARCH"
	// SeparatorMarker is the marker that separates search and replace content
	SeparatorMarker = "======="
	// ReplaceMarker is the marker that indicates the end of content to replace with
	ReplaceMarker = ">>>>>>> REPLACE"
)

var (
	// ErrInvalidDiffFormat is returned when the diff is not correctly formatted
	ErrInvalidDiffFormat = errors.New("invalid diff format")
	// ErrSearchNotFound is returned when the search content is not found in the target text
	ErrSearchNotFound = errors.New("search content not found in target")
)

// ParseDiff parses a diff string in the format of:
// <<<<<<< SEARCH
// content to search for
// =======
// content to replace with
// >>>>>>> REPLACE
// Returns the search content and replace content
func ParseDiff(diff string) (search string, replace string, err error) {
	lines := strings.Split(diff, "\n")

	// Validate basic structure
	if len(lines) < 3 {
		return "", "", ErrInvalidDiffFormat
	}

	searchStart := -1
	separatorIndex := -1
	replaceEnd := -1

	for i, line := range lines {
		if line == SearchMarker {
			searchStart = i
		} else if line == SeparatorMarker {
			separatorIndex = i
		} else if line == ReplaceMarker {
			replaceEnd = i
			break
		}
	}

	// Verify we found all markers in the right order
	if searchStart == -1 || separatorIndex == -1 || replaceEnd == -1 ||
		searchStart >= separatorIndex || separatorIndex >= replaceEnd {
		return "", "", ErrInvalidDiffFormat
	}

	// Extract the search and replace content
	searchContent := strings.Join(lines[searchStart+1:separatorIndex], "\n")
	replaceContent := strings.Join(lines[separatorIndex+1:replaceEnd], "\n")

	return searchContent, replaceContent, nil
}

// ApplyDiff applies a diff to the given file content
func ApplyDiff(fileContent, diff string) (string, error) {
	search, replace, err := ParseDiff(diff)
	if err != nil {
		return "", err
	}

	// Split the content at the search string
	parts := strings.SplitN(fileContent, search, 2)
	if len(parts) != 2 {
		return "", ErrSearchNotFound
	}

	// Special case for when we're removing a line entirely (replace is empty)
	if replace == "" && strings.HasSuffix(parts[0], "\n") && strings.HasPrefix(parts[1], "\n") {
		// Remove one of the newlines to avoid empty lines
		parts[1] = strings.TrimPrefix(parts[1], "\n")
	}

	// Join the parts with the replacement in between
	result := parts[0] + replace + parts[1]
	return result, nil
}

// ApplyMultipleDiffs applies multiple diffs to a file content
// The diffs will be applied sequentially
func ApplyMultipleDiffs(fileContent string, diffs []string) (string, error) {
	result := fileContent

	for i, diff := range diffs {
		modified, err := ApplyDiff(result, diff)
		if err != nil {
			return "", fmt.Errorf("failed to apply diff %d: %w", i, err)
		}
		result = modified
	}

	return result, nil
}
