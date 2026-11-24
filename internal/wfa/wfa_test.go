package wfa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/investor/internal/market"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func testSpec() Spec {
	return Spec{
		ID: "t1", Instrument: "EURUSD", Interval: "h1",
		Train:     Window{day(2010, 1, 1), day(2015, 1, 1)},
		Test:      Window{day(2015, 1, 1), day(2016, 1, 1)},
		Holdout:   day(2023, 1, 1),
		TraderAPI: TraderAPI{BaseURL: "http://127.0.0.1:8080"},
		Strategy:  Strategy{Executable: "emax", Trader: "127.0.0.1:9100"},
		Grid:      Grid{Short: []int{2, 5}, Long: []int{4, 10}, Stop: []string{"0.01"}},
		Selection: Selection{Metric: MetricNetReturn, MinTrades: 5}, RunTimeout: Duration(time.Minute), ExpectedInitialBalance: "10000", ExpectedCurrency: "USD",
	}
}

// fakeRunner answers from a table keyed by phase and params; it records calls.
type fakeRunner struct {
	equity map[Params]string // Equity of each configuration; missing means failure.
	trades int
	calls  []RunSpec
}

func (f *fakeRunner) Run(_ context.Context, s RunSpec) Run {
	f.calls = append(f.calls, s)
	r := Run{Phase: s.Phase, Params: s.Params, Window: s.Window, Started: day(2026, 1, 1)}
	eq, ok := f.equity[s.Params]
	if !ok {
		r.ExitCode, r.Err = 1, "strategy exited with status 1"
		return r
	}
	r.Outcome = &Outcome{RunID: fmt.Sprintf("run-%d-%d-%s", len(f.calls), s.Params.Short, s.Phase), Status: StatusCompleted,
		Summary: &Summary{InitialBalance: Money{"10000", "USD"}, Equity: Money{eq, "USD"}, Trades: f.trades}}
	return r
}

