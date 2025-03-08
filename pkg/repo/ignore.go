// Package repo provides utilities for ignoring files and directories
package repo

import (
	"path/filepath"
	"strings"
)

// Config represents configuration for file and directory filtering
type Config struct {
	// SkipDirs are directories to skip during file operations
	SkipDirs []string

	// SkipExts are file extensions to skip during file operations
	SkipExts []string
}

// DefaultConfig provides standard filtering rules for file operations
var DefaultConfig = Config{
	SkipDirs: []string{
		".git",
		".autoswe",
		"vendor",
		"node_modules",
		".idea",
		".vscode",
		"bin",
		"dist",
		"build",
	},
	SkipExts: []string{
		// Binaries
		".exe", ".dll", ".so", ".dylib",
		// Images
		".png", ".jpg", ".jpeg", ".gif", ".ico", ".svg",
		// Documents
		".pdf", ".doc", ".docx",
		// Archives
		".zip", ".tar", ".gz", ".rar", ".7z",
		// Other binaries
		".bin", ".dat", ".db",
		// Go junk
		"go.sum",
	},
}

// ShouldIgnore determines if a path should be skipped based on ignore configuration
func ShouldIgnore(path string, config Config) bool {
	// Skip directories in the skip list
	for _, dir := range config.SkipDirs {
		// Check if the path starts with the dir
		if strings.HasPrefix(path, dir) {
			return true
		}

		// Check if the path contains /dir/ or \dir\ (platform independent)
		if strings.Contains(path, string(filepath.Separator)+dir+string(filepath.Separator)) {
			return true
		}

		// Check if the path ends with /dir or \dir
		if strings.HasSuffix(path, string(filepath.Separator)+dir) {
			return true
		}
	}

	// Skip files with extensions in the skip list
	ext := filepath.Ext(path)
	for _, skipExt := range config.SkipExts {
		if ext == skipExt {
			return true
		}
	}

	return false
}

// PathHasPrefix is a safe replacement for checking if a path has a specific prefix
func PathHasPrefix(path, prefix string) bool {
	// Clean both paths to ensure consistent formatting
	path = filepath.Clean(path)
	prefix = filepath.Clean(prefix)

	// Check if paths match at component boundaries
	if len(path) < len(prefix) {
		return false
	}
	return path == prefix || (len(path) > len(prefix) && path[len(prefix)] == filepath.Separator && path[:len(prefix)] == prefix)
}
