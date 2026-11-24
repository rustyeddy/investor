// Package wfa runs one walk-forward experiment: every configuration of a small
// grid is backtested on the training window, one is chosen from training
// evidence alone, and exactly that one is backtested out of sample. Trader runs
// the backtests; Investor defines the experiment, chooses, and records. See
// docs/wfa.md.
package wfa

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"
)

// Window is a UTC range: From inclusive, To exclusive.
type Window struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// Params is one EMAX configuration. Stop is a decimal fraction of the price,
// kept as the string passed on the command line.
type Params struct {
	Short int    `json:"short"`
	Long  int    `json:"long"`
	Stop  string `json:"stop"`
}

// Grid lists the values to combine. Configurations are the cross product, in
// the order written, skipping those whose short window is not below the long.
type Grid struct {
	Short []int    `json:"short"`
	Long  []int    `json:"long"`
	Stop  []string `json:"stop"`
}

// Selection is the training-only rule that picks the configuration. See Score.
type Selection struct {
	Metric    string `json:"metric"`     // Only "net_return".
	MinTrades int    `json:"min_trades"` // Fewer closed trades are ineligible.
}

// Strategy says how to run the standalone strategy process.
type Strategy struct {
	Executable string `json:"executable"`
	Trader     string `json:"trader"` // Trader's strategy address, loopback.
	Tick       string `json:"tick,omitempty"`
}

// Spec is the whole experiment, fixed before any out-of-sample result exists.
type Spec struct {
	ID         string    `json:"id"`
	Instrument string    `json:"instrument"`
	Interval   string    `json:"interval"`
	Train      Window    `json:"train"`
	Test       Window    `json:"test"`
	Holdout    time.Time `json:"holdout_start"` // No window may reach this instant.
	Strategy   Strategy  `json:"strategy"`
	Grid       Grid      `json:"grid"`
	Selection  Selection `json:"selection"`
	RunTimeout Duration  `json:"run_timeout"`
	// ExpectedInitialBalance is the starting capital the experiment is meant to
	// run with, as a decimal. Trader reports what it used; a difference stops the
	// experiment before the out-of-sample run.
	ExpectedInitialBalance string `json:"expected_initial_balance"`
	// TraderRepo and StrategiesRepo are the checkouts the binaries were built
	// from; their git commits go in the report.
	TraderRepo     string `json:"trader_repo,omitempty"`
	StrategiesRepo string `json:"strategies_repo,omitempty"`
}

// Duration is a time.Duration written as a string such as "10m".
type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration: want a string such as \"10m\"")
	}
	v, err := time.ParseDuration(s)
	*d = Duration(v)
	return err
}

// LoadSpec reads and validates a spec file. Unknown fields are errors.
func LoadSpec(path string) (Spec, error) {
	f, err := os.Open(path)
	if err != nil {
		return Spec{}, err
	}
	defer f.Close()
	var s Spec
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Spec{}, fmt.Errorf("spec %s: %w", path, err)
	}
	if err := s.Validate(); err != nil {
		return Spec{}, fmt.Errorf("spec %s: %w", path, err)
	}
	return s, nil
}

// Validate checks everything that can be known before running. It makes the
// guarantees the experiment depends on: the windows are ordered, do not
// overlap, and stay clear of the final holdout.
func (s Spec) Validate() error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	if strings.TrimSpace(s.ID) == "" {
		bad("id is required")
	}
	if strings.TrimSpace(s.Instrument) == "" {
		bad("instrument is required")
	}
	if s.Interval != "h1" && s.Interval != "d1" {
		bad("interval %q (want h1 or d1)", s.Interval)
	}
	for name, w := range map[string]Window{"train": s.Train, "test": s.Test} {
		if w.From.IsZero() || w.To.IsZero() || !w.From.Before(w.To) {
			bad("%s: from must be before to", name)
		}
	}
	if s.Test.From.Before(s.Train.To) {
		bad("test starts %s, before training ends %s", s.Test.From.Format(time.DateOnly), s.Train.To.Format(time.DateOnly))
	}
	if s.Holdout.IsZero() {
		bad("holdout_start is required")
	} else if s.Test.To.After(s.Holdout) || s.Train.To.After(s.Holdout) {
		bad("a window reaches the holdout starting %s", s.Holdout.Format(time.DateOnly))
	}
	if strings.TrimSpace(s.Strategy.Executable) == "" || strings.TrimSpace(s.Strategy.Trader) == "" {
		bad("strategy.executable and strategy.trader are required")
	}
	if len(s.Configs()) == 0 {
		bad("the grid has no valid configuration")
	}
	if s.Selection.Metric != MetricNetReturn {
		bad("selection.metric %q (want %q)", s.Selection.Metric, MetricNetReturn)
	}
	if s.Selection.MinTrades < 1 {
		bad("selection.min_trades must be at least 1")
	}
	if r, err := decimal(s.ExpectedInitialBalance); err != nil || r.Sign() <= 0 {
		bad("expected_initial_balance %q must be a positive decimal", s.ExpectedInitialBalance)
	}
	if s.Strategy.Tick != "" {
		if r, ok := new(big.Rat).SetString(s.Strategy.Tick); !ok || r.Sign() <= 0 {
			bad("strategy.tick %q must be a positive decimal", s.Strategy.Tick)
		}
	}
	for _, c := range s.Configs() {
		// EMAX accepts a stop that is a fraction of the price: 0 < stop < 1. This is
		// the price distance of the stop, not Trader's per-trade risk.
		if r, ok := new(big.Rat).SetString(c.Stop); !ok || r.Sign() <= 0 || r.Cmp(big.NewRat(1, 1)) >= 0 {
			bad("grid stop %q must be a decimal fraction of the price, above 0 and below 1", c.Stop)
		}
	}
	if s.RunTimeout <= 0 {
		bad("run_timeout is required")
	}
	return errors.Join(errs...)
}

// Configs expands the grid.
func (s Spec) Configs() []Params {
	var out []Params
	for _, sh := range s.Grid.Short {
		for _, lg := range s.Grid.Long {
			if sh < 1 || sh >= lg {
				continue
			}
			for _, st := range s.Grid.Stop {
				out = append(out, Params{Short: sh, Long: lg, Stop: st})
			}
		}
	}
	return out
}