func TestConfigsSkipInvalidAndKeepOrder(t *testing.T) {
	s := testSpec()
	s.Grid = Grid{Short: []int{5, 2}, Long: []int{4, 10}, Stop: []string{"0.02", "0.01"}}
	got := s.Configs()
	want := []Params{{5, 10, "0.02"}, {5, 10, "0.01"}, {2, 4, "0.02"}, {2, 4, "0.01"}, {2, 10, "0.02"}, {2, 10, "0.01"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("configs = %v, want %v", got, want)
	}
}

func TestValidateWindowsAndHoldout(t *testing.T) {
	for name, mod := range map[string]func(*Spec){
		"overlap":         func(s *Spec) { s.Test.From = day(2014, 6, 1) },
		"touches holdout": func(s *Spec) { s.Test.To = day(2023, 6, 1) },
		"reversed":        func(s *Spec) { s.Train.From = day(2016, 1, 1) },
		"no holdout":      func(s *Spec) { s.Holdout = time.Time{} },
		"empty grid":      func(s *Spec) { s.Grid.Short = []int{10} },
		"bad metric":      func(s *Spec) { s.Selection.Metric = "sharpe" },
	} {
		s := testSpec()
		mod(&s)
		if s.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := testSpec().Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := (Experiment{Spec: Spec{}}).Execute(context.Background()); err == nil {
		t.Error("empty spec executed")
	}
}

func TestExampleSpecLoads(t *testing.T) {
	if _, err := LoadSpec("../../examples/wfa-emax-eurusd.json"); err != nil {
		t.Fatal(err)
	}
}

func TestParseOutput(t *testing.T) {
	o, err := ParseOutput("run r-1: completed\n" + realSummary + "\n")
	if err != nil || o.RunID != "r-1" || o.Status != "completed" || o.Summary.Trades != 107 || o.Summary.Equity.Amount != "9803.04774" || o.Summary.InitialBalance.Currency != "USD" || len(o.Raw) == 0 {
		t.Fatalf("%+v %v", o, err)
	}
	o, err = ParseOutput("run r-2: failed\nerror: no data\n")
	if err != nil || o.Status != "failed" || o.Error != "no data" || o.Summary != nil {
		t.Fatalf("%+v %v", o, err)
	}
	for _, bad := range []string{"", "hello", "run : x", "run r-3: completed\nnot json"} {
		if _, err := ParseOutput(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestChooseByScoreThenTieBreak(t *testing.T) {
	sel := Selection{MinTrades: 5}
	mk := func(p Params, eq string, trades int) Scored {
		return Score(Run{Params: p, Outcome: &Outcome{RunID: "x", Status: StatusCompleted,
			Summary: &Summary{InitialBalance: Money{"10000.00", "USD"}, Equity: Money{eq, "USD"}, Trades: trades}}}, sel)
	}
	scored := []Scored{
		mk(Params{5, 10, "0.01"}, "10500.00", 9),
		mk(Params{2, 10, "0.01"}, "10500.00", 9), // Ties with the first; smaller short wins.
		mk(Params{2, 4, "0.01"}, "10500.00", 9),  // Ties again; smaller long wins.
		mk(Params{2, 4, "0.02"}, "12000.00", 2),  // Best score but too few trades.
		mk(Params{3, 4, "0.01"}, "9000.00", 9),
	}
	if scored[3].Eligible || !strings.Contains(scored[3].Reason, "fewer than") {
		t.Errorf("min trades not enforced: %+v", scored[3])
	}
	if got := Choose(scored); got != 2 {
		t.Errorf("chose %d, want 2", got)
	}
	if scored[0].Score != "0.05000000" || scored[4].Score != "-0.10000000" {
		t.Errorf("scores %s %s", scored[0].Score, scored[4].Score)
	}
}

func TestScoreNeverTreatsFailureAsZero(t *testing.T) {
	sel := Selection{MinTrades: 1}
	for name, r := range map[string]Run{
		"launch error":   {Err: "boom"},
		"failed run":     {Outcome: &Outcome{Status: "failed", Error: "x"}},
		"zero balance":   {Outcome: &Outcome{Status: StatusCompleted, Summary: &Summary{InitialBalance: Money{Amount: "0"}, Equity: Money{Amount: "1"}, Trades: 3}}},
		"bad amount":     {Outcome: &Outcome{Status: StatusCompleted, Summary: &Summary{InitialBalance: Money{Amount: "10000"}, Equity: Money{Amount: "?"}, Trades: 3}}},
		"mixed currency": {Outcome: &Outcome{Status: StatusCompleted, Summary: &Summary{InitialBalance: Money{"10000", "USD"}, Equity: Money{"10100", "EUR"}, Trades: 3}}},
		"exit status":    {ExitCode: 1, Outcome: &Outcome{Status: StatusCompleted, Summary: &Summary{InitialBalance: Money{Amount: "10000"}, Equity: Money{Amount: "10000"}, Trades: 3}}},
	} {
		if s := Score(r, sel); s.Eligible || s.Score != "" || s.Reason == "" {
			t.Errorf("%s: %+v", name, s)
		}
	}
	if Choose(nil) != -1 {
		t.Error("choice from nothing")
	}
}

func TestExecuteTrainsThenOneOOSRunOfTheChoice(t *testing.T) {
	spec := testSpec()
	f := &fakeRunner{trades: 9, equity: map[Params]string{
		{2, 4, "0.01"}: "10100.00", {2, 10, "0.01"}: "10900.00", // {5, 10} is missing, so it fails.
	}}
	rep, err := Experiment{Spec: spec, Runner: f, Now: func() time.Time { return day(2026, 1, 2) }}.Execute(context.Background())
	if err != nil || rep.Status != StatusComplete {
		t.Fatalf("status %s, err %v", rep.Status, err)
	}
	if want := (Params{2, 10, "0.01"}); rep.Selected == nil || *rep.Selected != want {
		t.Fatalf("selected %+v", rep.Selected)
	}
	// Exactly the grid on training, then one OOS run, on the exact boundaries.
	if len(f.calls) != 4 {
		t.Fatalf("%d runs", len(f.calls))
	}
	for _, c := range f.calls[:3] {
		if c.Phase != PhaseTrain || c.Window != spec.Train {
			t.Errorf("training call %+v", c)
		}
	}
	if last := f.calls[3]; last.Phase != PhaseTest || last.Window != spec.Test || last.Params != *rep.Selected {
		t.Errorf("oos call %+v", last)
	}
	// Failed training run is recorded, not scored as zero.
	var failed int
	for _, s := range rep.Scores {
		if !s.Eligible {
			failed++
		}
	}
	if failed != 1 || rep.OOS == nil || !rep.OOS.OK() {
		t.Errorf("failed=%d oos=%+v", failed, rep.OOS)
	}
}

func TestOOSDoesNotInfluenceChoice(t *testing.T) {
	spec := testSpec()
	train := map[Params]string{{2, 4, "0.01"}: "10100.00", {2, 10, "0.01"}: "10900.00"}
	a := &fakeRunner{trades: 9, equity: train}
	repA, _ := Experiment{Spec: spec, Runner: a}.Execute(context.Background())
	// A runner whose OOS answers differ must not change which configuration is chosen.
	b := &phaseRunner{fakeRunner: fakeRunner{trades: 9, equity: train}}
	repB, _ := Experiment{Spec: spec, Runner: b}.Execute(context.Background())
	if *repA.Selected != *repB.Selected {
		t.Errorf("choice moved with OOS: %+v vs %+v", repA.Selected, repB.Selected)
	}
}

type phaseRunner struct{ fakeRunner }

func (p *phaseRunner) Run(ctx context.Context, s RunSpec) Run {
	r := p.fakeRunner.Run(ctx, s)
	if s.Phase == PhaseTest {
		r.Outcome.Summary.Equity = Money{"99999.00", "USD"}
	}
	return r
}

func TestNoEligibleSkipsOOS(t *testing.T) {
	f := &fakeRunner{trades: 1, equity: map[Params]string{{2, 4, "0.01"}: "10100.00"}}
	rep, err := Experiment{Spec: testSpec(), Runner: f}.Execute(context.Background())
	if err != nil || rep.Status != StatusNoEligible || rep.Selected != nil || rep.OOS != nil {
		t.Fatalf("%s %v %+v", rep.Status, err, rep)
	}
	for _, c := range f.calls {
		if c.Phase != PhaseTrain {
			t.Errorf("non-training run %+v", c)
		}
	}
}

func TestOOSFailureIsAnError(t *testing.T) {
	f := &fakeRunner{trades: 9, equity: map[Params]string{{2, 4, "0.01"}: "10100.00"}}
	r := &failOOS{f}
	rep, err := Experiment{Spec: testSpec(), Runner: r}.Execute(context.Background())
	if err == nil || rep.Status != StatusTestFailed || rep.OOS == nil || rep.OOS.OK() {
		t.Fatalf("%s %v", rep.Status, err)
	}
}

type failOOS struct{ *fakeRunner }

func (f *failOOS) Run(ctx context.Context, s RunSpec) Run {
	if s.Phase == PhaseTest {
		return Run{Phase: s.Phase, Params: s.Params, Window: s.Window, Err: "launch failed"}
	}
	return f.fakeRunner.Run(ctx, s)
}

func TestPreflight(t *testing.T) {
	spec := testSpec()
	f := &fakeRunner{trades: 9, equity: map[Params]string{{2, 4, "0.01"}: "10100.00"}}
	rep, err := Experiment{Spec: spec, Runner: f, Source: stubSource{err: market.ErrIncomplete}}.Execute(context.Background())
	if err == nil || rep.Status != StatusPreflight || len(f.calls) != 0 {
		t.Fatalf("%s %v calls=%d", rep.Status, err, len(f.calls))
	}
	if len(rep.Data) != 2 || rep.Data[0].Error == "" {
		t.Errorf("data %+v", rep.Data)
	}
	// An incomplete series fails even when the source returns it.
	rep, err = Experiment{Spec: spec, Runner: f, Source: stubSource{complete: false}}.Execute(context.Background())
	if err == nil || rep.Status != StatusPreflight {
		t.Fatalf("incomplete accepted: %s", rep.Status)
	}
	rep, err = Experiment{Spec: spec, Runner: f, Source: stubSource{complete: true}}.Execute(context.Background())
	if err != nil || rep.Status != StatusComplete || rep.Data[0].Fingerprint != "fp" {
		t.Fatalf("%s %v %+v", rep.Status, err, rep.Data)
	}
}

type stubSource struct {
	err      error
	complete bool
}

func (s stubSource) Bars(_ context.Context, r market.Request) (market.Bars, error) {
	if s.err != nil {
		return market.Bars{}, s.err
	}
	if !r.Strict || r.AsOf.IsZero() {
		return market.Bars{}, errors.New("preflight must be strict and as-of bounded")
	}
	return market.Bars{Source: "stub", Fingerprint: "fp", Coverage: market.Coverage{Complete: s.complete}}, nil
}

func TestReportDeterministicAndNotOverwritten(t *testing.T) {
	run := func() Report {
		f := &fakeRunner{trades: 9, equity: map[Params]string{{2, 4, "0.01"}: "10100.00", {2, 10, "0.01"}: "10900.00"}}
		rep, err := Experiment{Spec: testSpec(), Runner: f, Now: func() time.Time { return day(2026, 1, 2) }}.Execute(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}
	a, _ := json.Marshal(run())
	b, _ := json.Marshal(run())
	if string(a) != string(b) {
		t.Error("reports differ between identical runs")
	}
	dir := t.TempDir()
	path, err := Write(dir, run())
	if err != nil || filepath.Base(path) != "t1.json" {
		t.Fatalf("%q %v", path, err)
	}
	if _, err := Write(dir, run()); err == nil {
		t.Error("overwrote an existing report")
	}
	data, _ := os.ReadFile(path)
	var back Report
	if err := json.Unmarshal(data, &back); err != nil || !strings.HasPrefix(back.Provenance["trader_build"], Unknown) {
		t.Errorf("round trip: %v %v", err, back.Provenance)
	}
	if s := run().Summary(); !strings.Contains(s, "selected short=2 long=10") || !strings.Contains(s, "oos run=") {
		t.Errorf("summary:\n%s", s)
	}
}

func TestProcessArgsAndFailures(t *testing.T) {
	p := &Process{Strategy: Strategy{Executable: "x", Trader: "127.0.0.1:9100", Tick: "0.00001"}, Instr: "EURUSD", Interval: "h1", Timeout: time.Second}
	got := strings.Join(p.Args(RunSpec{Params: Params{2, 10, "0.01"}, Window: Window{day(2010, 1, 1), day(2015, 1, 1)}}), " ")
	want := "-trader 127.0.0.1:9100 -instrument EURUSD -interval h1 -from 2010-01-01T00:00:00Z -to 2015-01-01T00:00:00Z -short 2 -long 10 -stop 0.01 -tick 0.00001"
	if got != want {
		t.Errorf("args\n got %s\nwant %s", got, want)
	}
	p.Strategy.Executable = filepath.Join(t.TempDir(), "missing")
	if r := p.Run(context.Background(), RunSpec{}); r.OK() || r.Err == "" || r.ExitCode != -1 {
		t.Errorf("missing executable: %+v", r)
	}
	// A process that fails after printing a result keeps the RunID.
	script := filepath.Join(t.TempDir(), "emax")
	os.WriteFile(script, []byte("#!/bin/sh\necho 'run r-9: failed'\necho 'error: boom'\nexit 1\n"), 0o755)
	p.Strategy.Executable = script
	r := p.Run(context.Background(), RunSpec{})
	if r.OK() || r.ExitCode != 1 || r.Outcome == nil || r.Outcome.RunID != "r-9" || r.Outcome.Error != "boom" {
		t.Errorf("failed run: %+v", r)
	}
	// Success path through a real process.
	os.WriteFile(script, []byte(`#!/bin/sh
echo 'run r-10: completed'
echo '{"trades":6,"initial_balance":{"amount":"10000"},"equity":{"amount":"10200.00"}}'
`), 0o755)
	if r := p.Run(context.Background(), RunSpec{}); !r.OK() || r.Outcome.RunID != "r-10" {
		t.Errorf("good run: %+v", r)
	}
	// Overlap is refused.
	p.mu.Lock()
	if r := p.Run(context.Background(), RunSpec{}); r.Err != ErrBusy.Error() {
		t.Errorf("overlap: %+v", r)
	}
	p.mu.Unlock()
	// A hung process is killed at the timeout and reported as failed.
	os.WriteFile(script, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755)
	p.Timeout = 100 * time.Millisecond
	if r := p.Run(context.Background(), RunSpec{}); r.OK() || !strings.Contains(r.Err, "did not finish") {
		t.Errorf("timeout: %+v", r)
	}
}

// realSummary is the summary line `emax` printed against a live `trader serve`
// (2014-01-01 to 2014-04-01, EURUSD h1, short 5, long 20, stop 0.01).
const realSummary = `{"strategy":"remote","account":"sim123","initial_balance":{"amount":"10000","currency":"USD"},"bars":1514,"orders":{"submitted":323,"filled":215,"rejected":0,"cancelled":107,"pending":1},"trades":107,"wins":29,"losses":78,"break_even":0,"realized":{"amount":"-212.39832","currency":"USD"},"unrealized":{"amount":"15.44606","currency":"USD"},"balance":{"amount":"9787.60168","currency":"USD"},"equity":{"amount":"9803.04774","currency":"USD"},"open":[{"instrument":"EURUSD","direction":"long","units":"7118","entry_price":"1.37522","mark_price":"1.37739"}]}`

func TestValidateStopAndBalance(t *testing.T) {
	for name, mod := range map[string]func(*Spec){
		"zero stop":    func(s *Spec) { s.Grid.Stop = []string{"0"} },
		"stop of one":  func(s *Spec) { s.Grid.Stop = []string{"1"} },
		"percent text": func(s *Spec) { s.Grid.Stop = []string{"1%"} },
		"no balance":   func(s *Spec) { s.ExpectedInitialBalance = "" },
		"no currency":  func(s *Spec) { s.ExpectedCurrency = "" },
		"bad tick":     func(s *Spec) { s.Strategy.Tick = "-1" },
	} {
		s := testSpec()
		mod(&s)
		if s.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestBalanceMismatchStopsBeforeOOS(t *testing.T) {
	spec := testSpec()
	spec.ExpectedInitialBalance = "20000" // The fake runner reports 10000.
	f := &fakeRunner{trades: 9, equity: map[Params]string{{2, 4, "0.01"}: "10100.00"}}
	rep, err := Experiment{Spec: spec, Runner: f}.Execute(context.Background())
	if err == nil || rep.Status != StatusMismatch || rep.OOS != nil || rep.Provenance["trader_initial_balance"] != "10000 USD" {
		t.Fatalf("%s %v %v", rep.Status, err, rep.Provenance)
	}
	for _, c := range f.calls {
		if c.Phase == PhaseTest {
			t.Error("OOS ran despite a settings mismatch")
		}
	}
}

func TestGitIdentity(t *testing.T) {
	dir := t.TempDir()
	if id := GitIdentity(dir); !strings.HasPrefix(id, Unknown) {
		t.Errorf("non-repo: %q", id)
	}
	git := func(a ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, a...)...).CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
	git("init", "-q")
	os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644)
	git("add", "f")
	git("commit", "-q", "-m", "c")
	clean := GitIdentity(dir)
	if len(clean) != 40 {
		t.Fatalf("clean: %q", clean)
	}
	os.WriteFile(filepath.Join(dir, "f"), []byte("y"), 0o644)
	if id := GitIdentity(dir); id != clean+"+dirty" {
		t.Errorf("dirty: %q", id)
	}
}

func TestSourceFromSpec(t *testing.T) {
	env := func(k string) string { return map[string]string{"TRADER_API_TOKEN": "tok", "MY_TOKEN": "mine"}[k] }
	s := testSpec()
	src, err := s.Source("", env)
	if c, ok := src.(*market.Client); err != nil || !ok || c.BaseURL != "http://127.0.0.1:8080" || c.Token != "tok" {
		t.Fatalf("%+v %v", src, err)
	}
	s.TraderAPI.TokenEnv = "MY_TOKEN"
	src, _ = s.Source("http://127.0.0.1:9", env)
	if c := src.(*market.Client); c.BaseURL != "http://127.0.0.1:9" || c.Token != "mine" {
		t.Errorf("override: %+v", c)
	}
	s.TraderAPI = TraderAPI{}
	if _, err := s.Source("", env); err == nil {
		t.Error("no URL accepted")
	}
}

func TestCurrencyMismatchStopsBeforeOOS(t *testing.T) {
	spec := testSpec()
	spec.ExpectedCurrency = "EUR" // The fake runner reports USD.
	f := &fakeRunner{trades: 9, equity: map[Params]string{{2, 4, "0.01"}: "10100.00"}}
	rep, err := Experiment{Spec: spec, Runner: f}.Execute(context.Background())
	if err == nil || rep.Status != StatusMismatch || rep.OOS != nil {
		t.Fatalf("%s %v", rep.Status, err)
	}
}
