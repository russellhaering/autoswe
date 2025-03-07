package simplediff

import (
	"testing"
)

func TestParseDiff(t *testing.T) {
	tests := []struct {
		name          string
		diff          string
		wantSearch    string
		wantReplace   string
		wantErr       bool
		specificError error
	}{
		{
			name: "basic example",
			diff: `<<<<<<< SEARCH
from flask import Flask
=======
import math
from flask import Flask
>>>>>>> REPLACE`,
			wantSearch:  "from flask import Flask",
			wantReplace: "import math\nfrom flask import Flask",
			wantErr:     false,
		},
		{
			name: "multiline search and replace",
			diff: `<<<<<<< SEARCH
func add(a, b int) int {
	return a + b
}
=======
func add(a, b int) int {
	// Add two numbers
	return a + b
}
>>>>>>> REPLACE`,
			wantSearch:  "func add(a, b int) int {\n\treturn a + b\n}",
			wantReplace: "func add(a, b int) int {\n\t// Add two numbers\n\treturn a + b\n}",
			wantErr:     false,
		},
		{
			name: "empty search",
			diff: `<<<<<<< SEARCH
=======
// New code
>>>>>>> REPLACE`,
			wantSearch:  "",
			wantReplace: "// New code",
			wantErr:     false,
		},
		{
			name: "empty replace",
			diff: `<<<<<<< SEARCH
// Old code
=======
>>>>>>> REPLACE`,
			wantSearch:  "// Old code",
			wantReplace: "",
			wantErr:     false,
		},
		{
			name: "missing search marker",
			diff: `from flask import Flask
=======
import math
from flask import Flask
>>>>>>> REPLACE`,
			wantSearch:    "",
			wantReplace:   "",
			wantErr:       true,
			specificError: ErrInvalidDiffFormat,
		},
		{
			name: "missing separator marker",
			diff: `<<<<<<< SEARCH
from flask import Flask
import math
from flask import Flask
>>>>>>> REPLACE`,
			wantSearch:    "",
			wantReplace:   "",
			wantErr:       true,
			specificError: ErrInvalidDiffFormat,
		},
		{
			name: "missing replace marker",
			diff: `<<<<<<< SEARCH
from flask import Flask
=======
import math
from flask import Flask`,
			wantSearch:    "",
			wantReplace:   "",
			wantErr:       true,
			specificError: ErrInvalidDiffFormat,
		},
		{
			name: "markers in wrong order",
			diff: `=======
import math
from flask import Flask
<<<<<<< SEARCH
from flask import Flask
>>>>>>> REPLACE`,
			wantSearch:    "",
			wantReplace:   "",
			wantErr:       true,
			specificError: ErrInvalidDiffFormat,
		},
		{
			name:          "empty diff",
			diff:          "",
			wantSearch:    "",
			wantReplace:   "",
			wantErr:       true,
			specificError: ErrInvalidDiffFormat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			search, replace, err := ParseDiff(tt.diff)

			// Check error
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDiff() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// If we're expecting a specific error, check that it's the expected one
			if tt.wantErr && tt.specificError != nil && err != tt.specificError {
				t.Errorf("ParseDiff() error = %v, want specific error %v", err, tt.specificError)
				return
			}

			// Check search content
			if search != tt.wantSearch {
				t.Errorf("ParseDiff() search = %q, want %q", search, tt.wantSearch)
			}

			// Check replace content
			if replace != tt.wantReplace {
				t.Errorf("ParseDiff() replace = %q, want %q", replace, tt.wantReplace)
			}
		})
	}
}

