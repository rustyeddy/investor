package portfolio

import (
	"path/filepath"
	"testing"

	"github.com/rustyeddy/investor/internal/wfa"
)

// CompleteRunCount is a catalog/domain decision, not a Cobra rendering rule.
func TestCompleteRunCount(t *testing.T) {
	f := newFixture(t)
	if got := f.only().CompleteRunCount(); got != 3 {
		t.Fatalf("complete runs = %d, want 3", got)
	}

	f.remove(filepath.Join(RunsDir, runID(2), AccountFile))
	f.sign()
	if got := f.only().CompleteRunCount(); got != 2 {
		t.Fatalf("complete runs with partial evidence = %d, want 2", got)
	}

	f.remove(filepath.Join(RunsDir, runID(1)))
	f.editReport(func(r *wfa.Report) {
		r.Training[0].Outcome = nil
		r.Training[0].Err = "no Trader run started"
	})
	if got := f.only().CompleteRunCount(); got != 1 {
		t.Fatalf("complete runs with unassigned attempt = %d, want 1", got)
	}
}
