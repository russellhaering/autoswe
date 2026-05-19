package builtins

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("full", func(t *testing.T) {
		r, err := Read.Run(context.Background(), mustJSON(t, map[string]string{"path": path}))
		if err != nil || r.IsError {
			t.Fatalf("err=%v result=%+v", err, r)
		}
		if !strings.Contains(r.Content, "\t one\n") && !strings.Contains(r.Content, "\tone\n") {
			// numeric prefix uses %6d, so allow either spacing
			if !strings.Contains(r.Content, "one") || !strings.Contains(r.Content, "four") {
				t.Fatalf("want all four lines, got %q", r.Content)
			}
		}
	})

	t.Run("offset_and_limit", func(t *testing.T) {
		r, err := Read.Run(context.Background(), mustJSON(t, map[string]any{"path": path, "offset": 2, "limit": 2}))
		if err != nil || r.IsError {
			t.Fatalf("err=%v result=%+v", err, r)
		}
		if strings.Contains(r.Content, "one") {
			t.Fatalf("offset=2 should exclude line 1, got %q", r.Content)
		}
		if !strings.Contains(r.Content, "two") || !strings.Contains(r.Content, "three") {
			t.Fatalf("want lines 2-3, got %q", r.Content)
		}
		if strings.Contains(r.Content, "four") {
			t.Fatalf("limit=2 should exclude line 4, got %q", r.Content)
		}
	})

	t.Run("missing_path", func(t *testing.T) {
		r, _ := Read.Run(context.Background(), mustJSON(t, map[string]string{"path": filepath.Join(dir, "nope")}))
		if !r.IsError {
			t.Fatalf("expected IsError, got %+v", r)
		}
	})
}

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "f.txt")
	r, err := Write.Run(context.Background(), mustJSON(t, map[string]string{"path": path, "content": "hello"}))
	if err != nil || r.IsError {
		t.Fatalf("err=%v result=%+v", err, r)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("file content = %q", got)
	}
}

func TestEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")

	t.Run("single_match", func(t *testing.T) {
		if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
			t.Fatal(err)
		}
		r, err := Edit.Run(context.Background(), mustJSON(t, map[string]any{
			"path": path, "old_string": "world", "new_string": "go",
		}))
		if err != nil || r.IsError {
			t.Fatalf("err=%v result=%+v", err, r)
		}
		got, _ := os.ReadFile(path)
		if string(got) != "hello go" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("multiple_match_without_replace_all_errors", func(t *testing.T) {
		if err := os.WriteFile(path, []byte("a a a"), 0o644); err != nil {
			t.Fatal(err)
		}
		r, _ := Edit.Run(context.Background(), mustJSON(t, map[string]any{
			"path": path, "old_string": "a", "new_string": "b",
		}))
		if !r.IsError {
			t.Fatalf("want error for multiple matches without replace_all")
		}
	})

	t.Run("replace_all", func(t *testing.T) {
		if err := os.WriteFile(path, []byte("a a a"), 0o644); err != nil {
			t.Fatal(err)
		}
		r, err := Edit.Run(context.Background(), mustJSON(t, map[string]any{
			"path": path, "old_string": "a", "new_string": "b", "replace_all": true,
		}))
		if err != nil || r.IsError {
			t.Fatalf("err=%v result=%+v", err, r)
		}
		got, _ := os.ReadFile(path)
		if string(got) != "b b b" {
			t.Fatalf("got %q", got)
		}
	})

	t.Run("no_match", func(t *testing.T) {
		if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
		r, _ := Edit.Run(context.Background(), mustJSON(t, map[string]any{
			"path": path, "old_string": "zzz", "new_string": "x",
		}))
		if !r.IsError {
			t.Fatalf("want error for no match, got %+v", r)
		}
	})
}

