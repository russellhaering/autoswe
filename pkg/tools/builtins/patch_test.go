package builtins

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tagOf(line string) string { return lineTag([]byte(line)) }

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runPatch(t *testing.T, path string, edits []map[string]any) (string, bool) {
	t.Helper()
	args := mustJSON(t, map[string]any{"path": path, "edits": edits})
	r, err := Patch.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("patch err: %v", err)
	}
	return r.Content, r.IsError
}

func TestPatch_SingleLineReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	writeFile(t, path, "alpha\nbeta\ngamma\n")
	content, isErr := runPatch(t, path, []map[string]any{
		{"line": 2, "tag": tagOf("beta"), "new": "BETA"},
	})
	if isErr {
		t.Fatalf("unexpected error: %s", content)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "alpha\nBETA\ngamma\n" {
		t.Fatalf("got %q", got)
	}
}

func TestPatch_RangeReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	writeFile(t, path, "1\n2\n3\n4\n5\n")
	content, isErr := runPatch(t, path, []map[string]any{
		{
			"line": 2, "tag": tagOf("2"),
			"end_line": 4, "end_tag": tagOf("4"),
			"new": "X\nY",
		},
	})
	if isErr {
		t.Fatalf("unexpected error: %s", content)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "1\nX\nY\n5\n" {
		t.Fatalf("got %q", got)
	}
}

func TestPatch_InsertBefore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	writeFile(t, path, "a\nb\n")
	_, isErr := runPatch(t, path, []map[string]any{
		{"line": 2, "tag": tagOf("b"), "action": "insert_before", "new": "NEW"},
	})
	if isErr {
		t.Fatalf("expected success")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "a\nNEW\nb\n" {
		t.Fatalf("got %q", got)
	}
}

func TestPatch_InsertAfter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	writeFile(t, path, "a\nb\n")
	_, isErr := runPatch(t, path, []map[string]any{
		{"line": 1, "tag": tagOf("a"), "action": "insert_after", "new": "NEW"},
	})
	if isErr {
		t.Fatalf("expected success")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "a\nNEW\nb\n" {
		t.Fatalf("got %q", got)
	}
}

func TestPatch_Delete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	writeFile(t, path, "a\nb\nc\nd\n")
	_, isErr := runPatch(t, path, []map[string]any{
		{
			"line": 2, "tag": tagOf("b"),
			"end_line": 3, "end_tag": tagOf("c"),
			"action": "delete",
		},
	})
	if isErr {
		t.Fatalf("expected success")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "a\nd\n" {
		t.Fatalf("got %q", got)
	}
}

func TestPatch_TagMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	original := "alpha\nbeta\n"
	writeFile(t, path, original)
	content, isErr := runPatch(t, path, []map[string]any{
		{"line": 1, "tag": "WRONG", "new": "X"},
	})
	if !isErr {
		t.Fatalf("expected tag mismatch error")
	}
	if !strings.Contains(content, "tag mismatch") || !strings.Contains(content, "re-read") {
		t.Errorf("error should mention tag mismatch + re-read; got %q", content)
	}
	got, _ := os.ReadFile(path)
	if string(got) != original {
		t.Errorf("file should be unchanged; got %q", got)
	}
}

func TestPatch_OverlappingEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	original := "1\n2\n3\n4\n"
	writeFile(t, path, original)
	content, isErr := runPatch(t, path, []map[string]any{
		{"line": 1, "tag": tagOf("1"), "end_line": 3, "end_tag": tagOf("3"), "new": "X"},
		{"line": 2, "tag": tagOf("2"), "end_line": 4, "end_tag": tagOf("4"), "new": "Y"},
	})
	if !isErr {
		t.Fatalf("expected overlap error")
	}
	if !strings.Contains(content, "overlap") {
		t.Errorf("expected 'overlap' in error; got %q", content)
	}
	got, _ := os.ReadFile(path)
	if string(got) != original {
		t.Errorf("file should be unchanged on overlap; got %q", got)
	}
}

