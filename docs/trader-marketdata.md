# Market data from Trader

Trader owns market data: acquisition, cleaning and the canonical format. Investor
reads it over Trader's REST API (`GET /v1/marketdata/bars`, documented in
Trader's `docs/rest-api.md`) and never reads Trader's files or calls a broker.
Investor depends on Trader's leaf packages `num` and `marketdata` for the shared
value types (`marketdata.Bar`, `num.Price`) only; see
[AGENTS.md](../AGENTS.md#investor-and-trader).

## In Investor

`internal/market` defines what Investor needs:

- `Source` - `Bars(ctx, Request) (Bars, error)`. `Client` (REST) and `Fixture`
  (in-memory, deterministic) implement it. Everything else depends on `Source`,
  so tests and workflows run with no Trader, network or credentials.
- `Request` - instrument, interval (`h1` or `d1`), `From` inclusive and `To`
  exclusive by opening time, optional `AsOf` and `Strict`.
- `Bars` - the bars plus provenance: source, `RetrievedAt`, `Fingerprint`
  (SHA-256 of the exact bar content) and `Coverage`.

## Guarantees

- **No look-ahead.** Bars carry `AvailableAt`. With `AsOf`, Trader keeps only bars
  available at that instant and Investor re-checks every bar; a violation is an
  error. A forming candle is never a bar. Replays must pass `AsOf`.
- **Missing data is visible.** `Coverage` lists gaps (`closure`, `known_hole`,
  `unexplained`) and months that are `bad`, `missing` or `stale`. `Strict` turns an
  incomplete range into `ErrIncomplete`. Nothing is filled or substituted.
- **Validated.** Every bar passes `marketdata.Bar.Validate`, lies in the range,
  and is in strictly ascending order; pages follow `next_cursor` to the end.
- **Explicit errors.** `ErrNoData`, `ErrIncomplete`, `ErrUnauthorized` and
  `ErrInvalid` (also for malformed responses); an unreachable Trader is an error,
  never empty data.
- **Provenance.** Record `Source`, `Fingerprint`, `RetrievedAt`, the range, `AsOf`
  and `Coverage` with any decision that used the data.

## Configuration and CLI

```json
"trader": {
  "base_url": "http://127.0.0.1:8080",
  "token_env": "TRADER_API_TOKEN",
  "watchlist": ["EURUSD", "GBPUSD", "USDJPY", "USDCHF", "AUDUSD", "USDCAD"]
}
```

The bearer token is read from the environment variable `token_env` names (default
`TRADER_API_TOKEN`) and may be unset for a local Trader.

```sh
investor bars --instrument EURUSD --interval h1 --from 2024-01-02 --to 2024-01-03 --strict
investor snapshot        # latest completed D1 and H1 bar per watchlist pair, as of now
```

`snapshot` reports each pair and interval separately, with its completeness or
error, so one failure does not hide the others.

## Not yet

Current quotes and streaming ticks: Trader serves completed H1/D1 bars only.
Needs for them go to a Trader issue.
