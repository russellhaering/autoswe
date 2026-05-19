package codemode

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var jsIdentRe = regexp.MustCompile(`^[A-Za-z_$][\w$]*$`)

// isValidJSIdent reports whether name can be used as a top-level JS function
// identifier. Tool names that fail this check are skipped from the code-mode
// surface entirely (rather than mangled) so the mapping stays unambiguous.
func isValidJSIdent(name string) bool {
	return jsIdentRe.MatchString(name)
}

// toTSDeclaration renders a single `declare function ...` block for a tool
// from its JSON Schema. It only handles the subset of JSON Schema autoswe
// built-ins emit (object root with string/number/integer/boolean/array/
// inline-object fields, plus enum). Anything outside that subset falls
// through to `any` with a "// schema: <raw>" comment so the model still
// has the ground truth.
func toTSDeclaration(name, description string, schema json.RawMessage) string {
	args := tsObjectType(schema)
	var b strings.Builder
	if d := firstLine(description); d != "" {
		fmt.Fprintf(&b, "/** %s */\n", d)
	}
	fmt.Fprintf(&b, "declare function %s(args: %s): Promise<string>;\n", name, args)
	return b.String()
}

// tsObjectType renders the TS type for the top-level object schema.
func tsObjectType(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "any"
	}
	var s schemaNode
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Sprintf("any /* schema: %s */", oneLineRaw(raw))
	}
	if s.Type != "object" || s.Properties == nil {
		return fmt.Sprintf("any /* schema: %s */", oneLineRaw(raw))
	}
	return renderObject(s)
}

func renderObject(s schemaNode) string {
	required := map[string]struct{}{}
	for _, r := range s.Required {
		required[r] = struct{}{}
	}
	names := make([]string, 0, len(s.Properties))
	for k := range s.Properties {
		names = append(names, k)
	}
	sort.Strings(names)

	var fields []string
	for _, k := range names {
		raw := s.Properties[k]
		ts := renderType(raw)
		opt := ""
		if _, ok := required[k]; !ok {
			opt = "?"
		}
		fields = append(fields, fmt.Sprintf("%s%s: %s", tsKey(k), opt, ts))
	}
	if len(fields) == 0 {
		return "{}"
	}
	return "{ " + strings.Join(fields, "; ") + " }"
}

func renderType(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "any"
	}
	var s schemaNode
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Sprintf("any /* schema: %s */", oneLineRaw(raw))
	}
	if len(s.Enum) > 0 {
		return renderEnum(s.Enum)
	}
	switch s.Type {
	case "string":
		return "string"
	case "integer", "number":
		return "number"
	case "boolean":
		return "boolean"
	case "array":
		if len(s.Items) == 0 {
			return "any[]"
		}
		return renderType(s.Items) + "[]"
	case "object":
		if s.Properties == nil {
			return "Record<string, any>"
		}
		return renderObject(s)
	default:
		return fmt.Sprintf("any /* schema: %s */", oneLineRaw(raw))
	}
}

func renderEnum(values []json.RawMessage) string {
	if len(values) == 0 {
		return "string"
	}
	parts := make([]string, 0, len(values))
	for _, v := range values {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			parts = append(parts, fmt.Sprintf("%q", s))
			continue
		}
		var n float64
		if err := json.Unmarshal(v, &n); err == nil {
			parts = append(parts, fmt.Sprintf("%v", n))
			continue
		}
		var b bool
		if err := json.Unmarshal(v, &b); err == nil {
			parts = append(parts, fmt.Sprintf("%v", b))
			continue
		}
		parts = append(parts, "any")
	}
	return strings.Join(parts, " | ")
}

type schemaNode struct {
	Type       string                     `json:"type"`
	Properties map[string]json.RawMessage `json:"properties"`
	Required   []string                   `json:"required"`
	Items      json.RawMessage            `json:"items"`
	Enum       []json.RawMessage          `json:"enum"`
}

// tsKey quotes a property key if it isn't a valid JS identifier.
func tsKey(k string) string {
	if isValidJSIdent(k) {
		return k
	}
	return fmt.Sprintf("%q", k)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func oneLineRaw(raw json.RawMessage) string {
	s := string(raw)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
