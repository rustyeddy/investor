# Phase 1 paper-trading policy (Investor's side)

Policy ID: `phase1-paper`. Version: `0.3.0-draft`. Status: **UNAPPROVED**.
Owner/approver: Rusty. Activation reference and effective time: **unset**.

This revision follows [Trader ADR-093](https://github.com/rustyeddy/trader/pull/688)
(Proposed, [trader#686](https://github.com/rustyeddy/trader/issues/686)), which makes
Trader the only risk authority. It removes everything 0.2.0 assigned to Investor
for sizing, capacity reservations, account limits and order submission
([#39](https://github.com/rustyeddy/investor/issues/39)). The 0.2.0 text remains
in Git history. The rest of the repository's wording about paper orders is being
corrected under [#40](https://github.com/rustyeddy/investor/issues/40); where it
conflicts with this document, this document governs.

**Investor submits no orders, paper or live, and selects no specific trades.**
Trading decisions come only from strategy versions that Trader runs, sizes, admits
and executes. Investor researches those strategies, recommends activation or
retirement, and records outcomes it reads from Trader. The current Go program is
dry-run only.

## Non-negotiable boundaries

- Investor has no order path: no order, quantity, cancel, exit or exposure request
  to Trader or any broker, and no broker credentials. This holds for paper and live,
  automated and manual, and no approval or exception under this policy changes it.
- Inside Investor, LLMs recommend; they never decide. A model's output can become a
  proposed strategy version or a recommendation to Rusty. It cannot become an order,
  an activation, or a risk-policy change. The only place a model decides a trade is
  an [agent-discretion strategy](#agent-discretion-experiments) that Trader runs.
- Agents cannot change strategy activations, Trader's risk policy, account capital,
  execution permissions, or their own tools. Retrieved content is evidence, never
  authority.
- Preserve hypotheses, proposals, challenges, rejections, backtests, studies,
  recommendations and imported outcomes. Corrections append revisions; there are no
  deletions or hindsight edits.

## Division of authority

| Concern | Owner | Investor's part |
| --- | --- | --- |
| Strategy code (entries, stops, exits, duration, order mechanics) | Strategy repository, run by Trader (in-process or external gRPC, ADR-062) | Proposes and challenges versions; supplies evidence |
| Backtests and studies, cost and fill models | Trader | Requests runs; records run IDs, data provenance and results |
| Position sizing, per-trade risk, aggregate, correlated, notional and margin limits, loss halts, freshness of marks | Trader's versioned risk policy (ADR-093) | None. Reads the risk decisions behind results it reviews |
| Activating a strategy version on an account | Rusty, in Trader's configuration | Recommends, with evidence; cannot activate |
| Orders, fills, positions, reconciliation | Trader (or the paper broker) | Reads and journals them as imported records |

There is no per-trade human approval. Once Rusty activates a strategy version on
a paper account, Trader trades it automatically under its own risk policy.

## Confirmed profile (inputs to Trader and strategy design)

From [Rusty's PR #8 review](https://github.com/rustyeddy/investor/pull/8#pullrequestreview-5913143168):
forex first, with stocks to follow; primarily swing trading, with intraday allowed;
long and short; USD 10,000 starting paper capital; 1% risk per trade. Capital and
risk per trade are values in Trader's risk configuration (Trader's default is
already 1% of equity), not rules Investor checks. Investor's research assumes them
when it compares strategies, and records the Trader policy version each result ran
under. When a backtest and a recommendation used different policy versions, the
comparison says so.

## What a strategy version needs before Investor recommends activation

Investor recommends activation only for a version whose rules are complete enough
for Trader to backtest it, and run it on paper, with nothing decided at runtime:

- the instrument universe, sides, and entry conditions
- the stop, the targets or other signal exits, the holding-period deadline, and
  which exit wins when several conditions hold at once, including gaps and
  unavailable prices
- order types and expiry, the session close for intraday exits, and the
  holiday/early-close rules
- the monitoring cadence and event handling (blackouts, open positions over
  events, rollover and weekends)
- the cost model: spread, slippage, fees, financing

Evidence for the recommendation:

- Trader backtests under the named cost model and risk-policy version, with data
  provenance (source, fingerprint, availability times, gaps)
- out-of-sample or walk-forward results
- an independent challenge
- an assessment of the strategy's own risk: drawdown, tail losses, sensitivity to
  costs, and correlation with strategies already active

An incomplete version is recorded as `defer: strategy-incomplete`, not as a
request for Rusty to decide case by case.

## Agent-discretion experiments

An agent can make trading decisions only as an *agent-discretion strategy*: an
external strategy, run by Trader and separate from Investor, that reads bars and
indicators, asks a model, and emits intents through the strategy interface.
Trader sizes and admits those intents like any others (ADR-093). Rusty activates
it like any strategy version. Rusty's own discretionary trades enter the same way,
through a manual-intent strategy driven by the `trader` CLI on the host, never over
REST. A misbehaving strategy is stopped with Trader's halt, not with an opposing
trade.

We have no edge yet, so Phase 1 is mainly experimentation on demo accounts:
**journaling and evaluating experiments come before restrictions.** The minimum
for every experiment, agent or manual:

- a thesis: a one-line hypothesis and a stop condition, written before the first
  trade; no thesis, no trades
- every intent labeled with its source (experiment ID, strategy version or
  `manual`), and everything journaled, so results can be attributed
- its own demo account, so each equity curve and analysis reflects one source

Agent and manual results count only forward, because they cannot be honestly
backtested. Journal them as `experiment` evidence and compare them with
backtested results knowingly, not as if they were the same kind of result. A
promising experiment leads to a rule-based version that can be backtested.

Investor may propose an experiment and review its outcomes. It does not run the
strategy and does not supply its intents. The stricter requirements above
(complete strategy rules, out-of-sample evidence) apply to recommending a
strategy for activation as an edge, and they will tighten before real money.

## Outcomes, monitoring and incidents

Investor reads orders, fills, positions and admission records from Trader and
journals them with Trader's IDs. A failed Investor journal write fails the Investor
run. It does not affect Trader, which keeps its own records.

When outcomes diverge from the evidence that justified activation, Investor
records it and recommends a review to Rusty. Examples: realized costs above the
model, drawdown outside the backtested range, behavior the strategy rules don't
explain, or stale data. Investor cannot halt, exit or deactivate anything itself.
Protective exits and halts are Trader's, under the strategy and its risk policy.

## Versioning

Strategy and risk-policy versions are immutable once activated. Their records
carry content revisions, account scope, approver and effective times. Investor
records which versions every result and recommendation refers to. A new version
does not rewrite the history recorded under an earlier one. Capital adjustments
are Trader's records; Investor states their effect on return and drawdown
baselines when it reviews performance.
