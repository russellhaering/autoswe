package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/russellhaering/autoswe/pkg/tools"
)

func boolPtr(b bool) *bool { return &b }

func TestEffectsFor(t *testing.T) {
	cases := []struct {
		name string
		ann  Annotations
		want []tools.Effect
	}{
		{"readOnly_wins", Annotations{ReadOnlyHint: boolPtr(true), DestructiveHint: boolPtr(true)}, []tools.Effect{tools.EffectReadOnly}},
		{"destructive_only", Annotations{DestructiveHint: boolPtr(true)}, []tools.Effect{tools.EffectFilesystemWrite}},
		{"open_world", Annotations{OpenWorldHint: boolPtr(true)}, []tools.Effect{tools.EffectNetwork}},
		{"destructive_and_open", Annotations{DestructiveHint: boolPtr(true), OpenWorldHint: boolPtr(true)}, []tools.Effect{tools.EffectFilesystemWrite, tools.EffectNetwork}},
		{"no_hints_conservative", Annotations{}, []tools.Effect{tools.EffectFilesystemWrite, tools.EffectCodeExecution, tools.EffectNetwork}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EffectsFor(tc.ann)
			if !sameEffects(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func sameEffects(a, b []tools.Effect) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// fakeClient is an in-memory Client for tests.
type fakeClient struct {
	name      string
	tools     []Tool
	mu        sync.Mutex
	callsMade []struct {
		Name string
		Args json.RawMessage
	}
	respond CallResult
	err     error
}

func (c *fakeClient) Name() string                            { return c.name }
func (c *fakeClient) Initialize(context.Context) error        { return nil }
func (c *fakeClient) ListTools(context.Context) ([]Tool, error) { return c.tools, nil }
func (c *fakeClient) Close() error                            { return nil }
func (c *fakeClient) CallTool(_ context.Context, name string, args json.RawMessage) (CallResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.callsMade = append(c.callsMade, struct {
		Name string
		Args json.RawMessage
	}{name, args})
	if c.err != nil {
		return CallResult{}, c.err
	}
	return c.respond, nil
}

func TestMCPToolRun(t *testing.T) {
	c := &fakeClient{
		name:    "fs",
		respond: CallResult{Content: []ContentItem{{Type: "text", Text: "hello"}}},
	}
	tool := NewTool(c, Tool{Name: "read", InputSchema: json.RawMessage(`{"type":"object"}`), Annotations: Annotations{ReadOnlyHint: boolPtr(true)}}, true)

	if tool.Name() != "fs__read" {
		t.Fatalf("exposed name = %s", tool.Name())
	}
	if eff := tool.Effects(); len(eff) != 1 || eff[0] != tools.EffectReadOnly {
		t.Fatalf("effects = %v", eff)
	}
	res, err := tool.Run(context.Background(), json.RawMessage(`{"path":"f"}`))
	if err != nil || res.IsError || res.Content != "hello" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if got := c.callsMade[0].Name; got != "read" {
		t.Fatalf("underlying name = %s, want unprefixed 'read'", got)
	}
}

func TestMCPToolRunsErrorIsSurfaced(t *testing.T) {
	c := &fakeClient{err: errors.New("rpc boom")}
	tool := NewTool(c, Tool{Name: "x"}, false)
	res, err := tool.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("Run should not return Go error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "rpc boom") {
		t.Fatalf("expected error in result: %+v", res)
	}
}

func TestIndexSearchAndPromote(t *testing.T) {
	idx := &Index{
		entries: []IndexEntry{
			{Server: "fs", ExposedName: "fs__read", Tool: Tool{Name: "read", Description: "Read a file from disk", Annotations: Annotations{ReadOnlyHint: boolPtr(true)}}},
			{Server: "fs", ExposedName: "fs__write", Tool: Tool{Name: "write", Description: "Write a file", Annotations: Annotations{DestructiveHint: boolPtr(true)}}},
			{Server: "web", ExposedName: "web__fetch", Tool: Tool{Name: "fetch", Description: "Fetch a URL", Annotations: Annotations{OpenWorldHint: boolPtr(true)}}},
		},
		clients:  map[string]Client{"fs": &fakeClient{name: "fs"}, "web": &fakeClient{name: "web"}},
		prefixed: true,
	}

	// Search by description term.
	matches := idx.Search("file")
	if len(matches) != 2 {
		t.Fatalf("want 2 file matches, got %d", len(matches))
	}

	// Empty query returns all.
	if all := idx.Search(""); len(all) != 3 {
		t.Fatalf("empty query: want 3, got %d", len(all))
	}

	// Promote into a target registry.
	reg := tools.NewRegistry()
	promoted := idx.Promote(reg, matches)
	if len(promoted) != 2 {
		t.Fatalf("promoted %v, want 2", promoted)
	}
	if _, ok := reg.Get("fs__read"); !ok {
		t.Fatalf("registry missing fs__read")
	}
	// Idempotent: a second promote of the same entries shouldn't double-register or error.
	promoted2 := idx.Promote(reg, matches)
	if len(promoted2) != 0 {
		t.Fatalf("re-promote should be no-op, got %v", promoted2)
	}
}

func TestToolSearchRun(t *testing.T) {
	idx := &Index{
		entries: []IndexEntry{
			{Server: "fs", ExposedName: "fs__read", Tool: Tool{Name: "read", Description: "Read a file", InputSchema: json.RawMessage(`{"type":"object"}`), Annotations: Annotations{ReadOnlyHint: boolPtr(true)}}},
		},
		clients:  map[string]Client{"fs": &fakeClient{name: "fs"}},
		prefixed: true,
	}
	reg := tools.NewRegistry()
	search := NewToolSearch(idx, reg)
	res, err := search.Run(context.Background(), json.RawMessage(`{"query":"file"}`))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if !strings.Contains(res.Content, "fs__read") {
		t.Fatalf("expected fs__read in output, got %q", res.Content)
	}
	if !strings.Contains(res.Content, "Loaded into registry: fs__read") {
		t.Fatalf("expected promotion note, got %q", res.Content)
	}
	if _, ok := reg.Get("fs__read"); !ok {
		t.Fatalf("fs__read should be registered after search")
	}
}

func TestToolSearchInvalidArgs(t *testing.T) {
	idx := &Index{}
	search := NewToolSearch(idx, tools.NewRegistry())
	res, _ := search.Run(context.Background(), json.RawMessage(`not json`))
	if !res.IsError {
		t.Fatalf("invalid args should error, got %+v", res)
	}
}
