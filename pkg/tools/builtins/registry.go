package builtins

import "github.com/russellhaering/autoswe/pkg/tools"

// All returns the set of built-in tools shipped with autoswe.
func All() []tools.Tool {
	return []tools.Tool{Read, Write, Edit, Bash, Glob, Grep}
}

// DefaultRegistry returns a new Registry pre-populated with all built-ins.
func DefaultRegistry() *tools.Registry {
	r := tools.NewRegistry()
	for _, t := range All() {
		r.MustRegister(t)
	}
	return r
}
