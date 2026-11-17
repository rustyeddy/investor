package service

import (
	"context"
	"testing"

	"github.com/rustyeddy/investor/internal/firm"
)

func TestServicePlanRun(t *testing.T) {
	c := firm.Config{
		Agents:    []firm.Agent{{ID: "analyst", Role: "Market Analyst"}},
		Workflows: []firm.Workflow{{ID: "research", Agents: []string{"analyst"}}},
	}
	engine, err := firm.New(c, firm.DryRun{}, nil, "dry-run")
	if err != nil {
		t.Fatal(err)
	}
	s := New(c, engine)
	plan, err := s.Plan("research")
	if err != nil || len(plan) != 1 {
		t.Fatalf("plan: %v, %v", plan, err)
	}
	run, err := s.Run(context.Background(), "research", "evaluate")
	if err != nil || run.Status != "succeeded" || len(run.Results) != 1 {
		t.Fatalf("run: %+v, %v", run, err)
	}
}
