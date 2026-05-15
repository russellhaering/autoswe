package skills

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultDirs returns the standard skill-search directories in priority order:
// the working-directory-local `.autoswe/skills` first, then the user-level
// `~/.autoswe/skills`. Project skills shadow user skills with the same name.
func DefaultDirs() []string {
	dirs := []string{".autoswe/skills"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".autoswe", "skills"))
	}
	return dirs
}

// Load discovers skills from the given directories. Each `.md` file at the
// top of each directory, plus any `SKILL.md` one level deep, is treated as a
// skill. Skills earlier in dirs win on name collisions.
func Load(dirs []string) ([]Skill, error) {
	seen := map[string]struct{}{}
	var skills []Skill
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		dirSkills, err := loadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, s := range dirSkills {
			if _, dup := seen[s.Name]; dup {
				continue
			}
			seen[s.Name] = struct{}{}
			skills = append(skills, s)
		}
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

func loadDir(dir string) ([]Skill, error) {
	var out []Skill
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		switch {
		case e.IsDir():
			candidate := filepath.Join(path, "SKILL.md")
			if _, err := os.Stat(candidate); err == nil {
				s, err := parseFile(candidate)
				if err != nil {
					return nil, err
				}
				out = append(out, s)
			}
		case strings.HasSuffix(e.Name(), ".md") && e.Type().IsRegular():
			s, err := parseFile(path)
			if err != nil {
				return nil, err
			}
			out = append(out, s)
		case e.Type()&fs.ModeSymlink != 0:
			// Skip symlinks for now to avoid infinite recursion.
		}
	}
	return out, nil
}

func parseFile(path string) (Skill, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, fmt.Errorf("read %s: %w", path, err)
	}
	return ParseSkill(path, string(b))
}

// SystemPromptAddition returns a short addendum the agent appends to its
// system prompt so the model knows what skills exist and that there's a tool
// to retrieve their bodies. Empty if skills is empty.
func SystemPromptAddition(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nAvailable skills (call the `skill` tool with the skill's name to read its instructions):\n")
	for _, s := range skills {
		fmt.Fprintf(&b, "- %s: %s\n", s.Name, s.Description)
	}
	return b.String()
}
