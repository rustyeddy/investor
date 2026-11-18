package firm

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

var ErrUnknownWorkflow = errors.New("unknown workflow")

// ErrStopped is returned (wrapped) by a Runtime together with the agent's
// output when the agent legitimately halts the workflow, for example a role
// that reports its input blocked or rejected. The engine keeps that output as
// evidence, runs no further agents, and ends the run with status "stopped".
// A stopped run is not an approval and not a failure.
var ErrStopped = errors.New("workflow stopped")

type Task struct {
	RunID     string   `json:"run_id"`
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
	StopReason string    `json:"stop_reason,omitempty"`
}

type CallTool func(context.Context, string, json.RawMessage) (json.RawMessage, error)
type ToolFunc func(context.Context, json.RawMessage) (json.RawMessage, error)

// Runtime implementations must honor context cancellation and be safe for
// concurrent calls. All tool requests must go through callTool.
type Runtime interface {
	Execute(ctx context.Context, task Task, callTool CallTool) (string, error)
}

// RunFinisher is optionally implemented by a Runtime that keeps per-run state;
// the engine calls FinishRun when a run ends, however it ends.
type RunFinisher interface {
	FinishRun(runID string)
}

// SkillRef identifies the exact skill instructions an agent step used.
type SkillRef struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"` // Digest of the instructions supplied to the model.
}

// Step is the outcome of one agent step: success, a stop, or a failure.
type Step struct {
	AgentID    string     `json:"agent_id"`
	Role       string     `json:"role"`
	Skills     []SkillRef `json:"skills"`
	Status     string     `json:"status"` // succeeded, stopped, failed, or canceled.
	Output     string     `json:"output,omitempty"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt time.Time  `json:"finished_at"`
}

// Observer receives durable notifications as a run progresses so that evidence,
// including failures and interrupted runs, is preserved. It is called
// synchronously and fails closed: if any method returns an error, the engine
// runs no further agents and fails the run.
type Observer interface {
	RunStarted(run Run) error
	StepFinished(runID string, step Step) error
	RunFinished(run Run) error
}

type Engine struct {
	config   Config
	runtime  Runtime
	tools    map[string]ToolFunc
	mode     string
	observer Observer
}

// WithObserver sets the run observer and returns the engine. Call it before
// the engine is used; it is not safe to change while runs are in progress.
func (e *Engine) WithObserver(o Observer) *Engine {
	e.observer = o
	return e
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
	return &Engine{config: snapshot, runtime: runtime, tools: registered, mode: mode}, nil
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
	if f, ok := e.runtime.(RunFinisher); ok {
		defer f.FinishRun(run.ID)
	}
	if e.observer != nil {
		if oerr := e.observer.RunStarted(run); oerr != nil {
			err = fmt.Errorf("journal: %w", oerr)
			run.FinishedAt, run.Status, run.Error = time.Now().UTC(), "failed", err.Error()
			return run, err
		}
	}
	defer func() {
		run.FinishedAt = time.Now().UTC()
		run.Status = "succeeded"
		if run.StopReason != "" {
			run.Status = "stopped"
		}
		if err != nil {
			run.Status, run.Error = "failed", err.Error()
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				run.Status = "canceled"
			}
		}
		if e.observer != nil {
			if oerr := e.observer.RunFinished(run); oerr != nil {
				err = errors.Join(err, fmt.Errorf("journal: %w", oerr))
				run.Status, run.Error = "failed", err.Error()
			}
		}
	}()
	for _, agent := range plan {
		if err = ctx.Err(); err != nil {
			return run, err
		}
		task := Task{
			RunID:     run.ID,
			Agent:     agent,
			Skills:    e.skillsFor(agent),
			Objective: objective,
			Previous:  slices.Clone(run.Results),
		}
		started := time.Now().UTC()
		var output string
		output, err = e.runtime.Execute(ctx, task, e.toolCaller(agent))
		if err == nil {
			err = ctx.Err()
		}
		stopped := err != nil && errors.Is(err, ErrStopped) && ctx.Err() == nil
		if stopped {
			run.StopReason = fmt.Sprintf("agent %s: %v", agent.ID, err)
		} else if err != nil {
			err = fmt.Errorf("agent %s: %w", agent.ID, err)
		}
		if err == nil || stopped {
			run.Results = append(run.Results, Result{AgentID: agent.ID, Output: output})
		}
		if e.observer != nil {
			step := newStep(task, started, output, err, stopped)
			if oerr := e.observer.StepFinished(run.ID, step); oerr != nil {
				// The step's evidence is not durable: fail closed.
				run.StopReason = ""
				err = errors.Join(err, fmt.Errorf("journal: %w", oerr))
				return run, err
			}
		}
		if stopped {
			return run, nil
		}
		if err != nil {
			return run, err
		}
	}
	return run, nil
}

// newStep describes one finished agent step for an Observer. err is nil for a
// success; stopped marks a deliberate stop (err then carries the reason).
func newStep(task Task, started time.Time, output string, err error, stopped bool) Step {
	s := Step{AgentID: task.Agent.ID, Role: task.Agent.Role, Skills: []SkillRef{}, Status: "succeeded", Output: output, StartedAt: started, FinishedAt: time.Now().UTC()}
	for _, sk := range task.Skills {
		sum := sha256.Sum256([]byte(sk.Instructions))
		s.Skills = append(s.Skills, SkillRef{ID: sk.ID, SHA256: hex.EncodeToString(sum[:])})
	}
	switch {
	case stopped:
		s.Status, s.Error = "stopped", err.Error()
	case err != nil:
		s.Status, s.Error = "failed", err.Error()
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			s.Status = "canceled"
		}
	}
	return s
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
