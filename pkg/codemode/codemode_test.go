package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/russellhaering/autoswe/pkg/permissions"
	"github.com/russellhaering/autoswe/pkg/tools"
)

// fakeTool is a programmable tools.Tool used throughout these tests. Its
// Run captures the args it received and returns either a canned Result or
// an error.
type fakeTool struct {
	name    string
	desc    string
	schema  string
	effects []tools.Effect
	calls   atomic.Int32
	lastArg json.RawMessage
	run     func(ctx context.Context, args json.RawMessage) (tools.Result, error)
}

func (f *fakeTool) Name() string             { return f.name }
func (f *fakeTool) Description() string      { return f.desc }
func (f *fakeTool) Schema() json.RawMessage  { return json.RawMessage(f.schema) }
func (f *fakeTool) Effects() []tools.Effect  { return f.effects }
func (f *fakeTool) Run(ctx context.Context, args json.RawMessage) (tools.Result, error) {
	f.calls.Add(1)
	f.lastArg = append(f.lastArg[:0], args...)
	if f.run != nil {
		return f.run(ctx, args)
	}
	return tools.Result{Content: string(args)}, nil
}

func newEchoRegistry(t *testing.T, extras ...tools.Tool) *tools.Registry {
	t.Helper()
	r := tools.NewRegistry()
	echo := &fakeTool{
		name:   "echo",
		desc:   "Echo back the JSON object passed in.",
		schema: `{"type":"object","properties":{"msg":{"type":"string"}},"required":["msg"]}`,
	}
	if err := r.Register(echo); err != nil {
		t.Fatalf("register echo: %v", err)
	}
	for _, e := range extras {
		if err := r.Register(e); err != nil {
			t.Fatalf("register %s: %v", e.Name(), err)
		}
	}
	return r
}

func runScript(t *testing.T, r *RunScript, src string) tools.Result {
	t.Helper()
	args, err := json.Marshal(map[string]string{"script": src})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	res, err := r.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return res
}

func TestRunScript_BasicEcho(t *testing.T) {
	reg := newEchoRegistry(t)
	r := New(reg, permissions.AllowAll{}, Limits{}, nil)
	res := runScript(t, r, `
		const out = await echo({msg: "hi"});
		console.log("got:", out);
	`)
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if !strings.Contains(res.Content, `got: {"msg":"hi"}`) {
		t.Errorf("missing echo output in:\n%s", res.Content)
	}
}

func TestRunScript_NoRecursion(t *testing.T) {
	reg := newEchoRegistry(t)
	r := New(reg, permissions.AllowAll{}, Limits{}, nil)
	res := runScript(t, r, `console.log(typeof run_script);`)
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if !strings.Contains(res.Content, "undefined") {
		t.Errorf("run_script should not be bound; got:\n%s", res.Content)
	}
}

func TestRunScript_PolicyDeny(t *testing.T) {
	reg := newEchoRegistry(t)
	pol := stubPolicy{decision: permissions.Decision{Action: permissions.Deny, Reason: "nope"}}
	r := New(reg, pol, Limits{}, nil)
	res := runScript(t, r, `
		try { await echo({msg: "x"}); console.log("ran"); }
		catch (e) { console.log("denied:", e.message); }
	`)
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if !strings.Contains(res.Content, "denied: nope") {
		t.Errorf("expected deny reason; got:\n%s", res.Content)
	}
}

func TestRunScript_PolicyModify(t *testing.T) {
	reg := newEchoRegistry(t)
	rewritten := json.RawMessage(`{"msg":"modified"}`)
	pol := stubPolicy{decision: permissions.Decision{
		Action:       permissions.Modify,
		ModifiedArgs: rewritten,
	}}
	r := New(reg, pol, Limits{}, nil)
	res := runScript(t, r, `
		const out = await echo({msg: "original"});
		console.log(out);
	`)
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if !strings.Contains(res.Content, `"msg":"modified"`) {
		t.Errorf("expected modified args to be observed by tool; got:\n%s", res.Content)
	}
}

