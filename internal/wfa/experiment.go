package wfa

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rustyeddy/investor/internal/market"
)

// Report statuses.
const (
	StatusComplete    = "complete"              // Train grid, choice and one OOS run all finished.
	StatusNoEligible  = "no_eligible_selection" // Nothing qualified, so no OOS run was made.
	StatusPreflight   = "preflight_failed"      // The data was not good enough; nothing was run.
	StatusTestFailed  = "oos_failed"            // A configuration was chosen but its OOS run failed.
	StatusInterrupted = "interrupted"
)

// Unknown marks a provenance field the experiment could not obtain.
const Unknown = "unknown"

// DataCheck is what the preflight learned about one window's bars.
type DataCheck struct {
	Phase       Phase     `json:"phase"`
	Window      Window    `json:"window"`
	Source      string    `json:"source,omitempty"`
	Bars        int       `json:"bars"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	Complete    bool      `json:"complete"`
	Gaps        int       `json:"gaps"`
	RetrievedAt time.Time `json:"retrieved_at,omitzero"`
	Error       string    `json:"error,omitempty"`
}

// Report is the durable record of one experiment.
type Report struct {
	ID          string            `json:"id"`
	Status      string            `json:"status"`
	CreatedAt   time.Time         `json:"created_at"`
	Spec        Spec              `json:"spec"`
	Data        []DataCheck       `json:"data"`
	Training    []Run             `json:"training"`
	Scores      []Scored          `json:"scores"`
	Selected    *Params           `json:"selected,omitempty"`
	Rationale   string            `json:"rationale"`
	OOS         *Run              `json:"oos,omitempty"`
	Provenance  map[string]string `json:"provenance"`
	Limitations []string          `json:"limitations"`
}

// Experiment is everything Execute needs. Source may be nil, which skips the
// data preflight and says so in the report.
type Experiment struct {
	Spec   Spec
	Runner Runner
	Source market.Source
	Now    func() time.Time // Defaults to time.Now.
}

// Execute runs the experiment: preflight, the training grid, the choice, and
// one out-of-sample run of the chosen configuration. It always returns the
// report. The error is non-nil when the experiment did not reach a result it
// can stand behind (preflight or OOS failure, interruption); having no eligible
// configuration is a result, not an error.
func (e Experiment) Execute(ctx context.Context) (Report, error) {
	now := e.Now
	if now == nil {
		now = time.Now
	}
	sp := e.Spec
	rep := Report{
		ID: sp.ID, CreatedAt: now().UTC(), Spec: sp,
		Provenance: map[string]string{
			"trader_build": Unknown, "strategy_build": Unknown,
			"trader_risk_policy": Unknown, "trader_initial_balance": Unknown,
		},
		Limitations: []string{
			"The chosen configuration is the training-score maximum over a small fixed grid; the OOS result is one sample.",
			"Trader build, strategy build and risk configuration are not yet captured automatically.",
		},
	}
	if err := sp.Validate(); err != nil {
		return rep, fmt.Errorf("spec: %w", err)
	}

	if e.Source == nil {
		rep.Limitations = append(rep.Limitations, "Data preflight was not performed (no market source).")
	} else {
		ok := true
		for _, w := range []struct {
			p Phase
			w Window
		}{{PhaseTrain, sp.Train}, {PhaseTest, sp.Test}} {
			c := e.preflight(ctx, w.p, w.w)
			rep.Data = append(rep.Data, c)
			ok = ok && c.Error == ""
		}
		rep.Limitations = append(rep.Limitations, "Preflight checks bars inside each window; it does not check the strategy's warm-up bars before it.")
		if !ok {
			rep.Status, rep.Rationale = StatusPreflight, "Trader's data does not cover the requested windows; no backtest was run."
			return rep, errors.New("data preflight failed")
		}
	}

	configs := sp.Configs()
	for _, p := range configs {
		if err := ctx.Err(); err != nil {
			rep.Status, rep.Rationale = StatusInterrupted, "interrupted during the training grid"
			return rep, err
		}
		run := e.Runner.Run(ctx, RunSpec{Phase: PhaseTrain, Params: p, Window: sp.Train})
		rep.Training = append(rep.Training, run)
		rep.Scores = append(rep.Scores, Score(run, sp.Selection))
	}

	best := Choose(rep.Scores)
	if best < 0 {
		rep.Status = StatusNoEligible
		rep.Rationale = fmt.Sprintf("none of %d configurations was eligible (minimum %d trades, completed run required); the OOS window was not used.", len(configs), sp.Selection.MinTrades)
		return rep, nil
	}
	chosen := rep.Scores[best]
	rep.Selected = &chosen.Params
	rep.Rationale = fmt.Sprintf("highest training %s %s among %d eligible of %d (ties: smaller long, short, stop); chosen from training only.",
		sp.Selection.Metric, chosen.Score, countEligible(rep.Scores), len(configs))

	if err := ctx.Err(); err != nil {
		rep.Status = StatusInterrupted
		return rep, err
	}
	oos := e.Runner.Run(ctx, RunSpec{Phase: PhaseTest, Params: chosen.Params, Window: sp.Test})
	rep.OOS = &oos
	if !oos.OK() {
		rep.Status = StatusTestFailed
		return rep, fmt.Errorf("out-of-sample run failed: %s", oosReason(oos))
	}
	rep.Status = StatusComplete
	return rep, nil
}

func (e Experiment) preflight(ctx context.Context, p Phase, w Window) DataCheck {
	c := DataCheck{Phase: p, Window: w}
	got, err := e.Source.Bars(ctx, market.Request{
		Instrument: e.Spec.Instrument, Interval: e.Spec.Interval, From: w.From, To: w.To, AsOf: w.To, Strict: true,
	})
	if err != nil {
		c.Error = err.Error()
		return c
	}
	c.Source, c.Bars, c.Fingerprint = got.Source, len(got.Bars), got.Fingerprint
	c.Complete, c.Gaps, c.RetrievedAt = got.Coverage.Complete, len(got.Coverage.Gaps), got.RetrievedAt
	if !c.Complete {
		c.Error = "coverage is incomplete"
	}
	return c
}

func countEligible(s []Scored) (n int) {
	for _, c := range s {
		if c.Eligible {
			n++
		}
	}
	return n
}

func oosReason(r Run) string {
	switch {
	case r.Err != "":
		return r.Err
	case r.Outcome != nil && r.Outcome.Error != "":
		return r.Outcome.Error
	case r.Outcome != nil:
		return "status " + r.Outcome.Status
	}
	return "no result"
}
