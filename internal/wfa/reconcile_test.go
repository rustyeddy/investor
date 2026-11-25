package wfa

import (
	"bytes"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestReconcileRun(t *testing.T) {
	start := time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(hour int) time.Time { return start.Add(time.Duration(hour) * time.Hour) }
	usd := func(amount string) Money { return Money{Amount: amount, Currency: "USD"} }
	var journal bytes.Buffer
	enc := json.NewEncoder(&journal)
	for i, amount := range []string{"100", "80", "200", "170"} {
		point := EquityPoint{Seq: i + 1, Time: at(i), Kind: "frame", Account: "sim",
			Balance: usd(amount), Unrealized: usd("0"), Equity: usd(amount)}
		if i == 0 {
			point.Kind, point.Unrealized = "initial", usd("0")
		}
		if i == 3 {
			point.Balance, point.Unrealized = usd("160"), usd("10")
			point.Stale = []string{"EURUSD"}
		}
		if err := enc.Encode(point); err != nil {
			t.Fatal(err)
		}
	}
	returnPct, unrealized := "70", usd("10")
	sum := Summary{Account: "sim", InitialBalance: usd("100"), Balance: usd("160"),
		Bars: 3, Equity: usd("170"), Unrealized: &unrealized, NetReturnPct: &returnPct,
		MaxDrawdown:        &Drawdown{usd("30"), "15", usd("200"), usd("170"), at(2), at(3)},
		MaxPercentDrawdown: &Drawdown{usd("20"), "20", usd("100"), usd("80"), at(0), at(1)}}
	path, err := Write(t.TempDir(), Report{ID: "reconcile", OOS: &Run{
		Outcome: &Outcome{RunID: "run_123", Status: StatusCompleted, Summary: &sum},
	}})
	if err != nil {
		t.Fatal(err)
	}
	curve, err := ReconcileRun(path, "run_123", bytes.NewReader(journal.Bytes()))
	if err != nil || len(curve) != 4 || len(curve[3].Stale) != 1 {
		t.Fatalf("curve = %+v, err = %v", curve, err)
	}
	for _, tt := range []struct {
		want   string
		change func(*Summary)
	}{
		{"final account", func(s *Summary) { s.Balance = usd("159") }},
		{"net return", func(s *Summary) { s.NetReturnPct = nil }},
		{"absolute drawdown", func(s *Summary) { s.MaxDrawdown = s.MaxPercentDrawdown }},
		{"percentage drawdown", func(s *Summary) { s.MaxPercentDrawdown = s.MaxDrawdown }},
		{"observations", func(s *Summary) { s.Bars++ }},
	} {
		t.Run(tt.want, func(t *testing.T) {
			bad := sum
			tt.change(&bad)
			if _, err := ReconcileAccount(bytes.NewReader(journal.Bytes()), bad); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("wanted %q mismatch, got %v", tt.want, err)
			}
		})
	}
}

func TestPercentRoundsHalfToEven(t *testing.T) {
	for delta, want := range map[int64]string{
		1: "0.00000000", 3: "0.00000002", -1: "0.00000000", -3: "-0.00000002",
	} {
		if got := percent(big.NewRat(delta, 1), big.NewRat(20_000_000_000, 1)).FloatString(8); got != want {
			t.Errorf("delta %d: got %s, want %s", delta, got, want)
		}
	}
}

func TestParseRiskFieldsNullAndZero(t *testing.T) {
	got, err := ParseOutput("run abc: completed\n" +
		`{"net_return_pct":"0","win_rate":null,"max_drawdown":null,"unrealized":null,"settings":{"risk_per_trade":"0.01"}}`)
	if err != nil {
		t.Fatal(err)
	}
	s := got.Summary
	if s.NetReturnPct == nil || *s.NetReturnPct != "0" || s.WinRate != nil ||
		s.MaxDrawdown != nil || s.Unrealized != nil || string(s.Settings) != `{"risk_per_trade":"0.01"}` {
		t.Fatalf("unexpected summary: %+v", s)
	}
}
