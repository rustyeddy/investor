package wfa

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"time"
)

// ReconcileRun resolves a completed RunID from an Investor report. The caller
// supplies the corresponding Trader journal through an independent reader.
func ReconcileRun(reportPath, runID string, journal io.Reader) ([]EquityPoint, error) {
	data, err := os.ReadFile(reportPath)
	if err != nil {
		return nil, err
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("read Investor report: %w", err)
	}
	runs := report.Training
	if report.OOS != nil {
		runs = append(runs, *report.OOS)
	}
	for _, run := range runs {
		if run.OK() && run.Outcome.RunID == runID {
			points, err := ReconcileAccount(journal, *run.Outcome.Summary)
			if err != nil {
				return nil, fmt.Errorf("run %s: %w", runID, err)
			}
			return points, nil
		}
	}
	return nil, fmt.Errorf("completed run %q is absent from the Investor report", runID)
}

// EquityPoint retains Trader's recorded observation, including stale marks.
type EquityPoint struct {
	Seq        int       `json:"seq"`
	Time       time.Time `json:"time"`
	Kind       string    `json:"kind"`
	Account    string    `json:"account"`
	Balance    Money     `json:"balance"`
	Unrealized Money     `json:"unrealized"`
	Equity     Money     `json:"equity"`
	Stale      []string  `json:"stale,omitempty"`
}

type fall struct {
	amount *big.Rat
	pct    *big.Rat
	peak   EquityPoint
	trough EquityPoint
}

// ReconcileAccount validates the summary against the observed journal and
// returns the equity curve on success. It never derives balances or marks.
func ReconcileAccount(journal io.Reader, sum Summary) ([]EquityPoint, error) {
	var curve []EquityPoint
	var initial, peak *big.Rat
	var peakPoint EquityPoint
	var maxAmount, maxPercent fall
	scan := bufio.NewScanner(journal)
	for scan.Scan() {
		var point EquityPoint
		if err := json.Unmarshal(scan.Bytes(), &point); err != nil {
			return nil, fmt.Errorf("account observation %d: %w", len(curve)+1, err)
		}
		kind := "frame"
		if len(curve) == 0 {
			kind = "initial"
		}
		if point.Seq != len(curve)+1 || point.Kind != kind || point.Time.IsZero() ||
			sum.Account == "" || point.Account != sum.Account ||
			(len(curve) > 0 && !point.Time.After(curve[len(curve)-1].Time)) {
			return nil, fmt.Errorf("invalid account observation %d: sequence, kind, time or account mismatch", len(curve)+1)
		}
		currency := sum.InitialBalance.Currency
		for _, m := range []Money{point.Equity, point.Balance, point.Unrealized} {
			if _, err := decimal(m.Amount); err != nil || currency == "" || m.Currency != currency {
				return nil, fmt.Errorf("invalid money in account observation %d", point.Seq)
			}
		}
		equity, _ := decimal(point.Equity.Amount)
		curve = append(curve, point)
		if initial == nil {
			if equity.Sign() <= 0 || !sameMoney(point.Equity, sum.InitialBalance) ||
				!sameMoney(point.Balance, sum.InitialBalance) {
				return nil, fmt.Errorf("initial account balance or equity mismatch")
			}
			initial, peak, peakPoint = equity, equity, point
			maxAmount = fall{new(big.Rat), new(big.Rat), point, point}
			maxPercent = maxAmount
		}
		if equity.Cmp(peak) > 0 {
			peak, peakPoint = equity, point
		}
		depth := new(big.Rat).Sub(peak, equity)
		drawdown := fall{depth, percent(depth, peak), peakPoint, point}
		if depth.Cmp(maxAmount.amount) > 0 {
			maxAmount = drawdown
		}
		if drawdown.pct.Cmp(maxPercent.pct) > 0 {
			maxPercent = drawdown
		}
	}
	if err := scan.Err(); err != nil {
		return nil, fmt.Errorf("read account journal: %w", err)
	}
	if len(curve) < 2 || len(curve)-1 != sum.Bars {
		return nil, fmt.Errorf("account journal has %d observations, expected initial plus %d frames", len(curve), sum.Bars)
	}
	last := curve[len(curve)-1]
	if !sameMoney(last.Equity, sum.Equity) || !sameMoney(last.Balance, sum.Balance) ||
		sum.Unrealized == nil || !sameMoney(last.Unrealized, *sum.Unrealized) {
		return nil, fmt.Errorf("final account balance, unrealized P&L or equity mismatch")
	}
	end, _ := decimal(last.Equity.Amount)
	if sum.NetReturnPct == nil || !sameDecimal(*sum.NetReturnPct, percent(new(big.Rat).Sub(end, initial), initial)) {
		return nil, fmt.Errorf("net return percentage mismatch")
	}
	if !maxAmount.matches(sum.MaxDrawdown) {
		return nil, fmt.Errorf("maximum absolute drawdown mismatch")
	}
	if !maxPercent.matches(sum.MaxPercentDrawdown) {
		return nil, fmt.Errorf("maximum percentage drawdown mismatch")
	}
	return curve, nil
}

// Trader publishes percentages rounded to eight decimal places. Use the
// published precision when comparing falls so ties retain the first episode.
func percent(delta, base *big.Rat) *big.Rat {
	const scale = 100_000_000
	exact := new(big.Rat).Mul(new(big.Rat).Quo(delta, base), big.NewRat(100*scale, 1))
	// Round to nearest, with exact halves going to the even integer.
	q, remainder := new(big.Int), new(big.Int)
	q.QuoRem(exact.Num(), exact.Denom(), remainder)
	twiceRemainder := remainder.Abs(remainder).Lsh(remainder, 1)
	if cmp := twiceRemainder.Cmp(exact.Denom()); cmp > 0 || (cmp == 0 && q.Bit(0) != 0) {
		q.Add(q, big.NewInt(int64(exact.Sign())))
	}
	return new(big.Rat).SetFrac(q, big.NewInt(scale))
}

func sameMoney(a, b Money) bool {
	want, err := decimal(b.Amount)
	return err == nil && a.Currency != "" && a.Currency == b.Currency && sameDecimal(a.Amount, want)
}

func sameDecimal(s string, want *big.Rat) bool {
	got, err := decimal(s)
	return err == nil && got.Cmp(want) == 0
}

func (f fall) matches(d *Drawdown) bool {
	return d != nil && d.Amount.Currency == f.peak.Equity.Currency &&
		sameDecimal(d.Amount.Amount, f.amount) && sameDecimal(d.Pct, f.pct) &&
		sameMoney(d.PeakEquity, f.peak.Equity) && sameMoney(d.TroughEquity, f.trough.Equity) &&
		d.PeakTime.Equal(f.peak.Time) && d.TroughTime.Equal(f.trough.Time)
}
