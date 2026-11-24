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
the OOS run, and limitations. Provenance Investor cannot obtain yet (Trader and
strategy builds, risk configuration) is the string `unknown`. An existing report is
never overwritten; reruns need a new `id`. A short summary is printed.

## Reproduce

Adjacent checkouts: `../trader`, `../strategies`, `../investor`.

```sh
(cd ../strategies && go build -o bin/emax ./cmd/emax)
(cd ../trader && go build -o bin/trader ./cmd/trader && ./bin/trader serve)   # another terminal; needs the canonical EURUSD H1 data
make build
./bin/investor wfa run --spec examples/wfa-emax-eurusd.json --out wfa-reports
```

The preflight needs Trader's HTTP API (`trader.base_url` in the config file or
`--trader`, and `TRADER_API_TOKEN` if required). The exact Trader serve flags and a
real run have not been recorded yet; see the open questions on #42.
