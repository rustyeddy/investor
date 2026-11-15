# Manual Phase 1 research session

Use the [role contracts](../roles/README.md) and
[versioned paper policy](paper-trading-policy.md). The first workflow researches
forex swing opportunities, with long/short and intraday strategies supported.
Stocks remain part of the design but require market-specific configuration.
Trader, a model API, and new Go code are not required for this manual session.

Rusty approves strategy and risk-policy versions in advance. There is **no human
approval per paper trade**. A manual evaluator applies the deterministic rules
until software exists; future Investor performs those checks and may submit
qualifying paper orders automatically. A reject/defer decision also completes
the research workflow. Proposal, eligibility, order and fill remain distinct.

## Session record

Create a durable folder or notebook with a unique session ID. Retain source
snapshots and the shared metadata in the role contracts for every artifact.

| Record | Owner | Required contents / next handoff |
| --- | --- | --- |
| Session brief | Research Director | Objective, universe, horizon, cutoff, active strategy/policy versions, participants → Market Analyst |
| Observations/candidates | Market Analyst | Executable-side prices, events, conversion evidence, sources/times, hypothesis and gaps → Researcher |
| Proposal | Strategy Researcher | Strategy version, long/short, thesis/falsifier, entry/stop/exit, units, costs/financing, swing days or intraday close, expiry and cadence → Challenger pass |
| Challenge | Strategy Researcher/Challenger | Counterevidence, alternative explanations, recommendation and reviewer identity → Risk Officer |
| Risk memo/check | Risk Officer + manual evaluator | Advisory memo separately from rule inputs/results, strategy/policy activation references and validity → Paper Trader |
| Paper decision/lifecycle | Paper Trader | Order/reject/defer reason; reservation, order ID, fills/unfilled status, scheduled exits, monitoring and incidents → Performance Analyst |
| Session/weekly review | Director / Performance Analyst | All candidates, outcomes, missing data, lessons and follow-up questions |

Strategy/policy activation records identify Rusty, exact revisions, account scope
and effective time. Mark them pending or absent when appropriate; silence is
not activation. Do not create a per-trade approval field or handoff to Rusty.
Blocked records name the missing input, responsible role, and next review time.
Revised proposals repeat challenge and checks; policy/strategy changes separately
require versioned approval. Preserve earlier artifacts and rejected proposals.

## Daily research and proposal evaluation

1. Record the objective, forex universe, strategy/policy versions and activation
   status. With unapproved/incomplete configuration, label the work exploratory
   and prohibit order submission.
2. Collect current prices, both-currency events and conversion evidence from
   identified sources. Record market/publication/retrieval times and known
   delay separately. Distinguish facts from hypotheses.
3. Write a proposal within an approved strategy and a separate challenge. State
   base units, side, entry bounds, stop, costs/financing, and exit rules. Swing
   trades specify days and calendar convention; intraday trades specify exact
   market/session close with timezone and holiday handling. Request missing
   evidence rather than inventing it.
4. Attach the advisory risk memo and a rule-by-rule worksheet. Use configured
   capital (initial default USD 10,000), current marked equity, and configured
   risk fraction (default 1%). Check aggregate, correlated/currency, notional,
   margin and simultaneous exposure, including pending entries. Missing inputs
   are `blocked`, breached limits `fail`; neither can be overridden by an LLM.
5. A passing check under active approved versions goes directly to Paper Trader.
   Otherwise record reject/defer and reasons. Changing a strategy or policy
   requires Rusty's version approval; an ordinary eligible trade does not.

## Paper execution and monitoring

6. Revalidate versions, check validity, fresh evidence and portfolio state before
   entry; record the reservation and unique order ID. Changed inputs or expired
   checks require fresh deterministic evaluation, not discretionary approval.
7. Record submission separately from fills. Apply the approved paper fill model
   to subsequent observations, keeping partial fills, unfilled quantities,
   costs, financing and timestamps. Never fill retrospectively using information
   unavailable at decision time. Reconcile uncertain submission before retrying.
8. Monitor at the strategy's duration-appropriate cadence and required triggers;
   no blanket five-minute cycle. Apply approved stop/signal exits and the
   specified swing-day deadline or intraday market close. Record the calculated
   deadline and the next review. Entry freshness and protective exit handling
   remain separate from periodic research reviews. If data/markets are
   unavailable, follow the explicit next-tradable-time rule, preserve unresolved
   exposure and incidents, and never fabricate an on-time close.
9. Append the decision and order/position status. Zero orders is a valid outcome.
   Breaches or missed-monitoring tolerance halt new exposure under policy while
   preserving authorized risk-reducing exits and records.

## Weekly performance review

