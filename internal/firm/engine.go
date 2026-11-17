package firm

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

var ErrUnknownWorkflow = errors.New("unknown workflow")

type Task struct {
	Agent     Agent    `json:"agent"`
	Skills    []Skill  `json:"skills"`
	Objective string   `json:"objective"`
	Previous  []Result `json:"previous"`
}

type Result struct {
	AgentID string `json:"agent_id"`
	Output  string `json:"output"`
}

type Run struct {
	ID         string    `json:"id"`
	Workflow   string    `json:"workflow"`
	Objective  string    `json:"objective"`
	Mode       string    `json:"mode"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Results    []Result  `json:"results"`
	Error      string    `json:"error,omitempty"`
}

type CallTool func(context.Context, string, json.RawMessage) (json.RawMessage, error)
type ToolFunc func(context.Context, json.RawMessage) (json.RawMessage, error)

// Runtime implementations must honor context cancellation and be safe for
// concurrent calls. All tool requests must go through callTool.
type Runtime interface {
	Execute(ctx context.Context, task Task, callTool CallTool) (string, error)
}

type Engine struct {
	config  Config
	runtime Runtime
	tools   map[string]ToolFunc
	mode    string
}

func New(c Config, runtime Runtime, tools map[string]ToolFunc, mode string) (*Engine, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if runtime == nil {
		return nil, errors.New("runtime is required")
	}
	if mode != "dry-run" && mode != "research" {
		return nil, fmt.Errorf("unsupported mode %q", mode)
	}
	// Snapshot configuration and implementations before serving concurrent runs.
	data, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	var snapshot Config
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	registered := make(map[string]ToolFunc, len(tools))
	for id, fn := range tools {
		if fn == nil {
			return nil, fmt.Errorf("nil tool implementation %q", id)
		}
		if !slices.ContainsFunc(c.Tools, func(t Tool) bool { return t.ID == id }) {
			return nil, fmt.Errorf("undeclared tool %q", id)
		}
		registered[id] = fn
	}
	return &Engine{snapshot, runtime, registered, mode}, nil
}

func (e *Engine) Plan(id string) ([]Agent, error) {
	for _, w := range e.config.Workflows {
		if w.ID != id {
			continue
		}
		out := make([]Agent, 0, len(w.Agents))
		for _, aid := range w.Agents {
			for _, a := range e.config.Agents {
				if a.ID == aid {
					a.Skills, a.Tools = slices.Clone(a.Skills), slices.Clone(a.Tools)
					out = append(out, a)
				}
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrUnknownWorkflow, id)
}

// Run executes an ordered workflow and stops at the first failure. Earlier
// outputs are passed to subsequent agents; they are evidence, not approvals.
func (e *Engine) Run(ctx context.Context, workflow, objective string) (run Run, err error) {
	plan, err := e.Plan(workflow)
	if err != nil {
		return run, err
	}
	run = Run{ID: rand.Text(), Workflow: workflow, Objective: objective, Mode: e.mode, Status: "running", StartedAt: time.Now().UTC(), Results: []Result{}}
	defer func() {
		run.FinishedAt = time.Now().UTC()
		run.Status = "succeeded"
		if err != nil {
			run.Status, run.Error = "failed", err.Error()
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				run.Status = "canceled"
			}
		}
	}()
	for _, agent := range plan {
		if err = ctx.Err(); err != nil {
			return run, err
		}
		task := Task{
			Agent:     agent,
			Skills:    e.skillsFor(agent),
			Objective: objective,
			Previous:  slices.Clone(run.Results),
		}
		var output string
		output, err = e.runtime.Execute(ctx, task, e.toolCaller(agent))
		if err == nil {
			err = ctx.Err()
		}
		if err != nil {
			err = fmt.Errorf("agent %s: %w", agent.ID, err)
			return run, err
		}
		run.Results = append(run.Results, Result{AgentID: agent.ID, Output: output})
	}
	return run, nil
}

// skillsFor returns the configured skills the agent references, in the
// agent's order.
func (e *Engine) skillsFor(agent Agent) []Skill {
	skills := []Skill{}
	for _, id := range agent.Skills {
		for _, s := range e.config.Skills {
			if s.ID == id {
				skills = append(skills, s)
			}
		}
	}
	return skills
}

// toolCaller returns the only path by which an agent's runtime may invoke
// tools: it enforces the mode, the agent's declared tools, and connectivity.
func (e *Engine) toolCaller(agent Agent) CallTool {
	allowed := slices.Clone(agent.Tools)
	return func(ctx context.Context, id string, input json.RawMessage) (json.RawMessage, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if e.mode == "dry-run" {
			return nil, errors.New("tools cannot execute in dry-run mode")
		}
		if !slices.Contains(allowed, id) {
			return nil, fmt.Errorf("agent %q is not allowed to call %q", agent.ID, id)
		}
		fn, ok := e.tools[id]
		if !ok {
			return nil, fmt.Errorf("tool %q is not connected", id)
		}
		return fn(ctx, input)
	}
}

// DryRun exercises orchestration without model calls or trading operations.
type DryRun struct{}

func (DryRun) Execute(ctx context.Context, task Task, _ CallTool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return fmt.Sprintf("Dry run: %s; %d skills, %d declared tools, %d preceding results. No analysis performed.", task.Agent.Role, len(task.Skills), len(task.Agent.Tools), len(task.Previous)), nil
}
