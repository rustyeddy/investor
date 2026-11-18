package journal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/rustyeddy/investor/internal/firm"
)

// script is a Runtime whose agents succeed, stop, or fail by agent ID.
type script struct {
	calls   []string
	outcome map[string]error // Missing means success.
}

func (s *script) Execute(_ context.Context, task firm.Task, _ firm.CallTool) (string, error) {
	s.calls = append(s.calls, task.Agent.ID)
	return "out-" + task.Agent.ID, s.outcome[task.Agent.ID]
}

func config() firm.Config {
	return firm.Config{
		Skills:    []firm.Skill{{ID: "sk", Instructions: "SKILL BODY"}},
		Agents:    []firm.Agent{{ID: "a1", Role: "R1", Skills: []string{"sk"}}, {ID: "a2", Role: "R2"}},
		Workflows: []firm.Workflow{{ID: "w", Agents: []string{"a1", "a2"}}},
	}
}

func engine(t *testing.T, rt firm.Runtime, j Journal) *firm.Engine {
	t.Helper()
	e, err := firm.New(config(), rt, nil, "research")
	if err != nil {
		t.Fatal(err)
	}
	return e.WithObserver(NewObserver(j))
}

func payload[T any](t *testing.T, r Record) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(r.Payload, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRunAndStepsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	e := engine(t, &script{}, open(t, dir))
	run, err := e.Run(context.Background(), "w", "objective")
	if err != nil || run.Status != "succeeded" {
		t.Fatalf("run=%+v err=%v", run, err)
	}

	j := open(t, dir) // Restart.
	first, err := j.Get("run", run.ID, 1)
	if err != nil || payload[firm.Run](t, first).Status != "running" {
		t.Fatalf("revision 1 must keep the start state: %+v, %v", first, err)
	}
	last, err := j.Latest("run", run.ID)
	if err != nil || last.Revision != 2 || last.SupersedesRevisionID != first.RevisionID {
		t.Fatalf("latest = %+v, %v", last, err)
	}
	got := payload[firm.Run](t, last)
	if got.Status != "succeeded" || got.Objective != "objective" || !got.StartedAt.Equal(run.StartedAt) || !got.FinishedAt.Equal(run.FinishedAt) || len(got.Results) != 2 {
		t.Fatalf("run payload = %+v", got)
	}
	for _, agent := range []string{"a1", "a2"} {
		r, err := j.Latest("step", run.ID+"/"+agent)
		if err != nil {
			t.Fatalf("step %s: %v", agent, err)
		}
		s := payload[struct {
			RunID string `json:"run_id"`
			firm.Step
		}](t, r)
		if s.RunID != run.ID || s.Status != "succeeded" || s.Output != "out-"+agent || s.StartedAt.IsZero() {
			t.Fatalf("step %s = %+v", agent, s)
		}
		if agent == "a1" && (len(s.Skills) != 1 || s.Skills[0].ID != "sk" || len(s.Skills[0].SHA256) != 64) {
			t.Fatalf("instruction digest missing: %+v", s.Skills)
		}
	}
}

func TestFailedStopAndInterruptedRunsStayVisible(t *testing.T) {
	// A failing agent: its error is recorded on the step and the run.
	j := open(t, t.TempDir())
	rt := &script{outcome: map[string]error{"a1": errors.New("provider exploded")}}
	run, err := engine(t, rt, j).Run(context.Background(), "w", "x")
	if err == nil || run.Status != "failed" {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	s, err := j.Latest("step", run.ID+"/a1")
	if sp := payload[firm.Step](t, s); err != nil || sp.Status != "failed" || !strings.Contains(sp.Error, "provider exploded") {
		t.Fatalf("failed step = %+v, %v", sp, err)
	}
	if _, err := j.Latest("step", run.ID+"/a2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a2 never ran, so must have no step: %v", err)
	}
	if r, _ := j.Latest("run", run.ID); payload[firm.Run](t, r).Status != "failed" {
		t.Fatalf("run = %s", r.Payload)
	}

	// A deliberate stop keeps the evidence and the stop reason.
	j = open(t, t.TempDir())
	rt = &script{outcome: map[string]error{"a1": fmt.Errorf("%w: rejected", firm.ErrStopped)}}
	run, err = engine(t, rt, j).Run(context.Background(), "w", "x")
	if err != nil || run.Status != "stopped" {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	s, _ = j.Latest("step", run.ID+"/a1")
	if sp := payload[firm.Step](t, s); sp.Status != "stopped" || sp.Output != "out-a1" {
		t.Fatalf("stopped step = %+v", sp)
	}

	// A process that dies mid-run leaves revision 1: still "running", still findable.
	j = open(t, t.TempDir())
	o := NewObserver(j)
	if err := o.RunStarted(firm.Run{ID: "crashed", Status: "running", StartedAt: t0}); err != nil {
		t.Fatal(err)
	}
	r, err := j.Latest("run", "crashed")
	if err != nil || r.Revision != 1 || payload[firm.Run](t, r).Status != "running" {
		t.Fatalf("interrupted run = %+v, %v", r, err)
	}
}

func TestCanceledRunIsRecordedAsCanceled(t *testing.T) {
	j := open(t, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	run, err := engine(t, &script{}, j).Run(ctx, "w", "x")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if r, _ := j.Latest("run", run.ID); payload[firm.Run](t, r).Status != "canceled" {
		t.Fatalf("run = %s", r.Payload)
	}
}

// failing is a Journal whose Append fails after n successful appends.
type failing struct {
	Journal
	n int
}

func (f *failing) Append(r Record) error {
	if f.n--; f.n < 0 {
		return errors.New("disk full")
	}
	return f.Journal.Append(r)
}

func TestJournalFailureBlocksFurtherSteps(t *testing.T) {
	for name, n := range map[string]int{"start": 0, "first step": 1} {
		t.Run(name, func(t *testing.T) {
			rt := &script{}
			run, err := engine(t, rt, &failing{Journal: open(t, t.TempDir()), n: n}).Run(context.Background(), "w", "x")
			if err == nil || !strings.Contains(err.Error(), "journal: disk full") || run.Status != "failed" {
				t.Fatalf("run=%+v err=%v", run, err)
			}
			if name == "start" && len(rt.calls) != 0 {
				t.Fatalf("no agent may run when the run cannot be journaled: %v", rt.calls)
			}
			if name == "first step" && len(rt.calls) != 1 {
				t.Fatalf("second agent ran after a journal failure: %v", rt.calls)
			}
		})
	}
}

func TestObserverRedeliveryDoesNotDuplicate(t *testing.T) {
	j := open(t, t.TempDir())
	o := NewObserver(j)
	run := firm.Run{ID: "r1", Status: "running", StartedAt: t0}
	for i := 0; i < 3; i++ {
		if err := o.RunStarted(run); err != nil {
			t.Fatalf("delivery %d: %v", i, err)
		}
	}
	if ids, _ := j.List("run"); len(ids) != 1 {
		t.Fatalf("ids = %v", ids)
	}
}
