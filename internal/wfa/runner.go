package wfa

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// Phase labels a run so training and out-of-sample results cannot be confused.
type Phase string

const (
	PhaseTrain Phase = "train"
	PhaseTest  Phase = "oos"
)

// RunSpec is one backtest to launch.
type RunSpec struct {
	Phase  Phase
	Params Params
	Window Window
}

// Run is the record of one launched backtest. A failure is data: Err and
// ExitCode say what went wrong, and Outcome is nil unless the output parsed.
type Run struct {
	Phase    Phase     `json:"phase"`
	Params   Params    `json:"params"`
	Window   Window    `json:"window"`
	ExitCode int       `json:"exit_code"`
	Err      string    `json:"error,omitempty"`
	Outcome  *Outcome  `json:"outcome,omitempty"`
	Stdout   string    `json:"stdout,omitempty"`
	Stderr   string    `json:"stderr,omitempty"`
	Started  time.Time `json:"started"`
}

// OK reports whether the run completed and produced a summary.
func (r Run) OK() bool {
	return r.Err == "" && r.ExitCode == 0 && r.Outcome != nil && r.Outcome.Status == StatusCompleted &&
		r.Outcome.Error == "" && r.Outcome.Summary != nil
}

// Runner launches one backtest and waits for it.
type Runner interface {
	Run(ctx context.Context, spec RunSpec) Run
}

// ErrBusy means a run session is already active on this runner.
var ErrBusy = errors.New("wfa: another run is in progress")

// Process runs the standalone strategy executable, one process at a time.
type Process struct {
	Strategy Strategy
	Instr    string
	Interval string
	Timeout  time.Duration
	Now      func() time.Time // Defaults to time.Now.

	mu sync.Mutex
}

// Args is the exact command line for a run.
func (p *Process) Args(s RunSpec) []string {
	a := []string{
		"-trader", p.Strategy.Trader, "-instrument", p.Instr, "-interval", p.Interval,
		"-from", s.Window.From.UTC().Format(time.RFC3339), "-to", s.Window.To.UTC().Format(time.RFC3339),
		"-short", fmt.Sprint(s.Params.Short), "-long", fmt.Sprint(s.Params.Long), "-stop", s.Params.Stop,
	}
	if p.Strategy.Tick != "" {
		a = append(a, "-tick", p.Strategy.Tick)
	}
	return a
}

// Run implements Runner. Overlapping calls fail with ErrBusy instead of queueing.
func (p *Process) Run(ctx context.Context, s RunSpec) Run {
	r := Run{Phase: s.Phase, Params: s.Params, Window: s.Window}
	now := p.Now
	if now == nil {
		now = time.Now
	}
	r.Started = now().UTC()
	if !p.mu.TryLock() {
		r.Err = ErrBusy.Error()
		return r
	}
	defer p.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, p.Strategy.Executable, p.Args(s)...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	r.Stdout, r.Stderr = out.String(), errb.String()
	if err != nil {
		r.ExitCode = -1
		var ee *exec.ExitError
		switch {
		case ctx.Err() != nil:
			r.Err = fmt.Sprintf("run did not finish: %v", ctx.Err())
		case errors.As(err, &ee):
			r.ExitCode = ee.ExitCode()
			r.Err = fmt.Sprintf("strategy exited with status %d", r.ExitCode)
		default:
			r.Err = fmt.Sprintf("launching strategy: %v", err)
		}
	}
	// A failed run that still printed a result keeps it: its RunID matters.
	if o, perr := ParseOutput(r.Stdout); perr == nil {
		r.Outcome = &o
	} else if r.Err == "" {
		r.Err = perr.Error()
	}
	return r
}
