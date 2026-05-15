package skills

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSkill(t *testing.T) {
	content := `---
name: refactor
description: A skill for refactors.
triggers:
  - refactor
  - cleanup
---
Do the refactor.
`
	s, err := ParseSkill("test.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "refactor" || s.Description != "A skill for refactors." {
		t.Fatalf("metadata = %+v", s)
	}
	if len(s.Triggers) != 2 {
		t.Fatalf("triggers = %v", s.Triggers)
	}
	if !strings.Contains(s.Body, "Do the refactor.") {
		t.Fatalf("body = %q", s.Body)
	}
}

func TestParseSkillNoFrontmatterErrors(t *testing.T) {
	// Skill without frontmatter (no `name`) should error.
	_, err := ParseSkill("a.md", "just a body")
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "a.md"), []byte("---\nname: a\ndescription: A\n---\nA body."), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(skillDir, "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "SKILL.md"), []byte("---\nname: b\ndescription: B\n---\nB body."), 0o644); err != nil {
		t.Fatal(err)
	}

	skills, err := Load([]string{skillDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 {
		t.Fatalf("want 2 skills, got %d: %+v", len(skills), skills)
	}
	if skills[0].Name != "a" || skills[1].Name != "b" {
		t.Fatalf("names = %s, %s", skills[0].Name, skills[1].Name)
	}
}

func TestLoadShadowingByOrder(t *testing.T) {
	d1 := t.TempDir()
	d2 := t.TempDir()
	_ = os.WriteFile(filepath.Join(d1, "a.md"), []byte("---\nname: a\ndescription: from d1\n---\nbody1"), 0o644)
	_ = os.WriteFile(filepath.Join(d2, "a.md"), []byte("---\nname: a\ndescription: from d2\n---\nbody2"), 0o644)
	skills, err := Load([]string{d1, d2})
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Description != "from d1" {
		t.Fatalf("got %+v", skills)
	}
}

func TestTool(t *testing.T) {
	tt := NewTool([]Skill{
		{Name: "x", Description: "X skill", Body: "X body"},
		{Name: "y", Description: "Y skill", Body: "Y body"},
	})

	t.Run("list", func(t *testing.T) {
		r, _ := tt.Run(context.Background(), nil)
		if r.IsError || !strings.Contains(r.Content, "x:") || !strings.Contains(r.Content, "y:") {
			t.Fatalf("got %+v", r)
		}
	})

	t.Run("get", func(t *testing.T) {
		r, _ := tt.Run(context.Background(), json.RawMessage(`{"name":"x"}`))
		if r.IsError || r.Content != "X body" {
			t.Fatalf("got %+v", r)
		}
	})

	t.Run("unknown", func(t *testing.T) {
		r, _ := tt.Run(context.Background(), json.RawMessage(`{"name":"nope"}`))
		if !r.IsError {
			t.Fatalf("expected error for unknown skill, got %+v", r)
		}
	})
}

func TestSystemPromptAddition(t *testing.T) {
	if SystemPromptAddition(nil) != "" {
		t.Fatal("empty skills should produce empty addition")
	}
	addn := SystemPromptAddition([]Skill{{Name: "x", Description: "Xd"}})
	if !strings.Contains(addn, "x: Xd") {
		t.Fatalf("got %q", addn)
	}
}
