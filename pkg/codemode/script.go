package codemode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/fastschema/qjs"

	"github.com/russellhaering/autoswe/pkg/permissions"
	"github.com/russellhaering/autoswe/pkg/tools"
)

// execute runs script in a fresh QuickJS runtime. console.log/warn/error
// output is captured to a buffer and returned as Result.Content; if the
// script's final expression is non-undefined it's appended on a "[script
// result]" line. Uncaught exceptions and policy denials surface as
// Result{IsError: true}.
//
// qjs's MaxExecutionTime option is unused at the C level in v0.0.6, so we
// enforce the limit via context.WithTimeout + CloseOnContextDone — wazero
// closes the module instance when the context fires, which unblocks the
// wasm call. The closure may panic out of internal qjs frames, so we
// recover at the top level.
func (r *RunScript) execute(parentCtx context.Context, script string) (res tools.Result) {
	var stdout, stderr bytes.Buffer
	var exitAfter bool

	runCtx, cancel := context.WithTimeout(parentCtx, r.limits.MaxExecution)
	defer cancel()

	defer func() {
		if rec := recover(); rec != nil {
			msg := fmt.Sprintf("script error: %v", rec)
			if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
				msg = fmt.Sprintf("script exceeded max execution time (%s)", r.limits.MaxExecution)
			}
			res = tools.Result{
				IsError:   true,
				Content:   assembleOutput(&stdout, &stderr, "", msg),
				ExitAfter: exitAfter,
			}
		}
	}()

	rt, err := qjs.New(qjs.Option{
		Context:            runCtx,
		CloseOnContextDone: true,
		MemoryLimit:        int(r.limits.MemoryMiB << 20),
		Stdout:             &stdout,
		Stderr:             &stderr,
	})
	if err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("codemode: runtime init failed: %v", err)}
	}
	defer rt.Close()

	jsCtx := rt.Context()

	r.installConsole(jsCtx, &stdout)
	for _, b := range r.bindings {
		bind := b
		jsCtx.SetFunc(bind.jsName, r.makeHostFunc(parentCtx, bind, &exitAfter))
	}

	val, evalErr := jsCtx.Eval("script.js", qjs.Code(script), qjs.FlagAsync())
	if evalErr != nil {
		msg := fmt.Sprintf("script error: %v", evalErr)
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			msg = fmt.Sprintf("script exceeded max execution time (%s)", r.limits.MaxExecution)
		}
		return tools.Result{
			IsError:   true,
			Content:   assembleOutput(&stdout, &stderr, "", msg),
			ExitAfter: exitAfter,
		}
	}
	defer val.Free()

	if val.IsPromise() {
		resolved, awaitErr := val.Await()
		if awaitErr != nil {
			msg := fmt.Sprintf("script error: %v", awaitErr)
			if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
				msg = fmt.Sprintf("script exceeded max execution time (%s)", r.limits.MaxExecution)
			}
			return tools.Result{
				IsError:   true,
				Content:   assembleOutput(&stdout, &stderr, "", msg),
				ExitAfter: exitAfter,
			}
		}
		defer resolved.Free()
		return tools.Result{
			Content:   assembleOutput(&stdout, &stderr, stringifyFinal(resolved), ""),
			ExitAfter: exitAfter,
		}
	}

	return tools.Result{
		Content:   assembleOutput(&stdout, &stderr, stringifyFinal(val), ""),
		ExitAfter: exitAfter,
	}
}

// installConsole installs a console shim that pushes log/warn/error/info/
// debug lines into the supplied buffer. Non-string args are JSON-stringified
// for readability; warn/error/debug get a prefix so the model sees the
// channel within the single buffer.
func (r *RunScript) installConsole(jsCtx *qjs.Context, buf *bytes.Buffer) {
	jsCtx.SetFunc("__autoswe_log", func(this *qjs.This) (*qjs.Value, error) {
		ctx := this.Context()
		args := this.Args()
		var prefix, line string
		if len(args) >= 1 {
			prefix = args[0].String()
		}
		if len(args) >= 2 {
			line = args[1].String()
		}
		buf.WriteString(prefix)
		buf.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			buf.WriteByte('\n')
		}
		return ctx.NewUndefined(), nil
	})

	const shim = `
globalThis.console = (function() {
  function fmt(args) {
    return Array.prototype.map.call(args, function(a) {
      if (typeof a === "string") return a;
      try { return JSON.stringify(a); } catch (e) { return String(a); }
    }).join(" ");
  }
  return {
    log:   function() { __autoswe_log("",        fmt(arguments)); },
    info:  function() { __autoswe_log("",        fmt(arguments)); },
    warn:  function() { __autoswe_log("[warn] ", fmt(arguments)); },
    error: function() { __autoswe_log("[error] ",fmt(arguments)); },
    debug: function() { __autoswe_log("[debug] ",fmt(arguments)); },
  };
})();
`
	if v, err := jsCtx.Eval("console-shim.js", qjs.Code(shim)); err != nil {
		r.logger.Warn("codemode: failed to install console shim", "err", err)
	} else {
		v.Free()
	}
}

