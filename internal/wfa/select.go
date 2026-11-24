package wfa

import (
	"fmt"
	"math/big"
	"strings"
)

// MetricNetReturn is (equity - initial balance) / initial balance, from the
// Trader summary of a training run.
const MetricNetReturn = "net_return"

// Scored is a training run with its score, or the reason it was not scored.
type Scored struct {
	Params   Params `json:"params"`
	RunID    string `json:"run_id,omitempty"`
	Trades   int    `json:"trades"`
	Score    string `json:"score,omitempty"` // Exact decimal, 8 places.
	Eligible bool   `json:"eligible"`
	Reason   string `json:"reason,omitempty"` // Why it is ineligible.

	rat *big.Rat
}

// Score evaluates one training run. A run that failed, has too few trades, or
// has an unusable initial balance is ineligible; it never scores zero.
func Score(r Run, sel Selection) Scored {
	s := Scored{Params: r.Params}
	if r.Outcome != nil {
		s.RunID = r.Outcome.RunID
	}
	if !r.OK() {
		s.Reason = "run failed or incomplete"
		if r.Err != "" {
			s.Reason += ": " + r.Err
		} else if r.Outcome != nil && r.Outcome.Error != "" {
			s.Reason += ": " + r.Outcome.Error
		}
		return s
	}
	sum := r.Outcome.Summary
	s.Trades = sum.Trades
	if sum.Trades < sel.MinTrades {
		s.Reason = fmt.Sprintf("%d trades, fewer than the minimum %d", sum.Trades, sel.MinTrades)
		return s
	}
	init, err1 := decimal(sum.InitialBalance.Amount)
	eq, err2 := decimal(sum.Equity.Amount)
	if err1 != nil || err2 != nil {
		s.Reason = fmt.Sprintf("unreadable summary amounts: %q, %q", sum.InitialBalance.Amount, sum.Equity.Amount)
		return s
	}
	if init.Sign() <= 0 {
		s.Reason = "initial balance is not positive"
		return s
	}
	s.rat = new(big.Rat).Quo(new(big.Rat).Sub(eq, init), init)
	s.Score, s.Eligible = s.rat.FloatString(8), true
	return s
}

// decimal reads a Trader amount such as "10000.00" or "10000.00 USD".
func decimal(s string) (*big.Rat, error) {
	f := strings.Fields(s)
	if len(f) == 0 {
		return nil, fmt.Errorf("empty amount")
	}
	r, ok := new(big.Rat).SetString(f[0])
	if !ok {
		return nil, fmt.Errorf("bad amount %q", s)
	}
	return r, nil
}

// Choose picks the eligible configuration with the highest score. Ties go to
// the configuration with the smaller long window, then smaller short window,
// then smaller stop, so the choice never depends on run order. It returns -1
// when nothing is eligible.
func Choose(scored []Scored) int {
	best := -1
	for i, c := range scored {
		if !c.Eligible {
			continue
		}
		if best < 0 || better(c, scored[best]) {
			best = i
		}
	}
	return best
}

func better(a, b Scored) bool {
	if c := a.rat.Cmp(b.rat); c != 0 {
		return c > 0
	}
	if a.Params.Long != b.Params.Long {
		return a.Params.Long < b.Params.Long
	}
	if a.Params.Short != b.Params.Short {
		return a.Params.Short < b.Params.Short
	}
	as, e1 := decimal(a.Params.Stop)
	bs, e2 := decimal(b.Params.Stop)
	if e1 != nil || e2 != nil {
		return a.Params.Stop < b.Params.Stop
	}
	return as.Cmp(bs) < 0
}
