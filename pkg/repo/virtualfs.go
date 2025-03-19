package repo

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// VirtualFile represents a file in the VirtualFS
type VirtualFile struct {
	name    string
	content []byte
	modTime time.Time
	size    int64
	isDir   bool
	offset  int64
}

// Ensure VirtualFile implements fs.File and fs.FileInfo
var _ fs.File = (*VirtualFile)(nil)
var _ fs.FileInfo = (*VirtualFile)(nil)

// Stat implements fs.File
func (f *VirtualFile) Stat() (fs.FileInfo, error) {
	return f, nil
}

// Read implements fs.File
func (f *VirtualFile) Read(b []byte) (int, error) {
	if f.isDir {
		return 0, fmt.Errorf("cannot read from directory")
	}

	if f.offset >= int64(len(f.content)) {
		return 0, io.EOF
	}

	n := copy(b, f.content[f.offset:])
	f.offset += int64(n)
	return n, nil
}

// Close implements fs.File
func (f *VirtualFile) Close() error {
	return nil
}

// Name implements fs.FileInfo
func (f *VirtualFile) Name() string {
	return f.name
}

// Size implements fs.FileInfo
func (f *VirtualFile) Size() int64 {
	return f.size
}

// Mode implements fs.FileInfo
func (f *VirtualFile) Mode() fs.FileMode {
	if f.isDir {
		return fs.ModeDir | 0555
	}
	return 0444
}

// ModTime implements fs.FileInfo
func (f *VirtualFile) ModTime() time.Time {
	return f.modTime
}

// IsDir implements fs.FileInfo
func (f *VirtualFile) IsDir() bool {
	return f.isDir
}

// Sys implements fs.FileInfo
func (f *VirtualFile) Sys() interface{} {
	return nil
}

// DirEntry implements fs.DirEntry
type VirtualDirEntry struct {
	fileInfo fs.FileInfo
}

// Name implements fs.DirEntry
func (d *VirtualDirEntry) Name() string {
	return d.fileInfo.Name()
}

// IsDir implements fs.DirEntry
func (d *VirtualDirEntry) IsDir() bool {
	return d.fileInfo.IsDir()
}

// Type implements fs.DirEntry
func (d *VirtualDirEntry) Type() fs.FileMode {
	return d.fileInfo.Mode().Type()
}

// Info implements fs.DirEntry
func (d *VirtualDirEntry) Info() (fs.FileInfo, error) {
	return d.fileInfo, nil
}

// VirtualFS implements fs.ReadDirFS and provides a virtual filesystem where all files
// appear at the root level, regardless of their original location
type VirtualFS struct {
	files map[string]*VirtualFile
}

// Ensure VirtualFS implements fs.ReadDirFS and FilteredFS
var _ fs.ReadDirFS = (*VirtualFS)(nil)

// NewVirtualFS creates a new virtual filesystem
func NewVirtualFS() *VirtualFS {
	return &VirtualFS{
		files: make(map[string]*VirtualFile),
	}
}

// AddFile adds a file to the virtual filesystem by reading it from the real filesystem
func (vfs *VirtualFS) AddFile(sourcePath string) error {
	// Read file content
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Get file info
	info, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Add file to virtual filesystem with just the base name
	baseName := filepath.Base(sourcePath)
	vfs.files[baseName] = &VirtualFile{
		name:    baseName,
		content: content,
		modTime: info.ModTime(),
		size:    info.Size(),
		isDir:   false,
		offset:  0,
	}

	return nil
}

// Open implements fs.FS
func (vfs *VirtualFS) Open(name string) (fs.File, error) {
	// Handle special case of root directory
	if name == "." {
		return &VirtualFile{
			name:    ".",
			content: nil,
			modTime: time.Now(),
			size:    0,
			isDir:   true,
		}, nil
	}

	// Clean the path to handle "./" prefixes, etc.
	name = filepath.Clean(name)

	// Check if file exists
	file, ok := vfs.files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}

	// Return a new copy of the file with reset offset to ensure concurrent reads work
	fileCopy := *file
	fileCopy.offset = 0
	return &fileCopy, nil
}

// ReadDir implements fs.ReadDirFS
func (vfs *VirtualFS) ReadDir(name string) ([]fs.DirEntry, error) {
	// Only support reading the root directory
	if name != "." {
		return nil, fs.ErrNotExist
	}

	entries := make([]fs.DirEntry, 0, len(vfs.files))
	for _, file := range vfs.files {
		entries = append(entries, &VirtualDirEntry{fileInfo: file})
	}

	return entries, nil
}

// Filter returns a FilteredFS implementation for the virtual filesystem
func (vfs *VirtualFS) Filter() (FilteredFS, error) {
	return &virtualFilteredFS{VirtualFS: vfs}, nil
}

// virtualFilteredFS implements FilteredFS for VirtualFS
type virtualFilteredFS struct {
	*VirtualFS
}

func (f *virtualFilteredFS) isFilteredFS() {}

// WriteFile implements FilteredFS.WriteFile
func (f *virtualFilteredFS) WriteFile(name string, data []byte, perm os.FileMode) error {
	return fmt.Errorf("write operations not supported on virtual filesystem")
}

// Remove implements FilteredFS.Remove
func (f *virtualFilteredFS) Remove(name string) error {
	return fmt.Errorf("remove operations not supported on virtual filesystem")
}

// RemoveAll implements FilteredFS.RemoveAll
func (f *virtualFilteredFS) RemoveAll(name string) error {
	return fmt.Errorf("remove operations not supported on virtual filesystem")
}
