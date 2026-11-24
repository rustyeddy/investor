# Research journal and evidence contract

> **Superseded in part (Trader ADR-093; #39, #40).** Investor sizes nothing,
> reserves nothing and submits no orders; Trader is the only risk authority. Where
> this document describes Investor-side sizing, risk admission, reservations, paper
> orders or fills, follow [the paper-trading policy](paper-trading-policy.md) instead. Do not implement
> those parts.

Status: design for Phase 1. An append-only JSON-file journal records runs, steps and model calls (see "Phase 1 persistence and export"); paper execution is not implemented.
The current program remains dry-run only. This contract belongs to Investor;
Trader and Tulip may consume exports or future APIs but are not authoritative
stores for Investor workflow records.

The journal preserves what was known, when it was known, what an agent or
deterministic evaluator concluded, and what happened next. Records are append
only. A correction or changed conclusion creates a new revision linked to the
superseded record; it never rewrites history. A failed journal write blocks any
new external side effect, especially order submission.

See [operational workflows](workflows.md) for lifecycle behavior,
[manual research session](manual-research-session.md) for a working record
example, and [paper policy](paper-trading-policy.md) for the unapproved
execution constraints. Nothing in this document activates a policy, strategy,
fill model, or order.

## Identity and common evidence fields

`id` is the stable, globally unique logical object ID (UUID recommended),
`revision` is its increasing revision number (starting at 1), and `revision_id`
is a globally unique immutable ID for that exact revision. Every revision also
has `kind`, `schema_version`, `created_at`, and `recorded_at`. Re-delivery of
the same revision preserves both IDs; a correction keeps `id`, increments
`revision`, and receives a new `revision_id`. It points to the prior
`supersedes_revision_id`. IDs are never derived from mutable content. Relations
that must reproduce a decision's original inputs reference `revision_id`, not
just the logical `id`; a logical ID may be used when intentionally following
the latest revision. Parent/child links are explicit and validated on write.

All records that represent a run, decision, market fact, order, fill, position,
or outcome also carry the applicable subset of:

| Field | Meaning |
| --- | --- |
| `occurred_at` | Time the represented event happened, if known |
| `available_at` | Earliest time the system/operator could use the information |
| `as_of` | Decision or measurement cutoff; distinct from creation time |
| `source_id`, `source_revision` | Source artifact identity and immutable revision |
| `source_event_id` | Upstream event identity for deduplication, if supplied |
| `instrument_id`, `venue` | Canonical instrument and market context, when relevant |
| `price_side`, `currency`, `units` | Bid/ask/last/mark and unit context; never infer a side |
| `strategy_id`, `strategy_version` | Strategy identity at decision time, or explicit `none` |
| `policy_id`, `policy_version` | Policy identity at decision time, or explicit `none` |
| `agent_role`, `agent_id` | Responsible role and agent execution, or `human` for manual work |
| `model_provider`, `model_id`, `model_version` | Model identity when used; unknown stays unknown |
| `prompt_id`, `prompt_version`, `skill_id`, `skill_version` | Exact instruction artifacts used |
| `input_ids`, `output_ids` | Exact evidence and produced records for an agent step |
| `decision`, `rationale`, `rule_result_ids` | Conclusion, reasons, and deterministic checks where applicable |

Use UTC RFC 3339 timestamps with explicit offsets. Preserve source-provided time,
publication time, retrieval/availability time, and known delay separately.
Unknown values are null with an explanatory missing-evidence reason, not a
fabricated timestamp, price, identity, or default. Store decimal quantities and
prices as exact decimal text plus declared units/scale; do not use binary float
as the authoritative ledger value. Store original source payloads or a
content-addressed immutable snapshot and its digest, subject to source terms.

## Record catalog and links

| Record | Minimum content and links |
| --- | --- |
| `research_run` | Objective, universe, horizon, cutoff, owner, trigger, workflow/version, timezone, status and activation references. Parent of run steps and candidate artifacts. |
| `run_step` | Run ID, stable step ID, role, attempt number, inputs/outputs, start/end, status, error, next action, model/prompt/skill versions. Each retry is a separate record. |
| `hypothesis` | Thesis, falsifier, scope, horizon, author, exploratory/registered classification, optional registered strategy/version and evidence links. Revisions preserve prior text. |
| `observation` | Instrument, observed value(s), price side/spread where relevant, source snapshot/event, event/publication/available/as-of times, units/currency and quality/freshness. |
| `opportunity` | Candidate identity and revision, hypothesis ID, instrument/direction, proposal references, screening result, and retained include/exclude rationale. Rejections remain in the candidate population. |
| `agent_assessment` | Opportunity or run-step ID, role, assessment and rationale, evidence/input IDs, recommendation, output IDs, and exact runtime/instruction versions. Advisory text does not become a deterministic result. |
| `trade_proposal` | Opportunity/hypothesis ID, immutable entry/stop/target-or-alternative-exit terms, direction, quantity/units, expiry, strategy reference, assumptions and supporting evidence. A revised term set is a new proposal revision. |
| `risk_decision` | Opportunity/proposal revision, PASS/WATCH/REJECT/INSUFFICIENT_DATA, as-of/validity, reasons, evidence, activation versions, evaluator identity, rule results, next owner/trigger. Preserve every evaluation revision. |
| `rule_result` | Decision ID, rule ID/version, pass/fail/blocked, exact inputs/units, threshold, calculation/result, evidence IDs and reason. Manual evaluation is explicitly marked `manual`. |
| `reservation` | Account/order/opportunity IDs, reserved risk and exposure by currency/group, source account snapshot, creation/release/transfer times and amounts, status, and linked events. Capacity change is atomic with order/lifecycle journal writes. |
| `paper_order` | Stable order ID, account, exact PASS/check/proposal IDs, side, instrument, quantity, order type/limits, immutable stop/exit plan, expiry, fill-model version, strategy/policy versions and submission state. |
| `order_event` | Order ID, monotonically ordered event ID/sequence, prior/new state, event/available/recorded times, reason, evidence and reservation delta. Covers proposed, pending, partial, filled, rejected, canceled, expired and reconciliation outcomes. |
| `fill` | Unique fill ID, order/position ID, entry/exit role, side, quantity, exact price and side, event/availability times, source snapshot/event, model/version, fees, spread/slippage/financing and currency conversion evidence. |
| `position` | Stable position ID, account/instrument/side, opening order and fill links, open/closed projection, filled and remaining quantity, entry basis, exit plan/deadline, and linked incidents. Derived status can be rebuilt from fills and lifecycle events. |
| `completed_trade` | Stable trade ID linking one position to its entry and all exit fills, quantities, cash flows, costs, financing, conversion records, realized P&L and R calculation. Exists only for fully closed quantity; partial exits remain open exposure. |
| `incident` | Stable ID, scope (run/order/position/account), detected time, severity, description, evidence, halt/resolution status, owner, actions and resolution links. Includes journal/storage and reconciliation failures. |
| `weekly_review` | Cutoff, timezone, cohort manifest, linked records, counts and calculations, data completeness, known omissions, limitations, conclusions and follow-up assignments. A retry is a new review attempt. |

Relations use IDs, not copied mutable records. For audit readability, a record
may snapshot the version labels and key immutable decision inputs it depended
on. A referentially complete trace is:

```text
research_run → run_step → observation/source snapshot
                         → hypothesis → opportunity → agent_assessment
                         → proposal → risk_decision → rule_result
                         → reservation → paper_order → order_event
                         → fill → position → completed_trade → weekly_review
                         ↘ incident (where needed)
```

An exploratory hypothesis may produce observations, opportunities and
assessments, but is outside registered-strategy performance cohorts. Registration
is a separate immutable record linking a hypothesis to an approved strategy
version and effective time; it does not rewrite the earlier exploratory work.
Paper orders require that registered, active strategy and policy references and
a passing deterministic decision. Exploratory results, historical backtests,
counterfactuals and paper fills have distinct record kinds/cohort labels and
must never be combined in one paper-trade performance total.

## Lifecycle and invariants

Run status is `running`, `completed`, `blocked`, `failed`, or `cancelled`.
Each step attempt reaches an explicit outcome; retry means a new attempt. A run
can finish with no opportunities or orders. Candidate disposition is retained
as `included`, `excluded`, or `needs_review` with reasons; do not delete a
rejected candidate from the research denominator.

Risk decisions are immutable records with one of the operational outcomes:
`PASS`, `WATCH`, `REJECT`, or `INSUFFICIENT_DATA`. WATCH means a specified entry
condition is unmet; INSUFFICIENT_DATA means required information/configuration or
evaluation is missing. A known disqualifying fact takes precedence over a data
gap while both reasons remain recorded. A changed proposal or re-evaluation
creates a new linked decision. Only an unexpired PASS tied to exact activated
versions can make an order eligible.

Proposal acceptance is represented by a PASS risk decision on that exact
proposal revision; it is not a fill or completed trade. REJECT retains a
rejected proposal and its reasons. WATCH and INSUFFICIENT_DATA proposals remain
unaccepted with their explicit next action. An accepted proposal with no order
or no confirmed fill is counted as unfilled at the relevant review cutoff, not
as a trade.

Order state is separate from position state. Expected order transitions are:

```text
proposed → pending → partially_filled → filled
                   ↘ rejected | canceled | expired
```

`partially_filled` is an order state, not a position state: the confirmed entry
quantity opens a position immediately, while the unfilled remainder stays
pending and reserved. Cancel/expiry releases only the remainder. An order with
no fill that is canceled, expired, rejected, or still pending at a review cutoff
is explicitly unfilled; it creates no position or completed trade. Each fill is
unique and immutable, and must be reproducible from the approved, versioned
deterministic fill/cost rules and recorded quote/source inputs defined by the
[paper workflow](workflows.md#deterministic-reference-fill-and-exit-model).
Store the model version, event ordering, price side, slippage/rounding, fees,
financing and conversion evidence with every calculation. Position is `open`
until confirmed exit fills reduce its
quantity to zero, then `closed`; a partial exit does not close it. Corrections
or late fill reports append reconciliation events and may update the derived
projection, never erase prior events. Reservation and corresponding order/fill
transition commit together. Uncertain submission/cancellation retains the
reservation until reconciled; never blindly retry with a new ID.

`completed_trade` is an accounting view of a closed position, not a model
assessment. It sums all linked entry/exit cash flows and explicitly itemized
fees, spread/slippage, financing/borrow, and currency conversion. Partial
realized exits may be reported separately while the position remains open; they
must not be reported as a completed trade. Unrealized P&L is a timestamped mark
on an open position and is not realized P&L.

For each completed trade, report net realized account-currency P&L and R:

```text
net P&L = signed entry/exit cash flows + converted income − fees − financing
R = net P&L / original approved planned-loss amount in account currency
```

The exact cash-flow sign convention, conversion source/time, rounding, fees and
planned-loss basis are stored with the calculation and model versions. If any
required fill, cost, conversion or initial risk evidence is missing or ambiguous,
mark the affected value `incomplete`/unknown with a reason. Do not substitute
zero, a midpoint, a later price, or an assumed exchange rate. Missing exit price
means unresolved/open exposure, not a completed trade. A performance aggregate
states its included population and omitted/incomplete count.

## Phase 1 persistence and export

**Current implementation (issue #14).** While the schema is still changing,
Investor's authoritative journal is a directory of immutable JSON files behind the
narrow `journal.Journal` interface (`internal/journal`; set `journal_dir` in the config
file or pass `run --journal DIR`; the model runtime requires one). Each record revision is one file,
`records/<kind>/<id>/<revision>.json`, written to a synced temporary file and
hard-linked into place so an existing revision is never overwritten; the same
file is also linked at `revisions/<revision_id>.json`, which makes revision IDs
globally unique. Re-delivering an identical record is a no-op and a conflicting
one is an error. `journal.json` holds the journal format version, and an
unsupported version is refused. Files are plain, indented JSON with UTC RFC 3339
nanosecond timestamps, safe to read and diff but not to edit. Recorded kinds so far:
`run` (revision 1 at start, revision 2 with the outcome, so a run whose process
died stays visible as `running`), `step` (one per agent step, including failures
and stops, with SHA-256 digests of the skill instructions used) and `model_call`
(one per provider attempt with provider, model, token usage, cost and error). A
failed journal write fails the run and runs no further agents. Order, fill and
reservation records are deferred to the paper-trading issue.

Known limitations of the file journal: it assumes a single writer process (two
processes appending to the same directory are not coordinated beyond the
no-overwrite guarantee on each revision); it needs a filesystem that supports hard
links (local Linux and macOS filesystems do; some network and FAT filesystems do
not); there is no backup, retention or export tooling yet; and journaling is
required only for the model runtime, so dry-run workflows are recorded only when a
journal is configured.

**Intended end state.** Move to SQLite once the schema has settled; the interface
and record semantics above are meant to carry over unchanged. Use SQLite as
Investor's authoritative journal then. Keep the first schema
relational and small: a `records` table for common identity/revision/timestamps
and immutable payload, plus indexed typed tables (or JSON payload columns with
validated Go structs) for runs, evidence, decisions, reservations, orders,
events, fills, positions, trades, incidents and reviews. Enforce globally unique
`revision_id`s, unique `(kind, id, revision)` tuples, source event/fill IDs where
scoped, foreign keys to exact referenced `revision_id`s, valid transitions, and
atomic reservation/order/fill updates. Use explicit schema migrations and a
journal schema version; never mutate old payloads to
match a new Go struct. This is ordinary durable records plus append-only
lifecycle events, not a general event-sourcing platform.

Write each logical transition in one database transaction. A durable commit is
required before reporting success or performing an external side effect. On
startup/recovery, reconstruct position and reservation projections from stored
records and reconcile pending/uncertain orders before permitting new entries.
Backups, retention, privacy controls and filesystem placement must be specified
before operational deployment; they do not change record semantics.

Provide deterministic export from the same records to:

- Org tables/sections for operator-readable sessions, evidence and reviews;
- CSV files with stable IDs, UTC timestamps, declared units/currencies and
  explicit nulls for spreadsheet analysis;
- a versioned future Trader adapter for market snapshots and research/backtest
  artifacts, preserving provenance and distinguishing historical results from
  forward paper activity.

An export contains a schema version and cohort/time filters and does not become
an alternate source of truth. Reimport must preserve IDs and revisions or be
rejected as a conflicting duplicate. No persistence code or Trader dependency is
introduced by this design issue.

## Example trace

The synthetic `demo-*` records in the [manual walkthrough](manual-research-session.md)
illustrate an observation and proposal that end with
`INSUFFICIENT_DATA: configuration-unapproved-or-incomplete`. Their stable IDs
link source evidence to an assessment and decision; because no eligible PASS
exists, the trace correctly contains no reservation, order, fill, position, or
completed trade. If future evidence completes configuration, a new proposal and
decision revision would be linked to the prior work rather than changing that
historical outcome.