func TestPatch_ReverseOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	writeFile(t, path, "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n")
	// Two edits supplied in ascending order: line 3 and line 9.
	_, isErr := runPatch(t, path, []map[string]any{
		{"line": 3, "tag": tagOf("3"), "new": "THREE"},
		{"line": 9, "tag": tagOf("9"), "new": "NINE"},
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	got, _ := os.ReadFile(path)
	want := "1\n2\nTHREE\n4\n5\n6\n7\n8\nNINE\n10\n"
	if string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPatch_TrailingNewline(t *testing.T) {
	t.Run("preserved_when_file_ends_with_nl", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "f")
		writeFile(t, path, "a\nb\n")
		_, isErr := runPatch(t, path, []map[string]any{
			{"line": 2, "tag": tagOf("b"), "new": "B"},
		})
		if isErr {
			t.Fatal("unexpected error")
		}
		got, _ := os.ReadFile(path)
		if string(got) != "a\nB\n" {
			t.Fatalf("trailing newline lost; got %q", got)
		}
	})
	t.Run("absent_when_file_didnt_end_with_nl", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "f")
		writeFile(t, path, "a\nb")
		_, isErr := runPatch(t, path, []map[string]any{
			{"line": 2, "tag": tagOf("b"), "new": "B"},
		})
		if isErr {
			t.Fatal("unexpected error")
		}
		got, _ := os.ReadFile(path)
		if string(got) != "a\nB" {
			t.Fatalf("trailing newline added; got %q", got)
		}
	})
}

func TestPatch_EmptyEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	writeFile(t, path, "x\n")
	args := mustJSON(t, map[string]any{"path": path, "edits": []any{}})
	r, _ := Patch.Run(context.Background(), args)
	if !r.IsError {
		t.Fatalf("expected error on empty edits")
	}
}

func TestPatch_NonExistentFile(t *testing.T) {
	args := mustJSON(t, map[string]any{
		"path": filepath.Join(t.TempDir(), "missing"),
		"edits": []map[string]any{
			{"line": 1, "tag": "AAAA"},
		},
	})
	r, _ := Patch.Run(context.Background(), args)
	if !r.IsError {
		t.Fatal("expected error for missing file")
	}
}

func TestPatch_AtomicOnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	original := "a\nb\nc\n"
	writeFile(t, path, original)
	// First edit valid; second has wrong tag — entire patch must abort.
	content, isErr := runPatch(t, path, []map[string]any{
		{"line": 1, "tag": tagOf("a"), "new": "A"},
		{"line": 2, "tag": "WRONG", "new": "B"},
	})
	if !isErr {
		t.Fatalf("expected error")
	}
	if !strings.Contains(content, "tag mismatch") {
		t.Errorf("expected tag mismatch; got %q", content)
	}
	got, _ := os.ReadFile(path)
	if string(got) != original {
		t.Errorf("file mutated despite atomic abort; got %q", got)
	}
}

func TestPatch_LineOutOfRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	writeFile(t, path, "a\n")
	r, _ := Patch.Run(context.Background(), mustJSON(t, map[string]any{
		"path": path,
		"edits": []map[string]any{
			{"line": 5, "tag": "AAAA", "new": "X"},
		},
	}))
	if !r.IsError {
		t.Fatal("expected out-of-range error")
	}
	if !strings.Contains(r.Content, "out of range") {
		t.Errorf("expected 'out of range'; got %q", r.Content)
	}
}

func TestPatch_PreservesMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	writeFile(t, path, "a\n")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	_, isErr := runPatch(t, path, []map[string]any{
		{"line": 1, "tag": tagOf("a"), "new": "A"},
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode not preserved: got %o want 0600", info.Mode().Perm())
	}
}

func TestPatch_NewWithEmbeddedNewlines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	writeFile(t, path, "x\n")
	_, isErr := runPatch(t, path, []map[string]any{
		{"line": 1, "tag": tagOf("x"), "new": "one\ntwo\nthree"},
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "one\ntwo\nthree\n" {
		t.Fatalf("got %q", got)
	}
}

// Sanity check that the JSON encoder of patchEdit handles the schema names.
func TestPatch_SchemaSnapshot(t *testing.T) {
	var raw map[string]any
	if err := json.Unmarshal(Patch.Schema(), &raw); err != nil {
		t.Fatalf("Schema() not valid JSON: %v", err)
	}
	if raw["type"] != "object" {
		t.Errorf("schema root must be object")
	}
}
