package builtins

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/russellhaering/autoswe/pkg/tools"
)

func TestWebFetch_HTMLToText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>X</title>
<script>var x = 1;</script>
<style>body { color: red }</style>
</head>
<body>
<h1>Hello, world</h1>
<p>This is a <a href="https://example.com/about">link</a> to about.</p>
<script>tracker();</script>
</body></html>`))
	}))
	defer srv.Close()

	res := runWebFetch(t, srv.URL)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if strings.Contains(res.Content, "var x = 1") || strings.Contains(res.Content, "tracker") {
		t.Errorf("script content leaked into output:\n%s", res.Content)
	}
	if strings.Contains(res.Content, "color: red") {
		t.Errorf("style content leaked into output:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "Hello, world") {
		t.Errorf("expected heading text; got:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "link [1]") {
		t.Errorf("expected anchor with reference; got:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "[1] https://example.com/about") {
		t.Errorf("expected link footer; got:\n%s", res.Content)
	}
}

func TestWebFetch_PlainText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("just words"))
	}))
	defer srv.Close()

	res := runWebFetch(t, srv.URL)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "just words") {
		t.Errorf("plain text not preserved:\n%s", res.Content)
	}
}

func TestWebFetch_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()

	res := runWebFetch(t, srv.URL)
	if !res.IsError {
		t.Errorf("expected IsError on 404")
	}
	if !strings.Contains(res.Content, "404") {
		t.Errorf("expected status in error content; got:\n%s", res.Content)
	}
}

func TestWebFetch_RejectsScheme(t *testing.T) {
	res := runWebFetch(t, "file:///etc/passwd")
	if !res.IsError {
		t.Errorf("expected IsError on file:// scheme")
	}
	if !strings.Contains(res.Content, "scheme") {
		t.Errorf("expected error to mention scheme; got: %s", res.Content)
	}
}

func TestWebFetch_SizeCap(t *testing.T) {
	// Reply with > 2 MiB of body
	big := strings.Repeat("a", (2<<20)+1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()

	res := runWebFetch(t, srv.URL)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "[response truncated") {
		t.Errorf("expected truncation marker; got tail: %q", lastN(res.Content, 80))
	}
}

func runWebFetch(t *testing.T, url string) tools.Result {
	t.Helper()
	args, err := json.Marshal(map[string]any{"url": url})
	if err != nil {
		t.Fatal(err)
	}
	res, err := WebFetch.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("WebFetch.Run: %v", err)
	}
	return res
}

func lastN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