Reconcile the ledger first. Report candidates, proposals, rejections, deferrals,
submitted/unfilled orders, and open/closed positions. Separate realized and
marked unrealized P&L with price/conversion timestamps, execution costs and
financing. Review aggregate and currency/group exposure, drawdown, deviations,
missing data, challenge effectiveness and strategy/policy/model versions. Do
not claim profitability from incomplete or small samples. Research questions
go to Director; suggested configuration changes go to Rusty, never self-activate.

## Worked forex decision: research complete, configuration incomplete

This is a **synthetic worksheet fixture**, not current EUR/USD market evidence,
an activated account/strategy, or a claim that a trading session ran. All times
are fixture UTC times on `2026-11-16`; source `fixture:fx-opportunity-001` is
synthetic and retained with the worksheet. Set author/model to `human`, policy
to `phase1-paper/0.2.0-draft`, and strategy to unapproved `fx-swing-demo/1`.

| Artifact | Example contents |
| --- | --- |
| `demo-brief-1` at 15:00 | Director asks whether a EUR/USD short swing hypothesis merits a paper entry |
| `demo-observation-1` at 15:01 | Synthetic executable bid 1.1000 USD/EUR at 15:00:30, retrieved 15:00:31; USD account, quote-to-account rate 1; event/group evidence unavailable |
| `demo-proposal-1` at 15:02 | Sell 9,000 EUR base units at no less than 1.1000; stop 1.1100, target 1.0800; fixture cost allowance USD 10 including financing; order expiry 15:15 |
| `demo-exit-plan-1` at 15:02 | Three calendar days from simulated fill time; earliest stop, target or deadline exits, stop wins a simultaneous stop/target ambiguity; next tradable observation if closed; hourly reviews plus protective price triggers; all illustrative strategy settings, not activated |
| `demo-challenge-1` at 15:03 | Same human in separate challenger pass: reversal/currency-event risk, unverified costs and execution model; recommend defer |
| `demo-risk-1` at 15:04 | Arithmetic below fits the default per-trade budget; aggregate/group/margin checks and strategy activation blocked |
| `demo-decision-1` at 15:04 | `defer: configuration-unapproved-or-incomplete`; orders 0, fills 0; no per-trade human approval requested |
| `demo-review-1` at 15:05 | One candidate/proposal/defer; no trades, realized P&L not applicable; request missing configuration/evidence |

For arithmetic only, assume equity USD 10,000, no existing positions or pending
orders, an allowed increment of 1,000 EUR units, and the fixture cost allowance:

```text
risk budget = 0.01 * 10,000 = 100 USD
short stop distance = 1.1100 - 1.1000 = 0.0100 USD/EUR
planned loss = 9,000 EUR * 0.0100 USD/EUR * 1 + 10 USD = 100 USD
absolute notional = 9,000 EUR * 1.1000 USD/EUR * 1 = 9,900 USD
10,000 EUR would risk 110 USD including costs: reject that requested size
```

This is not a margin/correlation pass: those controls, the exact universe,
event evidence, execution/cost model and strategy activation are missing.
Default 1% per-trade risk does not waive aggregate limits. Record the blocked
check and defer; do not manufacture missing approvals or request a one-off
trade exception. With complete activated versions and passing checks, the same
handoff would permit paper submission automatically, with no per-trade human
gate. In the manual session the evaluator records the calculation; in future
software Go enforces it. Neither a passing check nor submission proves a fill.

If a later fixture supplies a fill at `2026-11-16T15:06:00Z`, the illustrated
three-calendar-day deadline is `2026-11-19T15:06:00Z`; a prior stop/target takes
precedence. A strategy using trading days must instead apply its approved
calendar. Intraday strategies use their configured session close, not this
three-day deadline. These example settings do not activate either strategy.

## Review scenarios

- Approved strategy/policy, complete evidence and passing checks: direct paper
  submission eligibility without human trade approval; fills need evidence.
- Long and short forex: stop on the correct side, approved unit increments,
  account-currency conversion, margin and all costs checked.
- Missing conversion, policy, strategy, group or event evidence: blocked/defer.
- 1% per-trade pass but aggregate/correlated cap exceeded: reject; no waiver.
- Two proposals sharing capacity: reserve the first before checking the next;
  opposing currency legs do not automatically net away exposure.
- Changed price/quantity/account state: new eligibility check. Changed risk
  fraction/capital/strategy: explicitly approved version and transition first.
- Swing: day count/deadline and configured cadence; intraday: exact session close,
  including early closes. Missed checks or gaps follow explicit incident rules.
- Unknown submission or duplicate order ID: reconcile, do not duplicate.
- Gap, outage or loss halt: retain approved exits, no invented fill or larger risk.
- LLM requests a limit change or live order: refuse and record; development
  instructions cannot grant trading authority.
