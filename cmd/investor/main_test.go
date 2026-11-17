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
		if err := execute(context.Background(), []string{command, "--config", path}, &out, &errOut); err != nil {
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
	err = execute(ctx, []string{"run", "--config", path}, &out, &errOut)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	var run firm.Run
	if err := json.Unmarshal(out.Bytes(), &run); err != nil || run.Status != "canceled" || len(run.Results) != 0 {
		t.Fatalf("run=%+v err=%v stdout=%q", run, err, out.String())
	}
}
