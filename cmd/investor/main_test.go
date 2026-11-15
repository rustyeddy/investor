package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rustyeddy/investor/internal/firm"
	"github.com/rustyeddy/investor/internal/service"
)

func TestCommandsRejectMissingSkillBeforeExecution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "firm.json")
	data := `{"skills":[{"id":"missing"}],"agents":[{"id":"a","role":"analyst","skills":["missing"]}],"workflows":[{"id":"research","agents":["a"]}]}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"validate", "plan", "run", "serve"} {
		var out bytes.Buffer
		err := execute(context.Background(), []string{command, "-config", path}, &out)
		if err == nil || !strings.Contains(err.Error(), `skill "missing"`) || out.Len() != 0 {
			t.Fatalf("%s: output=%q err=%v", command, out.String(), err)
		}
	}
}

func TestExampleCommandsAndCatalog(t *testing.T) {
	path, err := filepath.Abs("../../examples/firm.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	for _, command := range []string{"validate", "plan", "run"} {
		var out bytes.Buffer
		if err := execute(context.Background(), []string{command, "-config", path}, &out); err != nil {
			t.Fatalf("%s: %v", command, err)
		}
		if !json.Valid(out.Bytes()) {
			t.Fatalf("%s: %s", command, out.String())
		}
		if command == "run" {
			var run firm.Run
			if err := json.Unmarshal(out.Bytes(), &run); err != nil {
				t.Fatal(err)
			}
			if run.Mode != "dry-run" || run.Status != "succeeded" || len(run.Results) != 5 {
				t.Fatalf("%+v", run)
			}
		}
	}
	c, err := firm.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Skills) != 6 {
		t.Fatalf("skills: %d", len(c.Skills))
	}
	e, err := firm.New(c, firm.DryRun{}, nil, "dry-run")
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	service.Handler(c, e).ServeHTTP(w, httptest.NewRequest("GET", "/api/manager/skills", nil))
	var skills []firm.Skill
	if err := json.Unmarshal(w.Body.Bytes(), &skills); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(skills) != 6 || skills[0].Description == "" || !strings.Contains(skills[0].Instructions, "Required observations") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
