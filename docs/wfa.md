# Walk-forward experiment (WFA-01)

`investor wfa run` runs one walk-forward experiment ([#42](https://github.com/rustyeddy/investor/issues/42)):
every grid configuration is backtested on the training window, one is chosen from
training results alone, and exactly that one is backtested once out of sample.

Trader owns the data, fills, risk and accounting, and the RunIDs. The standalone
strategy (`strategies/cmd/emax`) connects to `trader serve` over loopback TCP/JSON
and asks for one historical run. Investor launches those processes one at a time,
reads each result, chooses, and records. All logic is in `internal/wfa`; the cobra
command in `cmd/investor/wfa.go` only turns flags into arguments and prints.

## Spec

`examples/wfa-emax-eurusd.json` is the first experiment. It fixes, before any
out-of-sample result exists: instrument, interval, the training and test windows
(`from` inclusive, `to` exclusive, UTC), the grid, and the selection rule.
Validation rejects overlapping windows and any window that reaches
`holdout_start` (the untouched final holdout), and unknown fields.

The grid is the cross product of `short`, `long` and `stop`, in the order written,
skipping configurations whose short window is not below the long.

## Selection

Score is `net_return = (equity - initial balance) / initial balance`, from Trader's
run summary, as an exact decimal. A run is ineligible, never scored as zero, if the
process failed or timed out, the status is not `completed`, the summary is missing
or unreadable, the initial balance is not positive, or it has fewer than
`min_trades` trades. The highest score wins; ties go to the smaller long window,
then the smaller short window, then the smaller stop. If nothing is eligible the
report says `no_eligible_selection` and the out-of-sample window is not used.

## Data preflight

Before any backtest, each window's bars are read through `market.Source` (Trader's
REST API) with `Strict` and `AsOf` set to the window's end. An incomplete or missing
range stops the experiment (`preflight_failed`); windows are never shortened. The
report records source, bar count, fingerprint and gaps. `--skip-preflight` omits
this and the report says so. Warm-up bars before a window are not checked.

## Report

`<out>/<id>.json` holds the spec, data checks, every training run (exit code,
stdout, stderr, RunID, Trader's summary), the scores, the selection and rationale,
the OOS run, and limitations. Unavailable build provenance is recorded as
`unknown`. An existing report is
never overwritten; reruns need a new `id`. A short summary is printed.

## Stop distance versus risk

EMAX's `stop` is a **price-distance stop**: a fraction of the entry price, so
`0.01` is 1% of the price. It is not Trader's per-trade risk (1% of equity), which
Trader uses to size the position from that stop distance. Validation requires
every grid stop to be a decimal above 0 and below 1.

## Effective Trader settings and build identity

Trader's run summary carries the starting balance it used. The spec's
`expected_initial_balance` and `expected_currency` are compared with it on every run (the equity currency too); a difference stops the
experiment (`settings_mismatch`) before the out-of-sample run, and the observed
value is recorded. Current Trader builds return each completed run's effective
`settings`, which Investor records with the run. Older summaries may omit them.

## WFA-02 account reconciliation

[WFA-02](https://github.com/rustyeddy/investor/issues/45) uses the same checked-in
spec and training selection as WFA-01. Current Trader summaries include net
return in percent, win rate as a fraction, and two drawdown episodes with
peak/trough equity and times. The largest dollar fall and the largest percentage
fall can be different episodes. A null win rate means unavailable, while a
reported zero is a measured value. Older summaries can lack the new fields.

Given an Investor report and a journal supplied for its RunID through an
authorized export, validate the journal and emit an equity curve as JSON:

```sh
investor wfa reconcile <investor-report> <run-id> < exported-account.jsonl
```

The command checks the observation sequence and times, verifies that each
observation's equity equals its balance plus unrealized P&L, and reconciles
initial and final account figures, net return, and both drawdown episodes.
An availability frame can contain bars from multiple instruments.
The command exits with an error on discrepancies
or missing required fields. Curve points preserve the journal's simulated time,
reported equity, and stale-mark indicators. The caller supplies the matching
journal and its provenance.

The [WFA-01 baseline](https://github.com/rustyeddy/portfolio/tree/main/backtests/emax/EURUSD/H1/wfa01/run1)
selected short 5, long 50, stop 0.01, with −12.91% training and +10.66% OOS net
return. Trader's run files reach Investor through the Portfolio ingress
(`/srv/portfolio/.incoming/runs`, Trader's `results.root`) rather than a REST
interface; see [portfolio.md](portfolio.md). The WFA-02 real-data run, its
archive and the result comparison are pending
[Investor #48](https://github.com/rustyeddy/investor/issues/48).

`trader_executable` names the trader binary; the report records the version it prints
(`trader version`, for example `v0.5.1`) as `trader_version`, or `unknown`. It is
the binary named, assumed to be the server that ran the backtests.

`trader_repo` and `strategies_repo` name the checkouts; the report records their
HEAD commits, with `+dirty` for uncommitted changes. This is the checkout, not
the binary, so build the binaries from those checkouts first.

## Where official results go

Official reports belong in the Portfolio repository, archived as experiment
packages with their Trader runs and checksums; see [portfolio.md](portfolio.md)
for the layout, the workflow and `investor portfolio`. Until `investor` prepares
and archives packages itself, pass a prepared Portfolio worktree's `backtests`
directory as `--out`, never the main checkout. Investor never overwrites a
report. Scratch and test runs should use another `--out`.

## Reproduce

Adjacent checkouts: `../trader`, `../strategies`, `../investor`.

```sh
(cd ../strategies && go build -o bin/emax ./cmd/emax)
(cd ../trader && go build -o bin/trader ./cmd/trader && ./bin/trader serve)   # another terminal; needs the canonical data under /srv/data (--data-root)
make build
./bin/investor wfa run --spec examples/wfa-emax-eurusd.json --out /srv/portfolio.worktrees/<experiment>/backtests   # a prepared worktree (docs/portfolio.md)
```

The preflight needs Trader's HTTP API: `trader_api.base_url` in the spec (or
`--trader`), and the token in the environment variable named by
`trader_api.token_env` (default `TRADER_API_TOKEN`) if Trader requires one. The
firm configuration is not used. Use `--strategy-listen` and `--listen` ports that match
`strategy.trader` and `trader_api.base_url` in the spec.

Smoke-tested on 2026-11-23 with Trader `6f4c276`: a single run
(`emax -trader 127.0.0.1:19100 -instrument EURUSD -interval h1 -from 2014-01-01 -to 2014-04-01 -short 5 -long 20 -stop 0.01`)
exited 0 and printed `run <RunID>: completed` and a one-line JSON summary in which
money values are `{"amount","currency"}` objects. The full grid and one OOS run
then completed in about 13 seconds.
