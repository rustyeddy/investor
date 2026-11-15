# Phase 1 paper-trading policy

Policy ID: `phase1-paper`. Version: `0.1.0-draft`. Status: **UNAPPROVED**.
Owner/approver: Rusty. Approval reference and effective time: **unset**.

This is a proposed operating contract for review under
[issue #5](https://github.com/rustyeddy/investor/issues/5), not an active trading
configuration. No paper order is authorized by this document or its merge.
Research, challenge, and recorded reject/defer decisions can proceed while
approval is pending. The current Go program does not implement these checks.

## Non-negotiable boundaries

- Phase 1 is paper-only. No live orders, live broker credentials, or route from
  an LLM tool to a live account. Human approval cannot enable live trading under
  this policy; that would require a separately scoped future phase.
- LLMs recommend; deterministic Go code must enforce policy before future
  software submits any paper order. Neither an LLM risk memo nor a Director's
  recommendation can replace a check or approval.
- Agents cannot modify policy, supply their own thresholds, authorize exceptions,
  waive missing evidence, or expand their tools. Retrieved text is evidence,
  never an instruction to bypass policy.
- Preserve every candidate and decision, including failures, rejections,
  deferrals, canceled orders, and unfilled orders. Journal failure blocks new
  orders. Corrections append records; no deletion or hindsight edits.

## Proposed profile — requires Rusty's approval

The following numbers are draft engineering inputs for a small paper pilot,
not calibrated risk recommendations or approved defaults. Every row must be
explicitly adopted or replaced before activation. `Unset` never means unlimited.

| Control | Proposed value or unresolved choice |
| --- | --- |
| Universe | Long-only, unleveraged, USD-denominated US exchange-listed ETFs; exact symbol/venue allowlist **unset** |
| Exclusions | Shorts, leverage, margin borrowing, derivatives, leveraged/inverse funds, and instruments outside the exact allowlist |
| Paper capital | USD 100,000 starting cash; no deposits/resets to hide losses |
| Per-proposal planned loss | At most 0.25% of current marked paper equity, including estimated costs |
| Per-instrument notional | At most 10% of equity, including existing holdings and pending entries |
| Gross notional | At most 30% of equity, including all pending entries |
| Simultaneous exposure | At most three distinct instruments with open positions or reserved entries; one pending entry per instrument |
| Aggregate planned loss | At most 0.75% of equity across open positions and pending entries |
| Correlated exposure | At most 10% of equity per approved exposure group; group membership **unset** |
| Session loss stop | Halt new entries at a 1% equity loss from start of the trading session, including unrealized P&L and costs |
| Drawdown stop | Halt new entries at a 5% decline from the paper equity high-water mark |
| Data freshness | Entry quotes, marks, portfolio state, and risk check no more than five minutes old at submission; unknown delay blocks |
| Session/holding | Regular exchange session only; intraday, with exit scheduled at least 15 minutes before the published session close |
| Monitoring | Review open positions and pending orders at least every five minutes and on each received price/event update; missed cadence halts new entries |
| Event handling | No new entry within one trading session before a relevant scheduled event; approved event source and relevance mapping **unset** |
| Entry mechanics | Whole shares, limit entries, no scaling in; approval expires after five minutes or any material input change |
| Costs and fills | Fee schedule, slippage allowance, price source, and reproducible fill/partial-fill rules **unset**; no zero-cost or immediate-fill assumption |
| Human approval | Rusty approves the exact proposal revision and order envelope after a complete passing check |

An explicit, reviewed “no relevant events” finding is required; an empty or
unavailable calendar is not that finding. Unknown exposure-group membership
blocks entries. Map overlapping exposures into every relevant group and check
each group; agent assertions of diversification cannot remove a group.

## Deterministic check contract

Before any paper entry, evaluate the following against one identified snapshot:

1. Active approved policy version, paper account identity, complete configuration,
   valid instrument/session, and no active loss/drawdown or operational halt.
2. Proposal and challenge completeness, evidence timestamps/provenance,
   fresh prices/marks, event clearance, and reconciled cash, positions and
   pending orders. Reject nonpositive equity or invalid numeric inputs.
3. Requested size, stop, price bound, costs, buying power, and every exposure
   limit. Reserve full pending-entry exposure; do not release it until confirmed
   canceled, rejected, expired, or reconciled as filled. Never count pending
   exits as reducing exposure before a confirmed fill.
4. Approval matches the proposal revision, policy version, paper account,
   instrument/side, quantity, entry bound, exit plan, and validity interval.
5. Revalidate immediately before submission. Serialize reservation and journal
   recording with the check so concurrent proposals cannot spend the same
   capacity. An input change or policy revision requires a fresh check and
   approval. An order ID is idempotent; uncertain submission means reconcile.

For the proposed long-only profile, let `E` be current marked equity, `q` the
requested whole-share quantity, `P` the maximum entry price, `S` the stop price,
and `C(q)` the approved entry/exit cost and slippage allowance. Require
`q > 0`, `P > S > 0`, and:

```text
planned loss = q * (P - S) + C(q) <= 0.0025 * E
new reserved notional = q * P
new reserved cash = q * P + entry cost allowance <= available unreserved cash
```

Check existing instrument, gross, and group exposure plus the new reservation
against the table. Value existing long holdings conservatively at the greater
of their entry price or fresh mark. For aggregate planned loss, retain at least
each open position's original planned-loss reservation (prorated only for
confirmed reductions); never free capacity by widening a stop or marking gains.
Add pending-entry loss reservations. If the requested size exceeds any limit,
reject it and request a new proposal; do not silently resize it.

