# Phase 1 research and paper-trading workflows

Status: design specification for [issue #4](https://github.com/rustyeddy/investor/issues/4).
No scheduler, queue, model runtime, simulator or persistent journal is implemented
by this document. The CLI sample remains a dry-run preview. Use the
[role contracts](../roles/README.md), [paper policy](paper-trading-policy.md), and
[manual walkthrough](manual-research-session.md) together.

Phase 1 uses current observations, starts with forex, supports long/short swing
and intraday strategies, and does not depend on Trader. Rusty activates strategy
and risk-policy versions; qualifying paper trades need no individual approval.
The configured USD 10,000 initial capital and 1% per-trade defaults do not replace
the remaining mandatory limits or activate the draft policy.

## Shared execution and evidence contract

Before a future model invocation, Investor supplies the applicable role authority,
activated strategy and policy contents/versions, skill body/version, objective,
input artifact revisions and explicit tool permissions as trusted application
inputs. Missing authority/configuration prevents order eligibility. Markdown
links in skills are not automatically read by the current loader and must not
be mistaken for supplied model context. Retrieved market text stays untrusted
evidence. Deterministic Go checks, not prompt wording, enforce numeric limits.

Each workflow has a session ID, workflow version, configured trigger/timezone,
as-of cutoff, owner and status (`running`, `completed`, `blocked`, `failed`, or
`cancelled`). Each step records attempts and terminal outcome before a handoff.
Persist source snapshots, event/publication/retrieval times, known delays,
input/output IDs and revisions, model/provider/skill versions, evaluator identity,
reasons and next action. Preserve `unknown` rather than inventing evidence.
Use the common role artifact contract for all records. Manual append-only files
or worksheets suffice now; Investor's eventual authoritative journal is SQLite.

An unavailable downstream step does not erase completed work. Append corrections
and superseding revisions; do not overwrite decisions, silently skip failed
agents, or filter rejected candidates out of the research population. A failed
journal write stops side effects. The manual evaluator performs the future Go
checker's arithmetic and records `manual`, not an extra trade-approval gate.

## 1. Morning market review

**Trigger:** operator starts a session at the configured research time, or
requests an ad hoc refresh. “Morning” refers to the selected forex session and
timezone, not an assumed US equity open. Scheduling is future implementation.

**Inputs:** research mandate/universe, decision cutoff, available current market
and event sources, strategies/policy and activation status, prior candidates,
paper positions and previous review. Research may continue as exploratory if
configuration is unapproved, but no opportunity can become execution-eligible.

| Step / owner | Action and output | Failure / next handoff |
| --- | --- | --- |
| Brief / Research Director | Fix objective, universe, horizon, cutoff and assigned review roles; persist session brief | Missing scope blocks session; otherwise → Market Analyst |
| Observe / Market Analyst (`market-overview`) | Collect timestamped quotes, calendars and both-currency events; persist observations and source snapshots | Missing/stale/conflicting inputs flag affected instruments; never invent a current quote |
| Screen / Market Analyst (`find-opportunities`) | Retain every screened candidate with hypothesis/falsifier and exclusion reasons | Unsupported or out-of-scope candidate is retained with reason; candidates → Researcher |
| Propose/rank / Strategy Researcher (`analyze-setup`) | Versioned entry/stop/target and exit proposals; ranked shortlist with explicit evidence-based ranking rationale | Gaps marked insufficient; rank expresses research priority, never permission or sizing authority |
| Challenge / assigned Challenger (`challenge-thesis`) | Separate review pass on raw evidence and proposal; record counterevidence, reviewer/model identity and proceed/revise/reject recommendation | Missing/failed challenge blocks PASS; revisions return to Researcher |
| Evaluate / Risk Officer + deterministic evaluator | Advisory memo plus independent rule results and opportunity decision, using workflow 2 | Failed/unknown rules cannot be overridden by ranking or model confidence |
| Report / Research Director | Publish session report linking all candidates, decisions, failures, next review times and eligible proposal IDs | Journal/report failure leaves incomplete session visible; eligible proposals may enter workflow 3 only after durable decision records |

Challenge must be independently assigned as a separate evaluation pass with
access to raw evidence, not merely a rewritten proposal conclusion. The same
model provider may serve both roles; record shared model/person/context and its
limitations, and never describe self-review as independent human approval.
If a required independent pass cannot be performed, return INSUFFICIENT_DATA.
This does not introduce human approval per trade or require six model processes.

**Persisted artifacts:** brief, observations, screened-out candidates, proposals
and rank rationale, challenges, rule results, decisions, attempts and report.
The session can complete with zero eligible opportunities and zero orders.

## 2. Opportunity evaluation

**Trigger:** new/revised proposal or the recorded recheck condition for an
existing WATCH/INSUFFICIENT_DATA decision. **Inputs:** exact proposal/challenge
revisions, current evidence, active strategy/policy, event/group data and a
reconciled account snapshot including pending reservations.

Researcher fixes instrument/venue, long/short, quantity and units, entry condition
and bounds, stop, target, strategy version, cost/conversion model, expiry,
swing-day deadline or intraday close, exit precedence and monitoring rules
before an order is possible. A strategy with no price target must explicitly
record `target: none` and its versioned alternative exit rule, never omit the
field. Changes create a new proposal revision and repeat challenge/eligibility.

Risk Officer prepares inputs and advisory findings; the deterministic evaluator
owns rule outcomes. Director records the decision and next action. Evaluate all
available facts; a known disqualifying violation takes precedence over a data
gap, while preserving both reasons. Otherwise use this order:

| Decision | Meaning | Next action |
| --- | --- | --- |
| REJECT | A supported thesis falsifier, scope violation, or deterministic limit failure disqualifies this revision | Retain it; a changed thesis/quantity/strategy requires a new revision and evaluation |
| INSUFFICIENT_DATA | No conclusive rejection, but mandatory evidence, activated configuration, challenge or check result is missing/stale/failed | Record missing inputs/owner and recheck deadline; no order |
| WATCH | Complete evidence and valid configuration, no disqualifier, but a specified strategy entry condition has not occurred | Record observable trigger and expiry; re-evaluate on new evidence, no reservation/order yet |
| PASS | Entry conditions satisfied, complete challenge/evidence, and all deterministic checks pass under activated versions | Persist decision/check and hand exact revision to Paper Trader; revalidate at submission |

WATCH is not a place to hide missing data or a failed risk check. LLM labels
cannot create PASS. Decision records contain reasons (including all failed and
unknown checks), evidence IDs, rule result IDs, as-of time, validity/expiry and
next owner/trigger. They are immutable snapshots, not ongoing guarantees.

Skill status `ready` means an artifact is complete, not that an opportunity is
PASS. `blocked` commonly maps to INSUFFICIENT_DATA; `rejected` maps to REJECT
when supported. The older manual `defer` label maps to WATCH for a known unmet
trigger or INSUFFICIENT_DATA for missing information; record which, explicitly.

## 3. Paper order and position lifecycle

**Trigger:** a durable PASS decision. **Owner:** Paper Trader, using deterministic
eligibility/reservation, simulator and ledger operations; Risk Officer handles
breaches and Director handles incidents. **Inputs:** exact proposal/check,
activated strategy/policy and fill-model versions, fresh account/price/conversion
state, exit plan and stable order ID. No per-order human approval handoff.

Revalidate before submission and reserve capacity atomically with the journal
record. A changed input invalidates eligibility and requires a new check; no
silent resizing or stop widening. Entry expiry and activation revocation prevent
new fills. The pending-entry evaluator must check current policy, quote, account
and event eligibility before each fill, excluding that order's own reservation
from duplicate counting. If it fails, cancel unfilled exposure and record why.

Order state and position state are separate records:

| Record / transition | Required evidence and effect |
| --- | --- |
| Proposal: proposed → decision | Proposal alone holds no position and implies no submission or fill |
| Order: proposed → pending | Persisted PASS/revalidation, immutable entry/stop/target/strategy, order ID, submission time and capacity reservation |
| Order: pending → filled | Verifiable simulated execution event with source snapshot, event/availability times, fill-model version, side, quantity, price, costs and unique fill ID |
| Order: pending → expired | Expiry reached with no earlier confirmed fill; append terminal event and release only unfilled reservation |
| Order: pending → cancelled | Confirmed cancellation/reconciliation; append reason and release only unfilled reservation |
| Position: absent → open | Confirmed nonzero entry fill; link order/fill and record entry basis, quantity, exits and calculated deadline |
| Position: open → closed | Confirmed exit fills reduce remaining quantity to zero; retain entry/exit evidence, costs/financing and realized P&L |

Failed entry checks produce a recorded rejection, never `filled`. A partial
entry fill opens a position immediately while the remaining order stays pending;
filled/remaining quantities are explicit. Expiry/cancellation ends only the
remainder and does not close the position. Partial exits leave the position open.
Use this extended handling only with an activated model that supports partial
fills; the reference model below is full-fill only.

**Outputs/artifacts:** decision, eligibility snapshot, reservation, order events,
fill evidence, position projection, exit plan/deadline, costs, incidents and
reconciliation records. Failure/timeout never counts as successful submission,
cancellation or execution. A late report of an earlier fill must be reconciled
before releasing reservations or retrying anything.

### Deterministic reference fill and exit model

`quote-paper-v1` below is a proposed, deliberately simple quote-based simulator
contract, not an activated trading model or real liquidity claim. Rusty must
approve its model version, fees/slippage/financing, sources, freshness and
strategy parameters with a complete policy before use. There are no implicit
zero-cost defaults. It does not use Trader or OHLC bars to invent intrabar fills.

1. Accept identified, finite positive bid/ask quotes with `bid <= ask`, source
   event time and recorded availability time. Process a stable recorded event
   sequence; reject out-of-order event times and deduplicate source event IDs.
   A late quote is never inserted into an already decided past. Require an
   event strictly after order submission, available to the evaluator, inside
   the instrument session and before order expiry. Expiry/cancel confirmed at
   the same time takes precedence over a new fill.
2. For a pending long limit entry, candidate fill is `ask + approved slippage`;
   fill only if at or below the fixed limit. For a short limit entry, use
   `bid - approved slippage` at or above the fixed limit. Round prices adversely
   to the configured price increment before testing the bound. Check that fill
   price retains valid stop/target geometry and passes the eligibility rules;
   otherwise cancel the remainder with the disqualifying reason.
3. The model simulates the full remaining entry quantity, explicitly assuming
   availability at that quote; it does not infer order-book liquidity. Commit
   fill, position and reservation transfer together. Unsupported partial-fill
   behavior requires another approved model, never an ad hoc assumption.
4. For an open long, a stop triggers on `bid <= stop` and target on `bid >= target`;
   for a short, stop on `ask >= stop` and target on `ask <= target`. A target of
   `none` disables only the target trigger. Evaluate triggers only after the
   entry fill event, at the earliest eligible new observation. Exit sells use
   bid minus slippage; exit buys use ask plus slippage, adversely rounded. These
   are market-exit prices, not guaranteed stop/target prices; preserve gap losses.
5. For this model, coincident triggers use stop, then target, then approved
   signal exit, then holding deadline. Strategy approval must adopt that
   precedence or select a different versioned model. Signals retain their
   availability evidence; no future signal can explain an earlier exit.
6. Swing deadlines are computed from first fill using the fixed number of
   calendar/trading days and approved calendar/timezone convention. Intraday
   deadlines are the strategy's exact session close, including early closes.
   At a deadline, use an eligible contemporaneous quote; if none is available,
   record a missed deadline and execute at the next permitted tradable quote
   under the approved fallback. Keep the actual later timestamp, never backdate
   a fill or assume a quote was executable after its session closed.
7. Record fees, adverse slippage and financing on their versioned schedule;
   convert P&L using timestamped account-currency rates. Missing evidence blocks
   the affected calculation. If required exit evidence is unavailable, retain
   open exposure, halt new entries and record the incident; do not pretend flat.

No quote, source ID, model version or verifiable calculation means no fill.
Changing levels or the model after seeing subsequent prices is prohibited.

## 4. Position monitoring

**Trigger:** strategy-specific scheduled review, protective price/signal event,
holding deadline, event-calendar update or reconciliation incident. **Inputs:**
open positions, pending orders, immutable exit plans, fresh marks/conversions,
policy state and the prior monitoring checkpoint.

Paper Trader reconciles orders/fills first, evaluates protective exits/deadlines,
then records marks, unrealized P&L, financing and next review time. The
deterministic evaluator checks account/group exposure, loss halts and event
conditions. Risk Officer receives exceptions; Director records unresolved
incidents and the responsible next action. Existing approved exits remain in
force during an LLM outage; model availability is not a protective-exit trigger.

The cadence comes from the activated strategy, not a universal five-minute
timer. A missed review beyond its approved tolerance halts new exposure and
records an incident. A stale mark is labelled stale, not reused as a current
entry check. Positions remain open until evidenced exit fills. Preserve every
monitoring attempt, quote/mark reference, rule result, exit event, next deadline
and incident. Resume follows policy rules, never an agent's discretionary waiver.

## 5. Weekly performance review

**Trigger:** configured weekly cutoff/timezone or operator-requested review.
**Inputs:** full cohort of screened candidates and decisions (including rejected,
WATCH and insufficient-data records), paper orders/fills/positions, marks/costs,
capital adjustments, incidents and all strategy/policy/model versions.

Performance Analyst reconciles the ledger, counts each stage without treating
proposals as trades, and calculates realized and marked unrealized results with
costs and conversion evidence. Compare rejected/watch candidates using a
separately labelled counterfactual study if desired; never add hypothetical
returns to actual paper P&L. Report exposure/drawdown and policy deviations,
challenge effectiveness, missing data and sample limitations. Director assigns
new research questions; Rusty receives proposed configuration changes.

**Outputs/artifacts:** versioned weekly report, cohort manifest, calculations,
marks/source IDs, incomplete metrics, incidents and follow-up assignments.
Missing fills/marks make affected metrics incomplete, not zero. Keep a failed
review and its retry attempt; reporting failure cannot alter an order or activate
policy. Findings do not automatically change strategies or risk limits.

## Failure, retry and recovery rules

Workflow configuration must specify each external read/agent step's timeout,
maximum attempts, retry delay, overall deadline, and evidence-age limits before
operational use. This issue deliberately adds no scheduler/queue or implicit
infinite retries. Manual sessions record the same deadlines and attempts.

| Failure | Required behavior |
| --- | --- |
| Transient read/model timeout | Bounded retry within step/session deadline; retain attempts, error and input versions; discard late output as decision authority |
| Retry exhaustion or failed agent | Mark step failed, dependent opportunities INSUFFICIENT_DATA and session blocked/failed as appropriate; continue independent candidates only with their own complete inputs |
| Invalid output or missing required artifact | Record validation failure; retry only within configured budget; never substitute a successful default |
| Stale/missing observation or changed proposal | Acquire fresh evidence and append revision; rerun affected challenge/checks, never reuse expired PASS |
| Unknown submission/cancellation result | Reconcile by stable order ID; no new order ID or duplicate side effect; hold reservation until reconciled |
| Journal failure | Stop new submissions; recover durable state before resuming. External uncertain actions require reconciliation |
| Restart after interruption | Reconcile open/pending records and deadlines before new work; do not replay side effects from the last prompt |
| User cancellation or policy deactivation | Stop dependent new entries, reconcile/cancel pending exposure and preserve approved exits; record reason and unresolved positions |

Every re-evaluation produces a new decision revision with its reason/trigger.
WATCH and INSUFFICIENT_DATA expire at their recorded recheck deadline; record
expiration without retroactively calling them rejected or filled. Rejected
records remain available for comparison even if a later revision passes.

## Phase 2 example: historical backtest research

The former sample sequence is retained here as a **conceptual Phase 2 example**:

```text
research → backtest → risk → portfolio → operations
```

This is not the default Phase 1 path or a currently runnable backtest. Trigger it
only when a historical hypothesis and Trader integration are available. Research
defines the hypothesis and immutable dataset/as-of range. Backtest requests a
reproducible Trader evaluation (the former `trader.history` and `trader.backtest`
capabilities), retaining strategy/data versions, costs and out-of-sample results.
Risk reviews drawdown and robustness; portfolio proposes constrained allocation;
operations records artifacts and failures. Missing Trader/data or a failed test
blocks that evaluation; it cannot prevent independent Phase 1 forward research.

Historical simulated orders/results remain identified as backtest artifacts;
they are never forward paper fills or authority to bypass workflow 3. Integrating
Trader, restoring a runnable backtest configuration and defining its API belong
to Phase 2, not this documentation change.

## Acceptance walkthroughs

The [manual forex example](manual-research-session.md) demonstrates observation,
proposal, challenge, arithmetic and a durable INSUFFICIENT_DATA decision today.
It must not be labelled PASS while policy/strategy configuration is incomplete.
Before implementation, also verify these scenarios against the contracts above:

- Complete context, unmet entry trigger: WATCH; no capacity reserved or order.
- Complete activated configuration, independent challenge and passing checks:
  PASS → pending order; no position before a verifiable post-submission fill.
- Per-trade pass but group/aggregate breach: REJECT, with all reasons retained.
- Missing challenge/context/conversion: INSUFFICIENT_DATA, never model-issued PASS.
- Entry quote outside limit, pre-submission, expired, duplicate or stale: no fill.
- Short entry uses bid; short stop/exit uses ask; long uses the opposite sides.
- Cancellation/expiry after partial fill: remainder terminates, position stays open.
- Stop gap or missed close: actual later price/time, retained incident and costs.
- Duplicate retry or restart: same order/fill IDs, reconciliation, no double count.
- Weekly review: rejected candidates retained; proposals/unfilled orders excluded
  from realized trade counts, missing marks excluded from confident P&L claims.