// makeHostFunc returns the JS-callable wrapper for one bound tool. Each
// invocation re-runs the configured Policy so --deny-write/--deny-exec/
// Interactive prompts still apply inside scripts. Returning a non-nil
// error from a qjs host function surfaces in JS as a thrown Error whose
// message is the error string — callers can try/catch it.
func (r *RunScript) makeHostFunc(parentCtx context.Context, b binding, exitAfter *bool) qjs.Function {
	return func(this *qjs.This) (*qjs.Value, error) {
		ctx := this.Context()
		argsJSON, err := marshalJSArgs(this.Args())
		if err != nil {
			return ctx.NewUndefined(), fmt.Errorf("%s: %w", b.jsName, err)
		}

		call := permissions.ToolCall{Name: b.toolName, Args: argsJSON, Tool: b.tool}
		dec, perr := r.policy.Check(parentCtx, call)
		if perr != nil {
			return ctx.NewUndefined(), fmt.Errorf("%s: policy error: %w", b.jsName, perr)
		}
		switch dec.Action {
		case permissions.Deny:
			reason := dec.Reason
			if reason == "" {
				reason = "denied by policy"
			}
			r.logger.InfoContext(parentCtx, "codemode tool denied", "tool", b.toolName, "reason", reason)
			return ctx.NewUndefined(), errors.New(reason)
		case permissions.Modify:
			if len(dec.ModifiedArgs) > 0 {
				argsJSON = dec.ModifiedArgs
			}
		}

		res, terr := b.tool.Run(parentCtx, argsJSON)
		if terr != nil {
			return ctx.NewUndefined(), fmt.Errorf("%s: %w", b.jsName, terr)
		}
		if res.ExitAfter {
			*exitAfter = true
		}
		if res.IsError {
			return ctx.NewUndefined(), fmt.Errorf("%s: %s", b.jsName, res.Content)
		}
		return ctx.NewString(res.Content), nil
	}
}

// marshalJSArgs takes a JS function's args array and returns the JSON
// representation of the first arg (or an empty object if none). Tools
// always take a single object; we round-trip via JSON.stringify for
// predictability rather than relying on Go reflection.
func marshalJSArgs(args []*qjs.Value) (json.RawMessage, error) {
	if len(args) == 0 {
		return json.RawMessage(`{}`), nil
	}
	first := args[0]
	if first.IsUndefined() || first.IsNull() {
		return json.RawMessage(`{}`), nil
	}
	s, err := first.JSONStringify()
	if err != nil {
		return nil, fmt.Errorf("JSON.stringify: %w", err)
	}
	if s == "" {
		return json.RawMessage(`{}`), nil
	}
	if !json.Valid([]byte(s)) {
		return nil, fmt.Errorf("argument is not JSON-serializable: %q", s)
	}
	return json.RawMessage(s), nil
}

// stringifyFinal renders the script's final expression value as a string.
// Returns "" for undefined or null so the caller can skip the trailing
// "[script result]" block.
func stringifyFinal(v *qjs.Value) string {
	if v == nil || v.IsUndefined() || v.IsNull() {
		return ""
	}
	if v.IsString() {
		return v.String()
	}
	if s, err := v.JSONStringify(); err == nil && s != "" {
		return s
	}
	return v.String()
}

// assembleOutput composes the tool_result content from the captured buffers
// and an optional final-value string + trailing error description.
func assembleOutput(stdout, stderr *bytes.Buffer, final, errSuffix string) string {
	var b strings.Builder
	if stdout.Len() > 0 {
		b.WriteString(stdout.String())
		if !strings.HasSuffix(stdout.String(), "\n") {
			b.WriteByte('\n')
		}
	}
	if stderr.Len() > 0 {
		b.WriteString("[stderr]\n")
		b.WriteString(stderr.String())
		if !strings.HasSuffix(stderr.String(), "\n") {
			b.WriteByte('\n')
		}
	}
	if final != "" {
		const maxFinal = 8 * 1024
		if len(final) > maxFinal {
			final = final[:maxFinal] + "… [truncated]"
		}
		b.WriteString("\n[script result]\n")
		b.WriteString(final)
		b.WriteByte('\n')
	}
	if errSuffix != "" {
		b.WriteString("\n")
		b.WriteString(errSuffix)
		b.WriteByte('\n')
	}
	return b.String()
}
