package wfa

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Run statuses reported by Trader.
const StatusCompleted = "completed"

// Summary is the part of Trader's run summary that selection reads. Money
// values are kept as the decimal strings Trader sent.
type Summary struct {
	InitialBalance string `json:"initial_balance"`
	Bars           int    `json:"bars"`
	Trades         int    `json:"trades"`
	Wins           int    `json:"wins"`
	Losses         int    `json:"losses"`
	Realized       string `json:"realized"`
	Balance        string `json:"balance"`
	Equity         string `json:"equity"`
}

// Outcome is what one strategy process reported.
type Outcome struct {
	RunID   string          `json:"run_id"`
	Status  string          `json:"status"`
	Error   string          `json:"error,omitempty"`
	Summary *Summary        `json:"summary,omitempty"`
	Raw     json.RawMessage `json:"raw_summary,omitempty"` // Trader's summary, unmodified.
}

// ParseOutput reads the standalone strategy's stdout: a line "run <id>:
// <status>", then either an "error: ..." line or the summary JSON. Anything
// else is a protocol error, never a result.
func ParseOutput(stdout string) (Outcome, error) {
	lines := strings.SplitN(strings.TrimSpace(stdout), "\n", 2)
	head, ok := strings.CutPrefix(lines[0], "run ")
	id, status, found := strings.Cut(head, ": ")
	if !ok || !found || id == "" || status == "" {
		return Outcome{}, fmt.Errorf("unrecognized strategy output %q", lines[0])
	}
	o := Outcome{RunID: id, Status: strings.TrimSpace(status)}
	if len(lines) < 2 {
		return o, nil
	}
	rest := strings.TrimSpace(lines[1])
	if msg, ok := strings.CutPrefix(rest, "error: "); ok {
		o.Error = strings.TrimSpace(strings.SplitN(msg, "\n", 2)[0])
		return o, nil
	}
	var s Summary
	if err := json.Unmarshal([]byte(rest), &s); err != nil {
		return o, fmt.Errorf("run %s: summary is not JSON: %w", id, err)
	}
	o.Summary, o.Raw = &s, json.RawMessage(rest)
	return o, nil
}
