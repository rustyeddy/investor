package wfa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rustyeddy/investor/internal/market"
)

// Write saves the report as <dir>/<id>.json. It refuses to replace an existing
// report: research records are kept, and a rerun needs a new experiment ID.
func Write(dir string, r Report) (string, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, r.ID+".json")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", fmt.Errorf("report %s: %w", path, err)
	}
	_, err = f.Write(append(data, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return path, err
}

// Summary is the concise human-readable account of a report.
func (r Report) Summary() string {
	var b strings.Builder
	day := func(t time.Time) string { return t.UTC().Format(time.DateOnly) }
	fmt.Fprintf(&b, "WFA %s: %s\n", r.ID, r.Status)
	fmt.Fprintf(&b, "%s %s  train [%s, %s)  test [%s, %s)\n", r.Spec.Instrument, r.Spec.Interval,
		day(r.Spec.Train.From), day(r.Spec.Train.To), day(r.Spec.Test.From), day(r.Spec.Test.To))
	for _, c := range r.Data {
		fmt.Fprintf(&b, "data %-5s %d bars complete=%t fingerprint=%.12s %s\n", c.Phase, c.Bars, c.Complete, c.Fingerprint, c.Error)
	}
	for _, s := range r.Scores {
		verdict := "score " + s.Score
		if !s.Eligible {
			verdict = "ineligible: " + s.Reason
		}
		fmt.Fprintf(&b, "train short=%d long=%d stop=%s run=%s trades=%d  %s\n", s.Params.Short, s.Params.Long, s.Params.Stop, orDash(s.RunID), s.Trades, verdict)
	}
	if r.Selected != nil {
		fmt.Fprintf(&b, "selected short=%d long=%d stop=%s: %s\n", r.Selected.Short, r.Selected.Long, r.Selected.Stop, r.Rationale)
	} else {
		fmt.Fprintf(&b, "selected none: %s\n", r.Rationale)
	}
	if r.OOS != nil {
		id, line := "-", r.OOS.Err
		if o := r.OOS.Outcome; o != nil {
			id = o.RunID
			if o.Summary != nil {
				s := Score(*r.OOS, Selection{MinTrades: 0})
				line = fmt.Sprintf("trades=%d net_return=%s", o.Summary.Trades, orDash(s.Score))
			}
		}
		fmt.Fprintf(&b, "oos run=%s %s\n", id, line)
	}
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// RunExperiment is the whole command: build the process runner from the spec, execute the
// experiment, and write the report whatever the outcome, so a failed experiment
// leaves its evidence. src may be nil to skip the data preflight.
func RunExperiment(ctx context.Context, spec Spec, src market.Source, outDir string) (Report, string, error) {
	e := Experiment{
		Spec: spec, Source: src,
		Runner: &Process{Strategy: spec.Strategy, Instr: spec.Instrument, Interval: spec.Interval, Timeout: time.Duration(spec.RunTimeout)},
	}
	rep, runErr := e.Execute(ctx)
	if rep.Status == "" { // Rejected before it started: nothing worth recording.
		return rep, "", runErr
	}
	path, werr := Write(outDir, rep)
	return rep, path, errors.Join(runErr, werr)
}

// Source builds the market-data client for the preflight from the spec's
// trader_api, or from baseURL when it is not empty (the command-line override).
// getenv looks up the token variable.
func (s Spec) Source(baseURL string, getenv func(string) string) (market.Source, error) {
	if baseURL == "" {
		baseURL = s.TraderAPI.BaseURL
	}
	if baseURL == "" {
		return nil, errors.New("no Trader API: set trader_api.base_url in the spec or pass --trader (or --skip-preflight)")
	}
	env := s.TraderAPI.TokenEnv
	if env == "" {
		env = "TRADER_API_TOKEN"
	}
	return &market.Client{BaseURL: baseURL, Token: getenv(env)}, nil
}
