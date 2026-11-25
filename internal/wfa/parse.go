package wfa

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Run statuses reported by Trader.
const StatusCompleted = "completed"

// Money is an amount as Trader sends it: {"amount": "10000", "currency": "USD"}.
// A bare string such as "10000 USD" is also read.
type Money struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency,omitempty"`
}

func (m *Money) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*m = Money{}
		return nil
	}
	var s string
	if json.Unmarshal(b, &s) == nil {
		f := strings.Fields(s)
		if len(f) == 0 || len(f) > 2 {
			return fmt.Errorf("bad money %q", s)
		}
		*m = Money{Amount: f[0]}
		if len(f) == 2 {
			m.Currency = f[1]
		}
		return nil
	}
	type plain Money
	return json.Unmarshal(b, (*plain)(m))
}

// Summary is the part of Trader's run summary that selection reads. Money
// values are kept as the decimal strings Trader sent.
type Summary struct {
	Account            string          `json:"account"`
	InitialBalance     Money           `json:"initial_balance"`
	Bars               int             `json:"bars"`
	Trades             int             `json:"trades"`
	Wins               int             `json:"wins"`
	Losses             int             `json:"losses"`
	Realized           Money           `json:"realized"`
	Balance            Money           `json:"balance"`
	Equity             Money           `json:"equity"`
	Unrealized         *Money          `json:"unrealized"`
	NetReturnPct       *string         `json:"net_return_pct"`
	WinRate            *string         `json:"win_rate"`
	MaxDrawdown        *Drawdown       `json:"max_drawdown"`
	MaxPercentDrawdown *Drawdown       `json:"max_percent_drawdown"`
	Settings           json.RawMessage `json:"settings,omitempty"`
}

// Drawdown describes one fall in the Trader account's observed equity.
type Drawdown struct {
	Amount       Money     `json:"amount"`
	Pct          string    `json:"pct"`
	PeakEquity   Money     `json:"peak_equity"`
	TroughEquity Money     `json:"trough_equity"`
	PeakTime     time.Time `json:"peak_time"`
	TroughTime   time.Time `json:"trough_time"`
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
