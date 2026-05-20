package builtins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/russellhaering/autoswe/pkg/tools"
)

type patchEdit struct {
	Line    int    `json:"line"`
	Tag     string `json:"tag"`
	EndLine int    `json:"end_line,omitempty"`
	EndTag  string `json:"end_tag,omitempty"`
	New     string `json:"new,omitempty"`
	Action  string `json:"action,omitempty"`
}

type patchArgs struct {
	Path  string      `json:"path"`
	Edits []patchEdit `json:"edits"`
}

type patchTool struct{}

func (patchTool) Name() string { return "patch" }

func (patchTool) Description() string {
	return "Apply tag-checked edits to a file. Each edit references a line by number plus the 4-char tag " +
		"from the most recent `read` output; the tag is verified before the edit applies. Actions: " +
		"\"replace\" (default), \"delete\", \"insert_before\", \"insert_after\". " +
		"Range edits (replace/delete) use end_line + end_tag for the last line of the range. " +
		"`new` is spliced in verbatim — embed \\n to span lines; do not add an implicit trailing newline. " +
		"All edits are validated up front; on any tag mismatch the file is left unchanged. " +
		"Use `write` to create new files."
}

func (patchTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "Path to the file to patch."},
    "edits": {
      "type": "array",
      "description": "List of edits; applied in reverse line order, all-or-nothing on tag mismatch.",
      "items": {
        "type": "object",
        "properties": {
          "line":     {"type": "integer", "minimum": 1, "description": "1-indexed line; first line for ranges."},
          "tag":      {"type": "string",  "description": "4-char tag of line, from a recent read."},
          "end_line": {"type": "integer", "minimum": 1, "description": "Inclusive last line for ranges (replace/delete only)."},
          "end_tag":  {"type": "string",  "description": "Tag of end_line."},
          "new":      {"type": "string",  "description": "Replacement text spliced in verbatim. No implicit trailing newline."},
          "action":   {"type": "string",  "enum": ["replace","delete","insert_before","insert_after"], "description": "Defaults to replace."}
        },
        "required": ["line","tag"]
      }
    }
  },
  "required": ["path","edits"]
}`)
}

func (patchTool) Effects() []tools.Effect { return []tools.Effect{tools.EffectFilesystemWrite} }

func (patchTool) Run(_ context.Context, raw json.RawMessage) (tools.Result, error) {
	var a patchArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("invalid args: %v", err)}, nil
	}
	if a.Path == "" {
		return tools.Result{IsError: true, Content: "path is required"}, nil
	}
	if len(a.Edits) == 0 {
		return tools.Result{IsError: true, Content: "edits is empty"}, nil
	}

	data, err := os.ReadFile(a.Path)
	if err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("read %s: %v", a.Path, err)}, nil
	}
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(a.Path); statErr == nil {
		mode = info.Mode().Perm()
	}

	// Split into line slices, recording whether the file ended with '\n'.
	parts := bytes.Split(data, []byte("\n"))
	hadTrailingNL := false
	if len(parts) > 0 && len(parts[len(parts)-1]) == 0 {
		parts = parts[:len(parts)-1]
		hadTrailingNL = true
	}
	numLines := len(parts)

	type normalized struct {
		lo, hi int // 1-indexed; hi=lo for inserts
		action string
		new    string
		idx    int // original index for error/summary clarity
	}
	norm := make([]normalized, 0, len(a.Edits))

	for i, e := range a.Edits {
		action := e.Action
		if action == "" {
			action = "replace"
		}
		switch action {
		case "replace", "delete", "insert_before", "insert_after":
		default:
			return tools.Result{IsError: true, Content: fmt.Sprintf("edit %d: unknown action %q", i, action)}, nil
		}
		if e.Line < 1 || e.Line > numLines {
			return tools.Result{IsError: true, Content: fmt.Sprintf("edit %d: line %d out of range (file has %d line(s))", i, e.Line, numLines)}, nil
		}
		if got := lineTag(parts[e.Line-1]); e.Tag != got {
			return tools.Result{IsError: true, Content: fmt.Sprintf("edit %d: tag mismatch at line %d: expected %q, found %q; re-read the file", i, e.Line, e.Tag, got)}, nil
		}
		hi := e.Line
		if action == "replace" || action == "delete" {
			if e.EndLine != 0 {
				if e.EndLine < e.Line {
					return tools.Result{IsError: true, Content: fmt.Sprintf("edit %d: end_line %d < line %d", i, e.EndLine, e.Line)}, nil
				}
				if e.EndLine > numLines {
					return tools.Result{IsError: true, Content: fmt.Sprintf("edit %d: end_line %d out of range (file has %d line(s))", i, e.EndLine, numLines)}, nil
				}
				if e.EndTag == "" {
					return tools.Result{IsError: true, Content: fmt.Sprintf("edit %d: end_tag required when end_line is set", i)}, nil
				}
				if got := lineTag(parts[e.EndLine-1]); e.EndTag != got {
					return tools.Result{IsError: true, Content: fmt.Sprintf("edit %d: end_tag mismatch at line %d: expected %q, found %q; re-read the file", i, e.EndLine, e.EndTag, got)}, nil
				}
				hi = e.EndLine
			}
		} else if e.EndLine != 0 || e.EndTag != "" {
			return tools.Result{IsError: true, Content: fmt.Sprintf("edit %d: end_line/end_tag not allowed with action=%s", i, action)}, nil
		}
		norm = append(norm, normalized{lo: e.Line, hi: hi, action: action, new: e.New, idx: i})
	}

	// Overlap check: sort by lo, verify no two intervals touch.
	sorted := append([]normalized(nil), norm...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].lo < sorted[j].lo })
	for i := 1; i < len(sorted); i++ {
		if sorted[i].lo <= sorted[i-1].hi {
			return tools.Result{IsError: true, Content: fmt.Sprintf("overlapping edits: lines %d-%d and %d-%d", sorted[i-1].lo, sorted[i-1].hi, sorted[i].lo, sorted[i].hi)}, nil
		}
	}

	// Apply in descending order so earlier inserts/deletes don't shift indices.
	descending := append([]normalized(nil), norm...)
	sort.Slice(descending, func(i, j int) bool { return descending[i].lo > descending[j].lo })

	for _, e := range descending {
		startIdx := e.lo - 1
		endIdx := e.hi - 1
		newLines := bytes.Split([]byte(e.new), []byte("\n"))
		switch e.action {
		case "replace":
			parts = spliceLines(parts, startIdx, endIdx+1, newLines)
		case "delete":
			parts = spliceLines(parts, startIdx, endIdx+1, nil)
		case "insert_before":
			parts = spliceLines(parts, startIdx, startIdx, newLines)
		case "insert_after":
			parts = spliceLines(parts, startIdx+1, startIdx+1, newLines)
		}
	}

	out := bytes.Join(parts, []byte("\n"))
	if hadTrailingNL {
		out = append(out, '\n')
	}

	if err := atomicWriteFile(a.Path, out, mode); err != nil {
		return tools.Result{IsError: true, Content: fmt.Sprintf("write %s: %v", a.Path, err)}, nil
	}

	var summary bytes.Buffer
	fmt.Fprintf(&summary, "applied %d edit(s) to %s\n", len(a.Edits), a.Path)
	for i, e := range a.Edits {
		action := e.Action
		if action == "" {
			action = "replace"
		}
		if e.EndLine != 0 {
			fmt.Fprintf(&summary, "  %d: %s lines %d-%d\n", i, action, e.Line, e.EndLine)
		} else {
			fmt.Fprintf(&summary, "  %d: %s line %d\n", i, action, e.Line)
		}
	}
	return tools.Result{Content: summary.String()}, nil
}

// spliceLines returns parts with indices [lo,hi) replaced by replacement.
func spliceLines(parts [][]byte, lo, hi int, replacement [][]byte) [][]byte {
	out := make([][]byte, 0, len(parts)-(hi-lo)+len(replacement))
	out = append(out, parts[:lo]...)
	out = append(out, replacement...)
	out = append(out, parts[hi:]...)
	return out
}

// atomicWriteFile writes data to path via tmpfile + rename.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".patch.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// Patch is the built-in patch tool.
var Patch tools.Tool = patchTool{}
