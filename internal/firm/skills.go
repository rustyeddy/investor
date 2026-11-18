package firm

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var skillIDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func validSkillID(id string) bool { return len(id) <= 63 && skillIDPattern.MatchString(id) }

// Load resolves file-backed skills and the journal directory relative to the
// configuration file. Legacy
// inline instructions remain supported. No filesystem access occurs during runs.
func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	c, err := decode(f)
	_ = f.Close()
	if err != nil {
		return c, err
	}
	dir := c.SkillsDir
	if dir == "" {
		dir = "skills"
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(filepath.Dir(path), dir)
	}
	var root *os.Root
	defer func() {
		if root != nil {
			_ = root.Close()
		}
	}()
	for i, s := range c.Skills {
		// Validate the ID before it is used to build a filesystem path.
		if !validSkillID(s.ID) {
			return c, fmt.Errorf("invalid skill id %q", s.ID)
		}
		if strings.TrimSpace(s.Instructions) != "" {
			continue
		}
		name := filepath.Join(s.ID, "SKILL.md")
		if root == nil {
			root, err = os.OpenRoot(dir)
			if err != nil {
				return c, fmt.Errorf("skill %q: open skills directory %s: %w", s.ID, dir, err)
			}
		}
		data, err := root.ReadFile(name)
		if err != nil {
			return c, fmt.Errorf("skill %q (%s): %w", s.ID, filepath.Join(dir, name), err)
		}
		loaded, err := parseSkill(s.ID, string(data))
		if err != nil {
			return c, fmt.Errorf("skill %q (%s): %w", s.ID, filepath.Join(dir, name), err)
		}
		c.Skills[i] = loaded
	}
	if c.JournalDir != "" && !filepath.IsAbs(c.JournalDir) {
		c.JournalDir = filepath.Join(filepath.Dir(path), c.JournalDir)
	}
	return c, c.Validate()
}

func parseSkill(id, text string) (Skill, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return Skill{}, fmt.Errorf("missing YAML frontmatter opening ---")
	}
	end := 1
	for end < len(lines) && lines[end] != "---" {
		end++
	}
	if end == len(lines) {
		return Skill{}, fmt.Errorf("missing YAML frontmatter closing ---")
	}
	var fields map[string]yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &fields); err != nil {
		return Skill{}, fmt.Errorf("invalid YAML frontmatter: %w", err)
	}
	for _, key := range []string{"name", "description"} {
		n := fields[key]
		if n.Kind != yaml.ScalarNode || n.Tag != "!!str" || strings.TrimSpace(n.Value) == "" {
			return Skill{}, fmt.Errorf("frontmatter %s must be a nonempty string", key)
		}
	}
	if fields["name"].Value != id {
		return Skill{}, fmt.Errorf("frontmatter name %q does not match skill id %q", fields["name"].Value, id)
	}
	body := strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	if body == "" {
		return Skill{}, fmt.Errorf("empty Markdown instructions")
	}
	return Skill{ID: id, Description: strings.TrimSpace(fields["description"].Value), Instructions: body}, nil
}
