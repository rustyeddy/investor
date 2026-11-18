package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rustyeddy/investor/internal/firm"
	"github.com/rustyeddy/investor/internal/journal"
	"github.com/rustyeddy/investor/internal/service"
	"github.com/rustyeddy/investor/internal/transport"
)

func TestCommandsRejectMissingSkillBeforeExecution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "firm.json")
	data := `{"skills":[{"id":"missing"}],"agents":[{"id":"a","role":"analyst","skills":["missing"]}],"workflows":[{"id":"research","agents":["a"]}]}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"validate", "plan", "run", "serve"} {
		var out, errOut bytes.Buffer
		err := execute(context.Background(), []string{command, "--config", path}, &out, &errOut)
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
		var out, errOut bytes.Buffer
		// The example config names a journal; keep tests out of the repository.
		args := []string{command, "--config", path}
		if command == "run" {
			args = append(args, "--journal", filepath.Join(t.TempDir(), "journal"))
		}
		if err := execute(context.Background(), args, &out, &errOut); err != nil {
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
	transport.Handler(service.New(c, e)).ServeHTTP(w, httptest.NewRequest("GET", "/api/manager/skills", nil))
	var skills []firm.Skill
	if err := json.Unmarshal(w.Body.Bytes(), &skills); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(skills) != 6 || skills[0].Description == "" || !strings.Contains(skills[0].Instructions, "Required observations") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestUsageAndInvalidInvocations(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := execute(context.Background(), nil, &out, &errOut); err != nil || !strings.Contains(out.String(), "validate") {
		t.Fatalf("help: output=%q err=%v", out.String(), err)
	}
	for _, args := range [][]string{{"bogus"}, {"validate", "extra"}, {"plan", "--nope"}} {
		out.Reset()
		errOut.Reset()
		if err := execute(context.Background(), args, &out, &errOut); err == nil {
			t.Fatalf("%v: expected error", args)
		}
		if out.Len() != 0 || errOut.Len() != 0 {
			t.Fatalf("%v: errors must be returned, not written: stdout=%q stderr=%q", args, out.String(), errOut.String())
		}
	}
}

func TestConfigFlagPositionAndHelpWithoutConfig(t *testing.T) {
	path, err := filepath.Abs("../../examples/firm.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--config", path, "validate"},
		{"validate", "--config", path},
	} {
		var out, errOut bytes.Buffer
		if err := execute(context.Background(), args, &out, &errOut); err != nil || !json.Valid(out.Bytes()) || errOut.Len() != 0 {
			t.Fatalf("%v: stdout=%q stderr=%q err=%v", args, out.String(), errOut.String(), err)
		}
	}
	missing := filepath.Join(t.TempDir(), "missing.json")
	for _, args := range [][]string{{"--config", missing, "--help"}, {"run", "--config", missing, "--help"}} {
		var out, errOut bytes.Buffer
		if err := execute(context.Background(), args, &out, &errOut); err != nil || out.Len() == 0 || errOut.Len() != 0 {
			t.Fatalf("%v: stdout=%q stderr=%q err=%v", args, out.String(), errOut.String(), err)
		}
	}
}

func TestFailuresKeepStdoutClean(t *testing.T) {
	var out, errOut bytes.Buffer
	missing := filepath.Join(t.TempDir(), "missing.json")
	if err := execute(context.Background(), []string{"validate", "--config", missing}, &out, &errOut); err == nil || out.Len() != 0 || errOut.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q err=%v", out.String(), errOut.String(), err)
	}
}

func TestRunHonorsContextCancellation(t *testing.T) {
	path, err := filepath.Abs("../../examples/firm.json")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	err = execute(ctx, []string{"run", "--config", path, "--journal", filepath.Join(t.TempDir(), "journal")}, &out, &errOut)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	var run firm.Run
	if err := json.Unmarshal(out.Bytes(), &run); err != nil || run.Status != "canceled" || len(run.Results) != 0 {
		t.Fatalf("run=%+v err=%v stdout=%q", run, err, out.String())
	}
}

func TestModelRuntimeFailsClearlyWithoutCredentials(t *testing.T) {
	path, err := filepath.Abs("../../examples/firm.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "")
	var out, errOut bytes.Buffer
	err = execute(context.Background(), []string{"run", "--runtime", "model", "--config", path}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY is not set") || out.Len() != 0 {
		t.Fatalf("stdout=%q err=%v", out.String(), err)
	}
	err = execute(context.Background(), []string{"run", "--runtime", "bogus", "--config", path}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "unknown runtime") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunJournalRecordsRunAndSteps(t *testing.T) {
	path, err := filepath.Abs("../../examples/firm.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "journal")
	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"run", "--config", path, "--journal", dir}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	var run firm.Run
	if err := json.Unmarshal(out.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	j, err := journal.Open(dir) // A fresh open, as after a restart.
	if err != nil {
		t.Fatal(err)
	}
	if r, err := j.Latest("run", run.ID); err != nil || r.Revision != 2 {
		t.Fatalf("run record: %+v, %v", r, err)
	}
	if ids, err := j.List("step"); err != nil || len(ids) != 5 {
		t.Fatalf("steps: %v, %v", ids, err)
	}
	// Without --journal nothing is written.
	if _, err := os.Stat(filepath.Join(t.TempDir(), "journal")); err == nil {
		t.Fatal("unexpected journal")
	}
}

// configWith writes a copy of the example config into a temp dir with extra
// top-level keys, absolute skills_dir, and returns its path.
func configWith(t *testing.T, extra map[string]any) string {
	t.Helper()
	example, err := filepath.Abs("../../examples/firm.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(example)
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	c["skills_dir"] = filepath.Join(filepath.Dir(example), "..", "skills")
	delete(c, "journal_dir")
	for k, v := range extra {
		c[k] = v
	}
	out, _ := json.Marshal(c)
	path := filepath.Join(t.TempDir(), "firm.json")
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestModelRuntimeRequiresAJournal(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	path := configWith(t, nil) // No journal_dir.
	var out, errOut bytes.Buffer
	err := execute(context.Background(), []string{"run", "--runtime", "model", "--config", path}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "requires a journal") || !strings.Contains(err.Error(), "journal_dir") || out.Len() != 0 {
		t.Fatalf("stdout=%q err=%v", out.String(), err)
	}
	// Credentials failing must not create the journal directory.
	t.Setenv("ANTHROPIC_API_KEY", "")
	dir := filepath.Join(t.TempDir(), "j")
	err = execute(context.Background(), []string{"run", "--runtime", "model", "--journal", dir, "--config", path}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY is not set") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("journal directory created before the run could start")
	}
}

func TestJournalDirFromConfigIsRelativeToConfigAndFlagOverrides(t *testing.T) {
	path := configWith(t, map[string]any{"journal_dir": "records-here"})
	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"run", "--config", path}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	var run firm.Run
	if err := json.Unmarshal(out.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	j, err := journal.Open(filepath.Join(filepath.Dir(path), "records-here"))
	if err != nil {
		t.Fatal(err)
	}
	if r, err := j.Latest("run", run.ID); err != nil || r.Revision != 2 {
		t.Fatalf("run not journaled in the config's journal_dir: %+v, %v", r, err)
	}
	// --journal wins over the config file.
	override := filepath.Join(t.TempDir(), "other")
	out.Reset()
	if err := execute(context.Background(), []string{"run", "--config", path, "--journal", override}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if ids, err := (func() ([]string, error) {
		o, e := journal.Open(override)
		if e != nil {
			return nil, e
		}
		return o.List("run")
	})(); err != nil || len(ids) != 1 {
		t.Fatalf("override journal: %v, %v", ids, err)
	}
	if ids, _ := j.List("run"); len(ids) != 1 {
		t.Fatalf("flag run leaked into the config journal: %v", ids)
	}
	// Commands that do not execute a workflow never open a journal.
	other := configWith(t, map[string]any{"journal_dir": "never-created"})
	for _, c := range []string{"validate", "plan"} {
		if err := execute(context.Background(), []string{c, "--config", other}, &out, &errOut); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(other), "never-created")); err == nil {
		t.Fatal("validate/plan created a journal")
	}
}