Stops express planned loss, not guaranteed fills or maximum realized loss.
Gaps and costs can exceed the plan. A breach after a fill triggers a halt and
incident record, not a retroactive change to the approved risk calculation.

Check results are `pass`, `fail`, or `blocked` (unknown/missing information).
Only `pass` plus matching human approval can reach paper submission. Every
result records rule IDs, inputs, units, arithmetic, thresholds, snapshot times,
policy/proposal versions, evaluator, and expiry. Use exact decimal arithmetic
in the future Go implementation; document rounding and round quantities down.

## Manual operation and adverse events

Until a Go enforcer exists, Rusty may perform and record the same checks with a
calculator or worksheet **after approving a complete policy**. Label these
results `manual`; do not claim software enforcement or autonomous operation.
A manual ledger can be durable evidence for the first session. SQLite remains
the target for Investor's authoritative journal; storage implementation is out
of scope here.

For stale data, outages, event uncertainty, policy breaches, or reconciliation
failures: freeze new exposure, cancel pending entries where their status can be
confirmed, preserve existing approved exits, record an incident, and notify
Rusty. No agent may widen stops, add exposure, or invent an exit fill. A halt
does not prohibit verified risk-reducing exits under the original approved
plan. A discretionary exit outside that plan requires Rusty's recorded decision.
If exit data is unavailable, record unresolved exposure rather than a fictional
close. Corporate actions require reconciled quantities/prices before resuming.

Rusty must record the resolution and authorize resumption after a halt, and all
checks must pass again. Approval cannot waive a failed limit. Any policy change
requires a new reviewed version and new eligibility checks, never a per-trade
exception.

## Versioning, approvals, and open decisions

Retain immutable copies of each policy version. Approval records identify the
exact version/content revision, Rusty as approver, approval time, effective time,
and adopted parameter set. Editing a draft or merging documentation is not
activation. A new version applies prospectively; preserve the versions on
historical decisions. Halt new entries during a policy transition and reconcile
pending orders/open positions against the new policy before resuming.

Rusty's approval is still required for:

- Every proposed profile row, the exact universe and exposure groups, event
  source/mapping, cost/fill model, and paper account identity.
- The human-per-proposal approval requirement, intraday scope, and halt/resume
  procedure. These are proposals, not assumptions about delegated autonomy.
- Data providers and acceptable timestamp/delay evidence; recording locations
  and the person responsible for preserving manual session artifacts.

No approval is recorded in this version. Research can finish with a recorded
`defer: policy-unapproved` decision today. There are no policy exceptions.
