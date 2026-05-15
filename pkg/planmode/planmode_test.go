package planmode

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/russellhaering/autoswe/pkg/tools"
)

func TestExitPlanModeReturnsExitAfter(t *testing.T) {
	res, err := ExitPlanMode{}.Run(context.Background(), json.RawMessage(`{"plan":"# Plan\n- step"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.ExitAfter {
		t.Fatal("expected ExitAfter=true")
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}
	if !strings.Contains(string(res.Metadata), "autoswe_plan_submitted") {
		t.Fatalf("metadata = %s", res.Metadata)
	}
}

func TestExitPlanModeRequiresPlan(t *testing.T) {
	res, _ := ExitPlanMode{}.Run(context.Background(), json.RawMessage(`{}`))
	if !res.IsError {
		t.Fatalf("missing plan should error, got %+v", res)
	}
}

func TestRegistryIsReadOnlyPlusExit(t *testing.T) {
	r := Registry()
	names := r.Names()
	hasExit := false
	for _, n := range names {
		if n == "exit_plan_mode" {
			hasExit = true
			continue
		}
		tool, _ := r.Get(n)
		for _, e := range tool.Effects() {
			if e != tools.EffectReadOnly {
				t.Fatalf("non-read-only tool %q in plan registry (effect %s)", n, e)
			}
		}
	}
	if !hasExit {
		t.Fatal("exit_plan_mode missing from plan registry")
	}
}