func TestRunScript_Throws(t *testing.T) {
	reg := newEchoRegistry(t)
	r := New(reg, permissions.AllowAll{}, Limits{}, nil)
	res := runScript(t, r, `throw new Error("boom");`)
	if !res.IsError {
		t.Fatalf("expected IsError; got:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "boom") {
		t.Errorf("expected 'boom' in error; got:\n%s", res.Content)
	}
}

func TestRunScript_FinalValueAppended(t *testing.T) {
	reg := newEchoRegistry(t)
	r := New(reg, permissions.AllowAll{}, Limits{}, nil)
	res := runScript(t, r, `42`)
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if !strings.Contains(res.Content, "[script result]") || !strings.Contains(res.Content, "42") {
		t.Errorf("expected final value in output; got:\n%s", res.Content)
	}
}

func TestRunScript_Timeout(t *testing.T) {
	reg := newEchoRegistry(t)
	lim := Limits{MaxExecution: 200 * time.Millisecond, MemoryMiB: 32}
	r := New(reg, permissions.AllowAll{}, lim, nil)
	start := time.Now()
	res := runScript(t, r, `while(true){}`)
	if !res.IsError {
		t.Fatalf("expected timeout error; got:\n%s", res.Content)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("timeout took too long: %v", elapsed)
	}
}

func TestRunScript_SkipsInvalidIdent(t *testing.T) {
	reg := newEchoRegistry(t, &fakeTool{
		name:   "weird-name",
		schema: `{"type":"object","properties":{}}`,
	})
	r := New(reg, permissions.AllowAll{}, Limits{}, nil)
	// Confirm only `echo` made it into the TS surface.
	if !strings.Contains(r.tsDecl, "declare function echo") {
		t.Error("echo missing from TS surface")
	}
	if strings.Contains(r.tsDecl, "weird-name") {
		t.Error("weird-name should not appear in TS surface")
	}
	res := runScript(t, r, `console.log(typeof globalThis["weird-name"]);`)
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if !strings.Contains(res.Content, "undefined") {
		t.Errorf("weird-name should not be bound; got:\n%s", res.Content)
	}
}

func TestRunScript_MCPLikeName(t *testing.T) {
	reg := newEchoRegistry(t, &fakeTool{
		name:   "mcp__foo__bar",
		schema: `{"type":"object","properties":{}}`,
		run:    func(_ context.Context, _ json.RawMessage) (tools.Result, error) { return tools.Result{Content: "ok"}, nil },
	})
	r := New(reg, permissions.AllowAll{}, Limits{}, nil)
	res := runScript(t, r, `console.log(await mcp__foo__bar({}));`)
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if !strings.Contains(res.Content, "ok") {
		t.Errorf("mcp__foo__bar binding broken; got:\n%s", res.Content)
	}
}

func TestRunScript_ToolErrorThrows(t *testing.T) {
	reg := tools.NewRegistry()
	if err := reg.Register(&fakeTool{
		name:   "boom",
		schema: `{"type":"object","properties":{}}`,
		run: func(_ context.Context, _ json.RawMessage) (tools.Result, error) {
			return tools.Result{IsError: true, Content: "kapow"}, nil
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	r := New(reg, permissions.AllowAll{}, Limits{}, nil)
	res := runScript(t, r, `
		try { await boom({}); console.log("ran"); }
		catch (e) { console.log("err:", e.message); }
	`)
	if res.IsError {
		t.Fatalf("script itself should not error: %+v", res)
	}
	if !strings.Contains(res.Content, "kapow") {
		t.Errorf("expected 'kapow' in caught error; got:\n%s", res.Content)
	}
}

func TestRunScript_GoErrorThrows(t *testing.T) {
	reg := tools.NewRegistry()
	if err := reg.Register(&fakeTool{
		name:   "fail",
		schema: `{"type":"object","properties":{}}`,
		run: func(_ context.Context, _ json.RawMessage) (tools.Result, error) {
			return tools.Result{}, errors.New("go side failed")
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	r := New(reg, permissions.AllowAll{}, Limits{}, nil)
	res := runScript(t, r, `
		try { await fail({}); console.log("ran"); }
		catch (e) { console.log("caught:", e.message); }
	`)
	if res.IsError {
		t.Fatalf("script itself should not error: %+v", res)
	}
	if !strings.Contains(res.Content, "go side failed") {
		t.Errorf("expected go error in caught message; got:\n%s", res.Content)
	}
}

func TestRunScript_Effects(t *testing.T) {
	reg := newEchoRegistry(t)
	r := New(reg, permissions.AllowAll{}, Limits{}, nil)
	got := r.Effects()
	if len(got) != 1 || got[0] != tools.EffectReadOnly {
		t.Errorf("expected [ReadOnly] (sandbox itself is compute-only); got %v", got)
	}
}

func TestRunScript_ExitAfterPropagates(t *testing.T) {
	reg := tools.NewRegistry()
	if err := reg.Register(&fakeTool{
		name:   "finish",
		schema: `{"type":"object","properties":{}}`,
		run: func(_ context.Context, _ json.RawMessage) (tools.Result, error) {
			return tools.Result{Content: "done", ExitAfter: true}, nil
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	r := New(reg, permissions.AllowAll{}, Limits{}, nil)
	res := runScript(t, r, `console.log(await finish({}));`)
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	if !res.ExitAfter {
		t.Errorf("expected ExitAfter to propagate from inner call; got %+v", res)
	}
}

func TestRunScript_EmptyScript(t *testing.T) {
	reg := newEchoRegistry(t)
	r := New(reg, permissions.AllowAll{}, Limits{}, nil)
	res, err := r.Run(context.Background(), json.RawMessage(`{"script":""}`))
	if err != nil {
		t.Fatalf("unexpected go error: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected IsError on empty script")
	}
}

// ---------- Policy stub ----------

type stubPolicy struct {
	decision permissions.Decision
	err      error
}

func (s stubPolicy) Check(_ context.Context, _ permissions.ToolCall) (permissions.Decision, error) {
	return s.decision, s.err
}

// ---------- TS declaration tests ----------

func TestTSDeclaration_BasicObject(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string"},
			"offset": {"type": "integer"},
			"limit": {"type": "integer"}
		},
		"required": ["path"]
	}`)
	out := toTSDeclaration("read", "Read a file.", schema)
	mustContain(t, out, "/** Read a file. */")
	mustContain(t, out, "declare function read(args: { limit?: number; offset?: number; path: string }): Promise<string>;")
}

func TestTSDeclaration_Array(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"patterns": {"type": "array", "items": {"type": "string"}}
		}
	}`)
	out := toTSDeclaration("glob", "", schema)
	mustContain(t, out, "patterns?: string[]")
}

func TestTSDeclaration_Enum(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"mode": {"type": "string", "enum": ["a", "b"]}
		},
		"required": ["mode"]
	}`)
	out := toTSDeclaration("x", "", schema)
	mustContain(t, out, `mode: "a" | "b"`)
}

func TestTSDeclaration_Nested(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"opts": {
				"type": "object",
				"properties": {
					"timeout": {"type": "number"},
					"flag": {"type": "boolean"}
				}
			}
		}
	}`)
	out := toTSDeclaration("x", "", schema)
	mustContain(t, out, "opts?: { flag?: boolean; timeout?: number }")
}

func TestTSDeclaration_UnknownFallsThrough(t *testing.T) {
	schema := json.RawMessage(`{"oneOf": [{"type":"string"}, {"type":"number"}]}`)
	out := toTSDeclaration("x", "", schema)
	mustContain(t, out, "any")
	mustContain(t, out, "schema:")
}

func mustContain(t *testing.T, s, want string) {
	t.Helper()
	if !strings.Contains(s, want) {
		t.Errorf("expected %q in:\n%s", want, s)
	}
}
