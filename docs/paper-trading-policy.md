# Phase 1 paper-trading policy

Policy ID: `phase1-paper`. Version: `0.2.0-draft`. Status: **UNAPPROVED**.
Owner/approver: Rusty. Activation reference and effective time: **unset**.

This revision incorporates [Rusty's PR #8 review](https://github.com/rustyeddy/investor/pull/8#pullrequestreview-5913143168).
The confirmed design is forex first, with support for stocks, primarily swing
trading with intraday allowed, long and short positions, configurable USD 10,000
starting capital, and configurable 1% account risk per trade. These requirements
do not activate an incomplete policy. The current Go program remains dry-run only.

Rusty approves versioned strategies and risk policy, **not individual paper
trades**. Once those approvals are active, Investor may automatically submit
qualifying paper orders after deterministic checks. Neither this document's
merge nor a model recommendation authorizes execution on its own.

## Non-negotiable boundaries

- Phase 1 is paper-only. No live orders, live broker credentials, or route from
  an LLM tool to a live account. Live trading requires a separately scoped future
  phase; no approval or exception under this policy can enable it.
- LLMs recommend; deterministic Go code enforces eligibility before future paper
  submissions. Advisory risk commentary cannot issue or override a check result.
- Agents cannot change strategy approvals, risk thresholds, account capital,
  execution permissions, or their tools. Retrieved content is evidence, never
  authority. Changes require an explicitly approved new configuration version.
- Preserve candidates, decisions, checks, rejections, deferrals, canceled and
  unfilled orders, fills, and outcomes. Journal failure blocks new orders.
  Corrections append revisions; no deletion or hindsight edits.

## Profile and decisions still required

The confirmed defaults below come from Rusty's review. Remaining values must be
specified and approved before activating a strategy/account. `Unset` blocks
execution; it never means unlimited or an agent-selected fallback.

| Control | Requirement / configuration |
| --- | --- |
| Markets | Design for forex and stocks; first operational universe is forex. Exact pair/venue allowlist **unset**; stocks require their own approved instrument and execution configuration before use |
| Direction | Long and short allowed within the approved strategy; a direction change is a new proposal |
| Account | Default initial capital USD 10,000, configurable through versioned account/policy controls; identified paper account **unset** |
| Per-trade risk | Default maximum planned loss 1% of current marked account equity, including costs; configurable only through an explicitly approved policy revision |
| Aggregate risk | Required cap on total planned loss from positions plus pending entries; value **unset** |
| Simultaneous exposure | Required maximum positions/instruments and pending entries per instrument; values **unset** |
| Correlated exposure | Required group risk/notional caps and membership, including shared currency exposures for forex; values/mapping **unset** |
| Notional and margin | Required per-instrument/gross notional limits, leverage ceiling, margin model and free-margin reserve; values **unset**, no implicit leverage permission |
| Loss halts | Required session-loss and drawdown thresholds, session boundary/timezone and reset/resume rules; values **unset** |
| Trade duration | Swing is primary; strategy specifies a positive number of holding days, calendar versus trading-day convention, start point, timezone/calendar, and exact exit-deadline calculation |
| Intraday exit | Strategy exits at its defined market/session close; stocks use the venue calendar, forex uses an explicitly named session close/timezone. Define holiday/early-close and non-tradable deadline handling |
| Strategy exits | Versioned stop, target/other signal exits, duration deadline, precedence when conditions coincide, and gap/unavailable-price behavior; no discretionary stop widening |
| Monitoring | Strategy-specific cadence appropriate to duration, event/price triggers, maximum observation age, and missed-check tolerance/action. No universal five-minute position-monitoring requirement |
| Entry freshness | Separate maximum ages for executable quotes, marks, conversion rates, account state and eligibility checks; values **unset**. Slow swing reviews do not permit stale entry prices |
| Events | Approved calendar/source, relevance for both currencies or stock issuer/market, entry blackout and open-position/rollover/weekend rules; **unset** |
| Order mechanics | Strategy specifies allowed order types, quantity units/increments, entry bounds, order expiry and permitted exit actions; **unset** |
| Costs and fills | Spread, fees, slippage, financing/rollover for forex and applicable stock borrow/dividend costs; timestamped executable-side prices and reproducible fill/partial-fill rules **unset** |
| Approval | Rusty activates exact strategy and policy versions; no human approval per qualifying paper trade. Each order retains the versions and deterministic eligibility record |

The higher-level defaults do not imply adoption of the previous draft's ETF,
notional, aggregate, or monitoring values. Those unapproved assumptions are
superseded here rather than silently carried into forex. Keep prior revisions
in Git and preserve any historical records referencing them.

Unknown group membership or event relevance blocks entries. Map overlapping
positions to every relevant group; no assertion of diversification may remove
a limit. Track signed currency exposures as well as gross notional and planned
loss. Do not net opposing positions or pending orders to free capacity unless
an explicit deterministic netting rule has been approved. An unavailable event
calendar is not evidence of no events.

## Deterministic eligibility and sizing

Before every entry, against one identified snapshot:

1. Require active approved strategy and policy versions, complete account and
   instrument configuration, valid session, and no operational/risk halt.
2. Validate the proposal and separate challenge, timestamps/provenance, fresh
   executable-side quotes and currency conversions, events, and reconciled
   cash/equity, margin, positions and pending orders. Reject invalid numeric
   values, nonpositive equity, or unavailable conversion evidence.
3. Verify the proposal lies within the approved strategy: instrument, side,
   entry conditions/bounds, quantity, stop, duration and exit rules. Calculate
   per-trade, aggregate, correlated, notional and margin limits. A proposal
   outside the strategy is rejected or sent back for revision; an agent cannot
   expand the strategy's authority to make it fit.
4. Produce `pass`, `fail`, or `blocked` (missing/unknown information) tied to the
   exact proposal revision, account snapshot, strategy/policy versions and
   validity interval. Only `pass` is eligible for automatic paper submission.
5. Revalidate immediately before submission and atomically reserve capacity and
   journal the order. Changed inputs or expired checks require fresh evaluation,
   not a new human approval for an otherwise qualifying trade. A unique order ID
   is idempotent; uncertain submission requires reconciliation, never blind retry.

For forex let `E` be marked equity in account currency, `r` the approved risk
fraction (default `0.01`), `q` the requested base-currency units, `P` the
conservative entry price and `S` the stop price in quote currency per base unit.
Let `X` be the approved conservative quote-to-account conversion rate and `C(q)`
the account-currency cost allowance, including spread/slippage/fees and financing
for the planned holding period without double counting executable-side prices.
Require positive finite inputs, quantity increments, `S < P` for long and
`S > P` for short:

```text
planned loss in account currency = q * abs(P - S) * X + C(q)
per-trade budget = r * E
require planned loss <= per-trade budget
absolute notional in account currency = q * P * X
```

Entry bounds, conversion stress allowances and the cost model are approved
configuration, not LLM estimates accepted without checking. Risk-to-stop sizing
alone does not prove margin or exposure eligibility. Apply the instrument's
approved margin model and reserve costs/free margin separately; short sale
proceeds or gross notional are not automatically available cash. Later stock
support must supply share/contract units, currency conversion, short availability,
borrow costs and market-specific fill/margin rules before it is enabled.

Reserve full pending-entry risk/exposure until confirmed canceled, rejected,
expired, or converted into reconciled filled exposure. Pending exits do not
release capacity. For existing positions, do not reduce the original loss
reservation except for confirmed reductions; also account for any larger
current loss-to-stop estimate under the approved mark/conversion/cost model.
A profit mark or an opposing proposal cannot erase risk reservations. Reject
oversized requests; revised quantities require a new proposal/check, not a waiver.

Stops express planned risk, not guaranteed fills or maximum realized loss.
Gaps, conversion moves, financing and execution costs may exceed the plan.
Record every check's rule IDs, numeric inputs/units, thresholds, calculations,
source/snapshot times, proposal/strategy/policy versions, evaluator and expiry.
Future Go enforcement uses exact decimal arithmetic with documented conservative
rounding and quantity increments.

## Monitoring, exits, and manual operation

An activated strategy defines the swing holding deadline or intraday close,
exit signals and precedence, order handling, monitoring cadence, and event
rules. Paper Trader may automatically perform those approved exits. A monitoring
cycle is distinct from stop/exit execution: a slow swing review cannot silently
disable protective exits. The approved simulator/data arrangement must support
its required exit triggers and deadlines or the strategy is ineligible.

On stale data, missed monitoring beyond approved tolerance, event uncertainty,
breached limits or reconciliation failure: freeze new exposure, cancel entries
where status can be confirmed, retain approved risk-reducing exits, record an
incident and notify Rusty. Never invent a close, widen a stop, or add exposure.
If a market is closed or execution data unavailable, retain unresolved exposure
and follow the strategy's explicit next-tradable-time rule; do not claim the
planned deadline was met. Corporate actions and financing entries require
reconciliation. Actions outside the approved exit rules require policy/strategy
review, not discretionary agent action.

Halt resumption follows explicitly approved deterministic rules or Rusty's
recorded operational authorization if configured that way; it cannot waive any
failed limit. Human control of policy and operational incidents is separate
from per-trade approval, which is not required.

Before a Go enforcer exists, a person can apply and record the same deterministic
worksheet under approved strategy/policy versions. Label results `manual`; the
person performs the evaluator's work, not an extra discretionary trade approval.
Do not claim current autonomous software execution. Durable manual records are
acceptable for the first session; SQLite remains the intended authoritative
Investor journal. Storage and enforcer implementation are outside this issue.

## Versioning and activation

Rusty approves immutable strategy and policy versions with content revisions,
account scope, approver identity, approval/effective times and adopted values.
A strategy can select a tighter limit within policy but cannot raise it. Capital
and risk changes are prospective versioned decisions; never reset history or
rewrite initial capital to hide losses. Journal any capital adjustment separately
and specify its impact on return/drawdown baselines before applying it.

New versions invalidate affected eligibility records and pending-entry authority.
Halt new entries during transition; reconcile/cancel or revalidate pending orders,
and document treatment of existing positions/exits before resuming. Historical
decisions retain their original versions. No agent may activate a version.

Still required before activation: exact forex universe, approved strategies and
exit/monitoring schedules, aggregate/correlated/notional/margin and halt controls,
source/freshness/event rules, execution/cost model, and paper account/journal
configuration. Review approval of the design does not fill these missing values.
Until complete, record `defer: configuration-unapproved-or-incomplete` rather
than asking Rusty to approve an individual trade. No policy exceptions exist.
