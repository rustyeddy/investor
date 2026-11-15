package firm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const skillText = "---\nname: sample\ndescription: >-\n  Explain observations\n  with evidence.\nmetadata:\n  version: 1\n---\n\nKeep source timestamps.\n"

func TestParseSkill(t *testing.T) {
	for _, input := range []string{skillText, strings.ReplaceAll(skillText, "\n", "\r\n")} {
		s, err := parseSkill("sample", input)
		if err != nil {
			t.Fatal(err)
		}
		if s.ID != "sample" || s.Description != "Explain observations with evidence." || s.Instructions != "Keep source timestamps." {
			t.Fatalf("%+v", s)
		}
	}
	for _, tc := range []struct{ name, text, want string }{
		{"no frontmatter", "body", "opening"},
		{"unclosed", "---\nname: sample", "closing"},
		{"yaml", "---\nname: [\n---\nbody", "invalid YAML"},
		{"name", "---\ndescription: test\n---\nbody", "name must"},
		{"mismatch", "---\nname: other\ndescription: test\n---\nbody", "does not match"},
		{"blank description", "---\nname: sample\ndescription: ' '\n---\nbody", "description must"},
		{"non-string", "---\nname: sample\ndescription: 123\n---\nbody", "description must"},
		{"sequence", "---\n- name\n---\nbody", "invalid YAML"},
		{"duplicate", "---\nname: sample\nname: other\ndescription: test\n---\nbody", "invalid YAML"},
		{"body", "---\nname: sample\ndescription: test\n---\n  ", "empty Markdown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseSkill("sample", tc.text)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
		})
	}
}

func writeFixture(t *testing.T, root, name, data string) string {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func configText(t *testing.T, dir, id string) string {
	t.Helper()
	c := Config{SkillsDir: dir, Skills: []Skill{{ID: id}}, Agents: []Agent{{ID: "a", Role: "analyst", Skills: []string{id}}}, Workflows: []Workflow{{ID: "w", Agents: []string{"a"}}}}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLoadRelativeAndRuntimeSnapshot(t *testing.T) {
	root := t.TempDir()
	path := writeFixture(t, root, "config/firm.json", configText(t, "../skills", "sample"))
	file := writeFixture(t, root, "skills/sample/SKILL.md", skillText)
	t.Chdir(t.TempDir())
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(c, runtimeFunc(func(_ context.Context, task Task, _ CallTool) (string, error) {
		if len(task.Skills) != 1 || task.Skills[0].Instructions != "Keep source timestamps." || task.Skills[0].Description != "Explain observations with evidence." {
			t.Fatalf("%+v", task)
		}
		return "ok", nil
	}), nil, "research")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Run(context.Background(), "w", ""); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDefaultsLegacyAndErrors(t *testing.T) {
	root := t.TempDir()
	path := writeFixture(t, root, "firm.json", configText(t, "", "sample"))
	writeFixture(t, root, "skills/sample/SKILL.md", skillText)
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "firm.json", configText(t, filepath.Join(root, "skills"), "sample"))
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../escape", "/absolute", "bad_name", "bad--name", strings.Repeat("a", 64), "missing"} {
		writeFixture(t, root, "firm.json", configText(t, "skills", id))
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), id) {
			t.Fatalf("%q: %v", id, err)
		}
	}
	writeFixture(t, root, "firm.json", configText(t, "skills", "sample"))
	writeFixture(t, root, "skills/sample/SKILL.md", "---\nname: sample\n---\nbody")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "SKILL.md") || !strings.Contains(err.Error(), "description") {
		t.Fatal(err)
	}
	c := fixture()
	c.SkillsDir = "does-not-exist"
	data, _ := json.Marshal(c)
	writeFixture(t, root, "firm.json", string(data))
	if _, err := Load(path); err != nil {
		t.Fatalf("legacy inline: %v", err)
	}
	if _, err := Decode(strings.NewReader(string(data))); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFixture(t, outside, "SKILL.md", skillText)
	path := writeFixture(t, root, "firm.json", configText(t, "skills", "sample"))
	if err := os.Mkdir(filepath.Join(root, "skills"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "skills", "sample")); err != nil {
		t.Skip(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("loaded skill outside root")
	}
}
