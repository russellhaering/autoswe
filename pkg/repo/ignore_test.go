package repo

import (
	"path/filepath"
	"testing"
)

func TestShouldIgnore(t *testing.T) {
	testCases := []struct {
		name     string
		path     string
		config   Config
		expected bool
	}{
		{
			name: "skip directory direct match",
			path: ".git",
			config: Config{
				SkipDirs: []string{".git"},
			},
			expected: true,
		},
		{
			name: "skip directory with separator suffix",
			path: "vendor/some/path",
			config: Config{
				SkipDirs: []string{"vendor"},
			},
			expected: true,
		},
		{
			name: "skip directory in middle of path",
			path: "src/node_modules/lib",
			config: Config{
				SkipDirs: []string{"node_modules"},
			},
			expected: true,
		},
		{
			name: "don't skip normal directory",
			path: "src/app/lib",
			config: Config{
				SkipDirs: []string{".git", "node_modules"},
			},
			expected: false,
		},
		{
			name: "skip file extension",
			path: "image.png",
			config: Config{
				SkipExts: []string{".png"},
			},
			expected: true,
		},
		{
			name: "don't skip normal file extension",
			path: "code.go",
			config: Config{
				SkipExts: []string{".exe", ".png"},
			},
			expected: false,
		},
		{
			name:     "use default config",
			path:     "image.png",
			config:   DefaultConfig,
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ShouldIgnore(tc.path, tc.config)
			if result != tc.expected {
				t.Errorf("ShouldIgnore(%q, config) = %v, expected %v", tc.path, result, tc.expected)
			}
		})
	}
}

func TestPathHasPrefix(t *testing.T) {
	testCases := []struct {
		name     string
		path     string
		prefix   string
		expected bool
	}{
		{
			name:     "exact match",
			path:     "foo/bar",
			prefix:   "foo/bar",
			expected: true,
		},
		{
			name:     "prefix match",
			path:     "foo/bar/baz",
			prefix:   "foo/bar",
			expected: true,
		},
		{
			name:     "no match",
			path:     "foo/bar",
			prefix:   "baz",
			expected: false,
		},
		{
			name:     "shorter path than prefix",
			path:     "foo",
			prefix:   "foo/bar",
			expected: false,
		},
		{
			name:     "partial component match",
			path:     "foo/barext/baz",
			prefix:   "foo/bar",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Normalize paths to the platform-specific format
			path := filepath.FromSlash(tc.path)
			prefix := filepath.FromSlash(tc.prefix)

			result := PathHasPrefix(path, prefix)
			if result != tc.expected {
				t.Errorf("PathHasPrefix(%q, %q) = %v, expected %v", path, prefix, result, tc.expected)
			}
		})
	}
}
