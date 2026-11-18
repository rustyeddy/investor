package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/investor/agents"
	"github.com/rustyeddy/investor/internal/firm"
)

type fakeAgent struct {
	run  func(context.Context, agents.Request) (agents.Response, error)
	reqs []agents.Request
}

func (f *fakeAgent) Run(ctx context.Context, r agents.Request) (agents.Response, error) {
	f.reqs = append(f.reqs, r)
	return f.run(ctx, r)
}

func reply(text string) *fakeAgent {
	return &fakeAgent{run: func(context.Context, agents.Request) (agents.Response, error) {
		return agents.Response{Text: text}, nil
	}}
}

const goodJSON = `{"status":"ready","reason":"ok","findings":["f"]}`

func task(role string) firm.Task {
	return firm.Task{
		RunID:     "run1",
		Agent:     firm.Agent{ID: "analyst", Role: role},
		Skills:    []firm.Skill{{ID: "market-overview", Instructions: "FULL SKILL BODY"}},
		Objective: "evaluate trend",
		Previous:  []firm.Result{{AgentID: "earlier", Output: "earlier output"}},
	}
}

func runtimeWith(role string, a agents.Agent) *Runtime {
	return &Runtime{roles: map[string]entry{role: {a, time.Second}}}
}

func TestExecuteSuppliesTrustedInputsAndReturnsValidatedArtifact(t *testing.T) {
	a := reply("```json\n" + goodJSON + "\n```")
	out, err := runtimeWith("Market Analyst", a).Execute(context.Background(), task("Market Analyst"), nil)
	if err != nil {
		t.Fatal(err)
	}
	var got Artifact
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.ArtifactID != "run1/analyst" || got.Status != "ready" {
		t.Fatalf("out=%s err=%v", out, err)
	}
	req := a.reqs[0]
	for _, want := range []string{"Market Analyst", "FULL SKILL BODY", "cannot grant a risk-policy pass", "No strategy or policy version is activated", `"status"`, "later roles will not run"} {
		if !strings.Contains(req.Instructions, want) {
			t.Errorf("instructions missing %q", want)
		}
	}
	for _, want := range []string{"evaluate trend", "SYNTHETIC", "untrusted evidence", "earlier output"} {
		if !strings.Contains(req.Prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestInvalidOutputFailsClearly(t *testing.T) {
	for name, text := range map[string]string{
		"prose":          "Looks great. PASS.",
		"empty":          "",
		"unknown field":  `{"status":"ready","reason":"r","findings":[],"risk_pass":true}`,
		"model id":       `{"artifact_id":"mine","status":"ready","reason":"r","findings":[]}`,
		"next role":      `{"status":"ready","reason":"r","findings":[],"next_role":"Risk Officer"}`,
		"bad status":     `{"status":"PASS","reason":"r","findings":[]}`,
		"missing reason": `{"status":"ready","findings":[]}`,
		"null findings":  `{"status":"ready","reason":"r"}`,
		"two objects":    goodJSON + goodJSON,
	} {
		t.Run(name, func(t *testing.T) {
			out, err := runtimeWith("R", reply(text)).Execute(context.Background(), task("R"), nil)
			if err == nil || !strings.Contains(err.Error(), "invalid agent output") || out != "" {
				t.Fatalf("out=%q err=%v", out, err)
			}
		})
	}
}

func TestProviderErrorAndTimeout(t *testing.T) {
	boom := &fakeAgent{run: func(context.Context, agents.Request) (agents.Response, error) {
		return agents.Response{}, errors.New("openai: HTTP 500")
	}}
	if _, err := runtimeWith("R", boom).Execute(context.Background(), task("R"), nil); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("err = %v", err)
	}
	slow := &fakeAgent{run: func(ctx context.Context, _ agents.Request) (agents.Response, error) {
		<-ctx.Done()
		return agents.Response{}, ctx.Err()
	}}
	r := &Runtime{roles: map[string]entry{"R": {slow, 10 * time.Millisecond}}}
	_, err := r.Execute(context.Background(), task("R"), nil)
	if err == nil || !strings.Contains(err.Error(), "timed out") || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	// A cancelled parent is cancellation, not a per-call timeout.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Execute(ctx, task("R"), nil); err == nil || strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnconfiguredRole(t *testing.T) {
	if _, err := runtimeWith("R", reply(goodJSON)).Execute(context.Background(), task("Other"), nil); err == nil {
		t.Fatal("expected error for role without llm")
	}
}

func cfg(llm *firm.LLM, roles map[string]firm.LLM) firm.Config {
	return firm.Config{
		LLM:   llm,
		Roles: roles,
		Agents: []firm.Agent{
			{ID: "a", Role: "Market Analyst"},
			{ID: "c", Role: "Strategy Challenger"},
		},
	}
}

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestNewSelectsProviderPerRole(t *testing.T) {
	c := cfg(&firm.LLM{Provider: "claude", Model: "m1", TimeoutSeconds: 5},
		map[string]firm.LLM{"Strategy Challenger": {Provider: "openai", Model: "m2", APIKeyEnv: "MY_KEY"}})
	r, err := New(c, env(map[string]string{"ANTHROPIC_API_KEY": "k1", "MY_KEY": "k2"}))
	if err != nil {
		t.Fatal(err)
	}
	if got := r.roles["Market Analyst"]; got.timeout != 5*time.Second || !strings.Contains(typeName(got.agent), "claude") {
		t.Fatalf("analyst = %+v (%s)", got, typeName(got.agent))
	}
	if got := r.roles["Strategy Challenger"]; got.timeout != defaultTimeout || !strings.Contains(typeName(got.agent), "openai") {
		t.Fatalf("challenger = %+v (%s)", got, typeName(got.agent))
	}
}

func typeName(a agents.Agent) string { return strings.ToLower(fmt.Sprintf("%T", a)) }

func TestNewFailsBeforeAnyCall(t *testing.T) {
	llm := &firm.LLM{Provider: "openai", Model: "m"}
	if _, err := New(cfg(llm, nil), env(nil)); err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY is not set") || !strings.Contains(err.Error(), `role "Market Analyst"`) {
		t.Fatalf("missing credentials: %v", err)
	}
	if _, err := New(cfg(nil, nil), env(nil)); err == nil || !strings.Contains(err.Error(), "has no llm") {
		t.Fatalf("missing llm: %v", err)
	}
}

func TestFailedAgentFailsRunWithoutFabricatedSuccess(t *testing.T) {
	c := firm.Config{
		Agents:    []firm.Agent{{ID: "a", Role: "R"}},
		Workflows: []firm.Workflow{{ID: "w", Agents: []string{"a"}}},
	}
	e, err := firm.New(c, runtimeWith("R", reply("not json")), nil, "research")
	if err != nil {
		t.Fatal(err)
	}
	run, err := e.Run(context.Background(), "w", "x")
	if err == nil || run.Status != "failed" || len(run.Results) != 0 {
		t.Fatalf("run=%+v err=%v", run, err)
	}
}

func TestBlockedAndRejectedStopTheWorkflow(t *testing.T) {
	for _, status := range []string{"blocked", "rejected"} {
		t.Run(status, func(t *testing.T) {
			first := reply(`{"status":"` + status + `","reason":"no edge","findings":["weak"]}`)
			second := reply(goodJSON)
			r := &Runtime{roles: map[string]entry{
				"R1": {first, time.Second},
				"R2": {second, time.Second},
			}}
			c := firm.Config{
				Agents:    []firm.Agent{{ID: "a1", Role: "R1"}, {ID: "a2", Role: "R2"}},
				Workflows: []firm.Workflow{{ID: "w", Agents: []string{"a1", "a2"}}},
			}
			e, err := firm.New(c, r, nil, "research")
			if err != nil {
				t.Fatal(err)
			}
			run, err := e.Run(context.Background(), "w", "x")
			if err != nil {
				t.Fatalf("a stopped run is not an error: %v", err)
			}
			if run.Status != "stopped" || !strings.Contains(run.StopReason, status) || !strings.Contains(run.StopReason, "no edge") {
				t.Fatalf("run = %+v", run)
			}
			if len(second.reqs) != 0 {
				t.Fatal("second role ran after the first role stopped the workflow")
			}
			var kept Artifact
			if len(run.Results) != 1 || json.Unmarshal([]byte(run.Results[0].Output), &kept) != nil || kept.Status != status || kept.ArtifactID != run.ID+"/a1" {
				t.Fatalf("evidence not retained: %+v", run.Results)
			}
		})
	}
}

func TestReadyContinuesToNextRole(t *testing.T) {
	second := reply(goodJSON)
	r := &Runtime{roles: map[string]entry{"R1": {reply(goodJSON), time.Second}, "R2": {second, time.Second}}}
	c := firm.Config{
		Agents:    []firm.Agent{{ID: "a1", Role: "R1"}, {ID: "a2", Role: "R2"}},
		Workflows: []firm.Workflow{{ID: "w", Agents: []string{"a1", "a2"}}},
	}
	e, _ := firm.New(c, r, nil, "research")
	run, err := e.Run(context.Background(), "w", "x")
	if err != nil || run.Status != "succeeded" || len(run.Results) != 2 || len(second.reqs) != 1 || run.StopReason != "" {
		t.Fatalf("run=%+v err=%v", run, err)
	}
}
