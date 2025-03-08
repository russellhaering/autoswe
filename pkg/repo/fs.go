package repo

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/russellhaering/autoswe/pkg/log"
	ignore "github.com/sabhiram/go-gitignore"
	"go.uber.org/zap"
)

var (
	SkipDirs = []string{
		".git",
		".autoswe",
		"vendor",
		"node_modules",
		".idea",
		".vscode",
		"bin",
		"dist",
		"build",
	}
	SkipExts = []string{
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
	}
)

type RepoFS struct {
	fs.ReadDirFS
	basePath string // Store the base path explicitly
}

func NewRepoFS(path string) *RepoFS {
	return &RepoFS{
		ReadDirFS: os.DirFS(path).(fs.ReadDirFS),
		basePath:  path,
	}
}

func (r *RepoFS) Path() string {
	return r.basePath
}

func (r *RepoFS) Filter() (FilteredFS, error) {
	bytes, err := fs.ReadFile(r, ".autosweignore")
	if err != nil {
		log.Debug("No .autosweignore file found, using default ignore rules")
	}

	lines := strings.Split(string(bytes), "\n")
	lines = append(lines, SkipDirs...)
	lines = append(lines, SkipExts...)

	gitignore := ignore.CompileIgnoreLines(lines...)

	return &filteredFS{
		ReadDirFS: r.ReadDirFS,
		gitignore: gitignore,
		basePath:  r.basePath, // Use the stored base path directly
	}, nil
}

type FilteredFS interface {
	fs.ReadDirFS
	isFilteredFS()

	// WriteFile writes data to the named file with the given permissions
	// It will return an error if the path is filtered or outside the mounted directory
	WriteFile(name string, data []byte, perm os.FileMode) error

	// Remove removes the named file or empty directory
	// It will return an error if the path is filtered or outside the mounted directory
	Remove(name string) error

	// RemoveAll removes the named file or directory and all its contents if it's a directory
	// It will return an error if the path is filtered or outside the mounted directory
	RemoveAll(name string) error
}

// filteredFS implements FilteredFS and fs.ReadDirFS interfaces to provide file filtering
type filteredFS struct {
	fs.ReadDirFS
	gitignore *ignore.GitIgnore
	basePath  string // Store the base path for validation
}

func (f *filteredFS) isFilteredFS() {}

// isValidUTF8File checks whether the first 512 bytes of a file are valid UTF-8
func (f *filteredFS) isBinaryFile(path string) bool {
	file, err := f.ReadDirFS.Open(path)
	if err != nil {
		return false
	}

	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return false
	}

	if info.IsDir() {
		return false
	}

	// Read up to 512 bytes
	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return false
	}

	// An empty file is valid UTF-8
	if n == 0 {
		return true
	}

	// Check if the bytes are valid UTF-8
	return !utf8.Valid(buf[:n])
}

func (f *filteredFS) Open(name string) (fs.File, error) {
	// Check if the file should be ignored
	if f.shouldIgnore(name) {
		return nil, fs.ErrNotExist
	}

	return f.ReadDirFS.Open(name)
}

// ReadDir reads the named directory and returns a list of directory entries
// filtered according to the ignore configuration
func (f *filteredFS) ReadDir(name string) ([]fs.DirEntry, error) {
	// Check if the directory itself should be ignored
	if f.shouldIgnore(name) {
		return nil, fs.ErrNotExist
	}

	entries, err := f.ReadDirFS.ReadDir(name)
	if err != nil {
		return nil, err
	}

	// Filter out ignored entries
	var filteredEntries []fs.DirEntry
	for _, entry := range entries {
		fullPath := filepath.Join(name, entry.Name())
		if !f.shouldIgnore(fullPath) {
			filteredEntries = append(filteredEntries, entry)
		}
	}

	return filteredEntries, nil
}

// shouldIgnore checks if the given path should be ignored
func (f *filteredFS) shouldIgnore(path string) bool {
	if f.gitignore.MatchesPath(path) {
		return true
	}

	if f.isBinaryFile(path) {
		return true
	}

	return false
}

// WalkDir walks the file tree rooted at root, calling fn for each file or
// directory in the tree, including root, but filtering out ignored files and directories
// as well as files with invalid UTF-8 content
func (f *filteredFS) WalkDir(root string, fn fs.WalkDirFunc) error {
	return fs.WalkDir(f, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fn(path, d, err)
		}

		// We already filter in Open and ReadDir, but we'll double-check here
		// to be completely consistent
		if f.shouldIgnore(path) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		return fn(path, d, nil)
	})
}

// Glob returns the names of all files matching pattern, but filters out ignored files
func (f *filteredFS) Glob(pattern string) ([]string, error) {
	// First, get all matching files
	matches, err := fs.Glob(f.ReadDirFS, pattern)
	if err != nil {
		return nil, err
	}

	// Then filter out ignored files
	var filtered []string
	for _, match := range matches {
		if !f.shouldIgnore(match) {
			filtered = append(filtered, match)
		}
	}

	// Sort for deterministic output
	sort.Strings(filtered)
	return filtered, nil
}

// validatePath checks if the path is valid for modification
// It returns an error if the path:
// 1. Is outside the base directory
// 2. Matches any filter rules
func (f *filteredFS) validatePath(name string) error {
	// Check if path is filtered
	if f.shouldIgnore(name) {
		return fmt.Errorf("path is filtered by ignore rules: %s", name)
	}

	// Ensure the path is within the mounted directory
	cleanPath := filepath.Clean(name)
	if strings.HasPrefix(cleanPath, "..") || strings.Contains(cleanPath, "../") {
		return fmt.Errorf("path attempts to access parent directory: %s", name)
	}

	return nil
}

// WriteFile writes data to the named file
func (f *filteredFS) WriteFile(name string, data []byte, perm os.FileMode) error {
	if err := f.validatePath(name); err != nil {
		log.Warn("Rejected write attempt", zap.String("path", name), zap.Error(err))
		return err
	}

	// Create absolute path by joining with base path
	absPath := filepath.Join(f.basePath, name)

	// Ensure directory exists
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write the file
	return os.WriteFile(absPath, data, perm)
}

// Remove removes the named file or empty directory
func (f *filteredFS) Remove(name string) error {
	if err := f.validatePath(name); err != nil {
		log.Warn("Rejected remove attempt", zap.String("path", name), zap.Error(err))
		return err
	}

	// Create absolute path by joining with base path
	absPath := filepath.Join(f.basePath, name)

	return os.Remove(absPath)
}

// RemoveAll removes the named file or directory and all its contents
func (f *filteredFS) RemoveAll(name string) error {
	if err := f.validatePath(name); err != nil {
		log.Warn("Rejected removeAll attempt", zap.String("path", name), zap.Error(err))
		return err
	}

	// Create absolute path by joining with base path
	absPath := filepath.Join(f.basePath, name)

	// If it's a directory, we need to check if any child would be filtered
	// This prevents removing a directory that contains filtered files
	info, err := os.Stat(absPath)
	if err == nil && info.IsDir() {
		var hasFiltered bool
		err := filepath.WalkDir(absPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			// Convert absolute path back to relative for filter check
			relPath, err := filepath.Rel(f.basePath, path)
			if err != nil {
				return err
			}

			if f.shouldIgnore(relPath) {
				hasFiltered = true
				return filepath.SkipDir
			}

			return nil
		})

		if err != nil {
			return err
		}

		if hasFiltered {
			return fmt.Errorf("directory contains filtered files: %s", name)
		}
	}

	return os.RemoveAll(absPath)
}