func TestBash(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		r, err := Bash.Run(context.Background(), mustJSON(t, map[string]string{"command": "echo hello"}))
		if err != nil || r.IsError {
			t.Fatalf("err=%v result=%+v", err, r)
		}
		if !strings.Contains(r.Content, "hello") {
			t.Fatalf("expected output to contain hello, got %q", r.Content)
		}
	})

	t.Run("nonzero_exit", func(t *testing.T) {
		r, _ := Bash.Run(context.Background(), mustJSON(t, map[string]string{"command": "exit 7"}))
		if !r.IsError || !strings.Contains(r.Content, "exit 7") {
			t.Fatalf("want exit 7 error, got %+v", r)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		r, _ := Bash.Run(context.Background(), mustJSON(t, map[string]any{
			"command": "sleep 1", "timeout_ms": 50,
		}))
		if !r.IsError || !strings.Contains(r.Content, "timed out") {
			t.Fatalf("want timeout error, got %+v", r)
		}
	})

	t.Run("context_canceled", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		r, _ := Bash.Run(ctx, mustJSON(t, map[string]string{"command": "sleep 1"}))
		if !r.IsError {
			t.Fatalf("want error when context cancels, got %+v", r)
		}
	})
}

func TestGlob(t *testing.T) {
	dir := t.TempDir()
	mkFile := func(p, content string) {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mkFile("a.go", "")
	mkFile("nested/b.go", "")
	mkFile("nested/c.txt", "")

	r, err := Glob.Run(context.Background(), mustJSON(t, map[string]string{
		"pattern": "**/*.go",
		"path":    dir,
	}))
	if err != nil || r.IsError {
		t.Fatalf("err=%v result=%+v", err, r)
	}
	if !strings.Contains(r.Content, "a.go") || !strings.Contains(r.Content, "b.go") {
		t.Fatalf("expected a.go and b.go, got %q", r.Content)
	}
	if strings.Contains(r.Content, "c.txt") {
		t.Fatalf("c.txt should not match *.go, got %q", r.Content)
	}
}

func TestGrep(t *testing.T) {
	dir := t.TempDir()
	mkFile := func(p, content string) {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mkFile("a.go", "package foo\nfunc Bar() {}\n")
	mkFile("b.go", "package foo\nfunc baz() {}\n")
	mkFile("c.txt", "func Bar() {}\n")
	mkFile(".git/log", "func Bar() {}\n") // should be skipped

	t.Run("basic", func(t *testing.T) {
		r, err := Grep.Run(context.Background(), mustJSON(t, map[string]string{
			"pattern": `func Bar`,
			"path":    dir,
		}))
		if err != nil || r.IsError {
			t.Fatalf("err=%v result=%+v", err, r)
		}
		if !strings.Contains(r.Content, "a.go") {
			t.Fatalf("want a.go in matches, got %q", r.Content)
		}
		if strings.Contains(r.Content, ".git") {
			t.Fatalf(".git should be skipped, got %q", r.Content)
		}
	})

	t.Run("glob_filter", func(t *testing.T) {
		r, _ := Grep.Run(context.Background(), mustJSON(t, map[string]any{
			"pattern": "func",
			"path":    dir,
			"glob":    "**/*.go",
		}))
		if strings.Contains(r.Content, "c.txt") {
			t.Fatalf("c.txt should be excluded by glob, got %q", r.Content)
		}
	})

	t.Run("case_insensitive", func(t *testing.T) {
		r, _ := Grep.Run(context.Background(), mustJSON(t, map[string]any{
			"pattern":          "BAR",
			"path":             dir,
			"case_insensitive": true,
		}))
		if !strings.Contains(r.Content, "func Bar") {
			t.Fatalf("case-insensitive should match, got %q", r.Content)
		}
	})

	t.Run("no_match", func(t *testing.T) {
		r, _ := Grep.Run(context.Background(), mustJSON(t, map[string]string{
			"pattern": "zzz_no_such",
			"path":    dir,
		}))
		if r.Content != "(no matches)" {
			t.Fatalf("want no matches sentinel, got %q", r.Content)
		}
	})
}

func TestDefaultRegistry(t *testing.T) {
	r := DefaultRegistry()
	want := []string{"bash", "edit", "glob", "grep", "read", "web_fetch", "write"}
	got := r.Names()
	if len(got) != len(want) {
		t.Fatalf("want %d tools, got %d (%v)", len(want), len(got), got)
	}
	for i, n := range want {
		if got[i] != n {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
}
