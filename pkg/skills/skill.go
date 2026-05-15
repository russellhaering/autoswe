// Package skills loads markdown-with-frontmatter skill files from the user's
// home and the project's working directory, and exposes them to the agent via
// a single `skill` meta-tool.
package skills

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill is one parsed skill: frontmatter metadata + markdown body.
type Skill struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Triggers    []string `yaml:"triggers,omitempty"`
	Body        string   `yaml:"-"`
	Path        string   `yaml:"-"`
}

// frontmatterRe / parseFrontmatter splits a markdown file into its YAML
// frontmatter and body. A file without a leading `---\n…\n---\n` block has
// empty frontmatter and the whole file as body.
func parseFrontmatter(content string) (front, body string, err error) {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return "", content, nil
	}
	// Find the closing delimiter.
	after := strings.TrimPrefix(strings.TrimPrefix(content, "---\n"), "---\r\n")
	end := strings.Index(after, "\n---\n")
	endCR := strings.Index(after, "\r\n---\r\n")
	switch {
	case end == -1 && endCR == -1:
		return "", "", errors.New("frontmatter: missing closing ---")
	case endCR != -1 && (end == -1 || endCR < end):
		return after[:endCR], after[endCR+len("\r\n---\r\n"):], nil
	default:
		return after[:end], after[end+len("\n---\n"):], nil
	}
}

// ParseSkill parses one skill from raw markdown content. Path is recorded for
// diagnostics.
func ParseSkill(path, content string) (Skill, error) {
	front, body, err := parseFrontmatter(content)
	if err != nil {
		return Skill{}, fmt.Errorf("skill %s: %w", path, err)
	}
	s := Skill{Body: strings.TrimSpace(body), Path: path}
	if front != "" {
		if err := yaml.Unmarshal([]byte(front), &s); err != nil {
			return Skill{}, fmt.Errorf("skill %s: parse frontmatter: %w", path, err)
		}
	}
	if s.Name == "" {
		return Skill{}, fmt.Errorf("skill %s: name is required in frontmatter", path)
	}
	return s, nil
}