func TestApplyDiff(t *testing.T) {
	tests := []struct {
		name          string
		fileContent   string
		diff          string
		want          string
		wantErr       bool
		specificError error
	}{
		{
			name:        "simple replacement",
			fileContent: "from flask import Flask\n\napp = Flask(__name__)",
			diff: `<<<<<<< SEARCH
from flask import Flask
=======
import math
from flask import Flask
>>>>>>> REPLACE`,
			want:    "import math\nfrom flask import Flask\n\napp = Flask(__name__)",
			wantErr: false,
		},
		{
			name:        "search not found",
			fileContent: "import os\nfrom flask import Flask",
			diff: `<<<<<<< SEARCH
from django import forms
=======
from django.forms import ModelForm
>>>>>>> REPLACE`,
			want:          "",
			wantErr:       true,
			specificError: ErrSearchNotFound,
		},
		{
			name:        "replacement in middle of file",
			fileContent: "package main\n\nfunc add(a, b int) int {\n\treturn a + b\n}\n\nfunc main() {\n\tfmt.Println(add(1, 2))\n}",
			diff: `<<<<<<< SEARCH
func add(a, b int) int {
	return a + b
}
=======
func add(a, b int) int {
	// Add two numbers
	return a + b
}
>>>>>>> REPLACE`,
			want:    "package main\n\nfunc add(a, b int) int {\n\t// Add two numbers\n\treturn a + b\n}\n\nfunc main() {\n\tfmt.Println(add(1, 2))\n}",
			wantErr: false,
		},
		{
			name:          "invalid diff format",
			fileContent:   "Some content",
			diff:          "This is not a valid diff",
			want:          "",
			wantErr:       true,
			specificError: ErrInvalidDiffFormat,
		},
		{
			name:        "remove content",
			fileContent: "Line 1\nLine to remove\nLine 3",
			diff: `<<<<<<< SEARCH
Line to remove
=======
>>>>>>> REPLACE`,
			want:    "Line 1\nLine 3",
			wantErr: false,
		},
		{
			name:        "add content",
			fileContent: "Line 1\nLine 3",
			diff: `<<<<<<< SEARCH
Line 1
=======
Line 1
Line 2
>>>>>>> REPLACE`,
			want:    "Line 1\nLine 2\nLine 3",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyDiff(tt.fileContent, tt.diff)

			// Check error
			if (err != nil) != tt.wantErr {
				t.Errorf("ApplyDiff() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// If we're expecting a specific error, check that it's the expected one
			if tt.wantErr && tt.specificError != nil && err != tt.specificError {
				t.Errorf("ApplyDiff() error = %v, want specific error %v", err, tt.specificError)
				return
			}

			if !tt.wantErr && got != tt.want {
				t.Errorf("ApplyDiff() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyMultipleDiffs(t *testing.T) {
	tests := []struct {
		name        string
		fileContent string
		diffs       []string
		want        string
		wantErr     bool
	}{
		{
			name:        "apply multiple diffs",
			fileContent: "func add(a, b int) int {\n\treturn a + b\n}\n\nfunc sub(a, b int) int {\n\treturn a - b\n}",
			diffs: []string{
				`<<<<<<< SEARCH
func add(a, b int) int {
	return a + b
}
=======
func add(a, b int) int {
	// Add two numbers
	return a + b
}
>>>>>>> REPLACE`,
				`<<<<<<< SEARCH
func sub(a, b int) int {
	return a - b
}
=======
func sub(a, b int) int {
	// Subtract b from a
	return a - b
}
>>>>>>> REPLACE`,
			},
			want:    "func add(a, b int) int {\n\t// Add two numbers\n\treturn a + b\n}\n\nfunc sub(a, b int) int {\n\t// Subtract b from a\n\treturn a - b\n}",
			wantErr: false,
		},
		{
			name:        "fail on first diff",
			fileContent: "Some content",
			diffs: []string{
				"Invalid diff",
				`<<<<<<< SEARCH
Not found
=======
Replacement
>>>>>>> REPLACE`,
			},
			want:    "",
			wantErr: true,
		},
		{
			name:        "fail on second diff",
			fileContent: "func add(a, b int) int {\n\treturn a + b\n}\n\nfunc sub(a, b int) int {\n\treturn a - b\n}",
			diffs: []string{
				`<<<<<<< SEARCH
func add(a, b int) int {
	return a + b
}
=======
func add(a, b int) int {
	// Add two numbers
	return a + b
}
>>>>>>> REPLACE`,
				`<<<<<<< SEARCH
Does not exist
=======
Replacement
>>>>>>> REPLACE`,
			},
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyMultipleDiffs(tt.fileContent, tt.diffs)

			// Check error
			if (err != nil) != tt.wantErr {
				t.Errorf("ApplyMultipleDiffs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && got != tt.want {
				t.Errorf("ApplyMultipleDiffs() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestLargerExamples tests more realistic, larger examples
func TestLargerExamples(t *testing.T) {
	t.Run("larger example", func(t *testing.T) {
		// Verify our diff package can handle larger, more realistic examples
		fileContent := `package main

import "fmt"

func main() {
	fmt.Println("Hello, world!")
}
`
		diff := `<<<<<<< SEARCH
	fmt.Println("Hello, world!")
=======
	fmt.Println("Hello, improved world!")
	fmt.Println("This is a much better greeting.")
>>>>>>> REPLACE`

		got, err := ApplyDiff(fileContent, diff)
		if err != nil {
			t.Fatalf("ApplyDiff() error = %v", err)
		}

		want := `package main

import "fmt"

func main() {
	fmt.Println("Hello, improved world!")
	fmt.Println("This is a much better greeting.")
}
`
		if got != want {
			t.Errorf("ApplyDiff() = %q, want %q", got, want)
		}
	})
}
