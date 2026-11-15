package firm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type runtimeFunc func(context.Context, Task, CallTool) (string, error)

func (f runtimeFunc) Execute(c context.Context, t Task, call CallTool) (string, error) {
	return f(c, t, call)
}

func fixture() Config {
	return Config{Agents: []Agent{{ID: "a", Role: "research", Skills: []string{"s"}, Tools: []string{"read"}}, {ID: "b", Role: "review"}}, Skills: []Skill{{ID: "s", Instructions: "cite evidence"}}, Tools: []Tool{{ID: "read"}, {ID: "write"}}, Workflows: []Workflow{{ID: "w", Agents: []string{"a", "b"}}}}
}

func TestHandoffAndFailure(t *testing.T) {
	want := errors.New("review failed")
	e, err := New(fixture(), runtimeFunc(func(ctx context.Context, task Task, call CallTool) (string, error) {
		if task.Objective != "evaluate" {
			t.Fatal("objective lost")
		}
		if task.Agent.ID == "a" {
			if len(task.Skills) != 1 || len(task.Previous) != 0 {
				t.Fatal(task)
			}
			return "evidence", nil
		}
		if len(task.Previous) != 1 || task.Previous[0].Output != "evidence" {
			t.Fatal(task)
		}
		return "", want
	}), nil, "research")
	if err != nil {
		t.Fatal(err)
	}
	run, err := e.Run(context.Background(), "w", "evaluate")
	if !errors.Is(err, want) || run.Status != "failed" || len(run.Results) != 1 || run.FinishedAt.IsZero() {
		t.Fatalf("%+v %v", run, err)
	}
}

func TestToolPermissions(t *testing.T) {
	for _, tc := range []struct {
		mode, tool string
		allowed    bool
	}{{"dry-run", "read", false}, {"research", "write", false}, {"research", "read", true}} {
		t.Run(tc.mode+tc.tool, func(t *testing.T) {
			calls := 0
			c := fixture()
			c.Workflows[0].Agents = []string{"a"}
			e, err := New(c, runtimeFunc(func(ctx context.Context, task Task, call CallTool) (string, error) {
				// A runtime mutating its task must not expand its permission set.
				task.Agent.Tools[0] = "write"
				_, err := call(ctx, tc.tool, json.RawMessage(`{}`))
				return "done", err
			}), map[string]ToolFunc{"read": func(context.Context, json.RawMessage) (json.RawMessage, error) {
				calls++
				return json.RawMessage(`{}`), nil
			}, "write": func(context.Context, json.RawMessage) (json.RawMessage, error) { calls++; return nil, nil }}, tc.mode)
			if err != nil {
				t.Fatal(err)
			}
			_, err = e.Run(context.Background(), "w", "")
			if (err == nil) != tc.allowed || (calls == 1) != tc.allowed {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestCancellationAndUnknownWorkflow(t *testing.T) {
	e, err := New(fixture(), runtimeFunc(func(context.Context, Task, CallTool) (string, error) {
		t.Fatal("canceled run invoked runtime")
		return "", nil
	}), nil, "research")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	run, err := e.Run(ctx, "w", "")
	if !errors.Is(err, context.Canceled) || run.Status != "canceled" {
		t.Fatalf("%+v %v", run, err)
	}
	if _, err := e.Run(context.Background(), "missing", ""); !errors.Is(err, ErrUnknownWorkflow) {
		t.Fatal(err)
	}
}

func TestConfigValidation(t *testing.T) {
	for _, mutate := range []func(*Config){
		func(c *Config) { c.Agents = append(c.Agents, c.Agents[0]) },
		func(c *Config) { c.Agents[0].Skills = []string{"missing"} },
		func(c *Config) { c.Agents[0].Tools = []string{"missing"} },
		func(c *Config) { c.Workflows[0].Agents = []string{"missing"} },
	} {
		c := fixture()
		mutate(&c)
		if c.Validate() == nil {
			t.Fatal("accepted invalid config")
		}
	}
	data, _ := json.Marshal(fixture())
	if _, err := Decode(strings.NewReader(string(data) + ` {}`)); err == nil {
		t.Fatal("accepted trailing JSON")
	}
	if _, err := Decode(strings.NewReader(`{"unexpected":true}`)); err == nil {
		t.Fatal("accepted unknown field")
	}
}
