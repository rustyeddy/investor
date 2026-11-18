package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/investor/agents"
	"github.com/rustyeddy/investor/agents/claude"
	"github.com/rustyeddy/investor/agents/openai"
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
		return agents.Response{Text: text, Usage: agents.Usage{InputTokens: 100, OutputTokens: 50}}, nil
	}}
}

// ent is a role entry with a one-second timeout, no retries and no prices.
func ent(a agents.Agent) entry { return entry{agent: a, timeout: time.Second} }

// newRT builds a Runtime with default limits and a 1ms retry backoff.
func newRT(roles map[string]entry) *Runtime {
	return &Runtime{
		roles:   roles,
		limits:  firm.Limits{MaxCallsPerRun: defaultMaxCalls, MaxTokensPerRun: defaultMaxTokens},
		backoff: time.Millisecond,
		ledgers: map[string]*ledger{},
	}
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
	return newRT(map[string]entry{role: ent(a)})
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
	r := newRT(map[string]entry{"R": {agent: slow, timeout: 10 * time.Millisecond}})
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
			r := newRT(map[string]entry{"R1": ent(first), "R2": ent(second)})
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
	r := newRT(map[string]entry{"R1": ent(reply(goodJSON)), "R2": ent(second)})
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

// flaky fails with err for the first n calls, then returns goodJSON.
func flaky(n int, err error) *fakeAgent {
	calls := 0
	return &fakeAgent{run: func(context.Context, agents.Request) (agents.Response, error) {
		calls++
		if calls <= n {
			return agents.Response{}, err
		}
		return agents.Response{Text: goodJSON, Usage: agents.Usage{InputTokens: 100, OutputTokens: 50}}, nil
	}}
}

func TestRetriesTransientErrorsWithinBound(t *testing.T) {
	transient := &agents.StatusError{Provider: "claude", Status: 503, Body: "busy"}
	a := flaky(2, transient)
	e := ent(a)
	e.retries = 2
	if _, err := newRT(map[string]entry{"R": e}).Execute(context.Background(), task("R"), nil); err != nil || len(a.reqs) != 3 {
		t.Fatalf("calls=%d err=%v", len(a.reqs), err)
	}
	// Exhausted retries fail with the provider error, after retries+1 calls.
	a = flaky(100, transient)
	e = ent(a)
	e.retries = 2
	_, err := newRT(map[string]entry{"R": e}).Execute(context.Background(), task("R"), nil)
	var se *agents.StatusError
	if !errors.As(err, &se) || len(a.reqs) != 3 {
		t.Fatalf("calls=%d err=%v", len(a.reqs), err)
	}
}

func TestDoesNotRetryPermanentErrorsOrBadOutput(t *testing.T) {
	for name, a := range map[string]*fakeAgent{
		"auth":    flaky(100, &agents.StatusError{Provider: "claude", Status: 401}),
		"timeout": flaky(100, context.DeadlineExceeded),
		"output":  reply("not json"),
	} {
		t.Run(name, func(t *testing.T) {
			e := ent(a)
			e.retries = 3
			if _, err := newRT(map[string]entry{"R": e}).Execute(context.Background(), task("R"), nil); err == nil || len(a.reqs) != 1 {
				t.Fatalf("calls=%d err=%v", len(a.reqs), err)
			}
		})
	}
}

func TestRetryBackoffHonorsCancellation(t *testing.T) {
	a := flaky(100, &agents.StatusError{Status: 503})
	e := ent(a)
	e.retries = 5
	r := newRT(map[string]entry{"R": e})
	r.backoff = time.Hour
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := r.Execute(ctx, task("R"), nil); !errors.Is(err, context.DeadlineExceeded) || len(a.reqs) != 1 {
		t.Fatalf("calls=%d err=%v", len(a.reqs), err)
	}
}

func TestLimitsStopFurtherCalls(t *testing.T) {
	for name, tc := range map[string]struct {
		limits firm.Limits
		price  firm.LLM
		want   int // Max calls made before the limit stops the run.
	}{
		"calls":  {firm.Limits{MaxCallsPerRun: 2, MaxTokensPerRun: 1e9}, firm.LLM{}, 2},
		"tokens": {firm.Limits{MaxCallsPerRun: 100, MaxTokensPerRun: 300}, firm.LLM{}, 2},                                                                        // 150 tokens per call.
		"cost":   {firm.Limits{MaxCallsPerRun: 100, MaxTokensPerRun: 1e9, MaxCostUSDPerRun: 0.0002}, firm.LLM{InputUSDPerMTok: 1000, OutputUSDPerMTok: 1000}, 1}, // $0.15 per call exceeds $0.0002 after one call.
	} {
		t.Run(name, func(t *testing.T) {
			a := reply(goodJSON)
			e := ent(a)
			e.price = tc.price
			r := newRT(map[string]entry{"R": e})
			r.limits = tc.limits
			var err error
			for i := 0; i < 10 && err == nil; i++ {
				_, err = r.Execute(context.Background(), task("R"), nil)
			}
			if !errors.Is(err, ErrBudgetExceeded) || len(a.reqs) > tc.want {
				t.Fatalf("calls=%d err=%v", len(a.reqs), err)
			}
		})
	}
}

func TestRetriesCountAgainstCallLimit(t *testing.T) {
	a := flaky(100, &agents.StatusError{Status: 503})
	e := ent(a)
	e.retries = 5
	r := newRT(map[string]entry{"R": e})
	r.limits.MaxCallsPerRun = 3
	if _, err := r.Execute(context.Background(), task("R"), nil); !errors.Is(err, ErrBudgetExceeded) || len(a.reqs) != 3 {
		t.Fatalf("calls=%d err=%v", len(a.reqs), err)
	}
}

func TestMissingUsageFailsClosed(t *testing.T) {
	a := &fakeAgent{run: func(context.Context, agents.Request) (agents.Response, error) {
		return agents.Response{Text: goodJSON}, nil
	}}
	if _, err := newRT(map[string]entry{"R": ent(a)}).Execute(context.Background(), task("R"), nil); !errors.Is(err, ErrNoUsage) {
		t.Fatalf("err = %v", err)
	}
}

func TestLimitsArePerRunAndLedgerIsReleased(t *testing.T) {
	r := newRT(map[string]entry{"R": ent(reply(goodJSON))})
	r.limits.MaxCallsPerRun = 1
	t1, t2 := task("R"), task("R")
	t2.RunID = "run2"
	if _, err := r.Execute(context.Background(), t1, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(context.Background(), t1, nil); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("second call in run1: %v", err)
	}
	if _, err := r.Execute(context.Background(), t2, nil); err != nil {
		t.Fatalf("run2 must have its own budget: %v", err)
	}
	r.FinishRun("run1")
	r.FinishRun("run2")
	if len(r.ledgers) != 0 {
		t.Fatalf("ledgers leaked: %v", r.ledgers)
	}
}

func TestEngineReleasesLedgerWhenRunEnds(t *testing.T) {
	r := newRT(map[string]entry{"R": ent(reply("not json"))})
	c := firm.Config{
		Agents:    []firm.Agent{{ID: "a", Role: "R"}},
		Workflows: []firm.Workflow{{ID: "w", Agents: []string{"a"}}},
	}
	e, _ := firm.New(c, r, nil, "research")
	if _, err := e.Run(context.Background(), "w", "x"); err == nil {
		t.Fatal("expected failure")
	}
	if len(r.ledgers) != 0 {
		t.Fatalf("ledgers leaked after failed run: %v", r.ledgers)
	}
}

func TestNewAppliesLimitsRetriesAndRequiresPricesForCostLimit(t *testing.T) {
	retries := 4
	c := cfg(&firm.LLM{Provider: "claude", Model: "m", MaxRetries: &retries}, nil)
	r, err := New(c, env(map[string]string{"ANTHROPIC_API_KEY": "k"}))
	if err != nil {
		t.Fatal(err)
	}
	if r.limits.MaxCallsPerRun != defaultMaxCalls || r.limits.MaxTokensPerRun != defaultMaxTokens || r.roles["Market Analyst"].retries != 4 {
		t.Fatalf("limits=%+v retries=%d", r.limits, r.roles["Market Analyst"].retries)
	}
	c = cfg(&firm.LLM{Provider: "claude", Model: "m"}, nil)
	if r, _ := New(c, env(map[string]string{"ANTHROPIC_API_KEY": "k"})); r.roles["Market Analyst"].retries != defaultRetries {
		t.Fatalf("default retries = %d", r.roles["Market Analyst"].retries)
	}
	c.Limits.MaxCostUSDPerRun = 1
	for name, l := range map[string]firm.LLM{
		"no prices":   {Provider: "claude", Model: "m"},
		"input only":  {Provider: "claude", Model: "m", InputUSDPerMTok: 3},
		"output only": {Provider: "claude", Model: "m", OutputUSDPerMTok: 15},
	} {
		c.LLM = &l
		if _, err := New(c, env(map[string]string{"ANTHROPIC_API_KEY": "k"})); err == nil || !strings.Contains(err.Error(), "positive input_usd_per_mtok and output_usd_per_mtok") {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	// A role override with incomplete prices is rejected even if the default is complete.
	good := firm.LLM{Provider: "claude", Model: "m", InputUSDPerMTok: 3, OutputUSDPerMTok: 15}
	c = cfg(&good, map[string]firm.LLM{"Strategy Challenger": {Provider: "openai", Model: "m", InputUSDPerMTok: 1}})
	c.Limits.MaxCostUSDPerRun = 1
	if _, err := New(c, env(map[string]string{"ANTHROPIC_API_KEY": "k", "OPENAI_API_KEY": "k"})); err == nil || !strings.Contains(err.Error(), "Strategy Challenger") {
		t.Fatalf("role override: %v", err)
	}
	c.Roles["Strategy Challenger"] = firm.LLM{Provider: "openai", Model: "m", InputUSDPerMTok: 1, OutputUSDPerMTok: 2}
	if _, err := New(c, env(map[string]string{"ANTHROPIC_API_KEY": "k", "OPENAI_API_KEY": "k"})); err != nil {
		t.Fatalf("complete prices: %v", err)
	}
}

// TestDefaultConfigBoundsOutputOnTheWire builds agents through New with no
// max_tokens configured and checks the request each provider actually sends.
func TestDefaultConfigBoundsOutputOnTheWire(t *testing.T) {
	for provider, field := range map[string]string{"openai": "max_output_tokens", "claude": "max_tokens"} {
		t.Run(provider, func(t *testing.T) {
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&body)
				http.Error(w, "stop here", http.StatusBadRequest) // Only the request matters.
			}))
			defer server.Close()
			c := cfg(&firm.LLM{Provider: provider, Model: "m"}, nil)
			r, err := New(c, env(map[string]string{"ANTHROPIC_API_KEY": "k", "OPENAI_API_KEY": "k"}))
			if err != nil {
				t.Fatal(err)
			}
			switch a := r.roles["Market Analyst"].agent.(type) {
			case *openai.Agent:
				a.Endpoint, a.Client = server.URL, server.Client()
			case *claude.Agent:
				a.Endpoint, a.Client = server.URL, server.Client()
			}
			_, _ = r.Execute(context.Background(), task("Market Analyst"), nil)
			if body[field] != float64(defaultMaxOutput) {
				t.Fatalf("%s = %v, want %d (body %v)", field, body[field], defaultMaxOutput, body)
			}
		})
	}
	// An explicit max_tokens wins over the default.
	r, err := New(cfg(&firm.LLM{Provider: "openai", Model: "m", MaxTokens: 123}, nil), env(map[string]string{"OPENAI_API_KEY": "k"}))
	if err != nil || r.roles["Market Analyst"].agent.(*openai.Agent).MaxTokens != 123 {
		t.Fatalf("explicit max_tokens: %v", err)
	}
}
