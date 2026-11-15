# Manual Phase 1 research session

Use the [role contracts](../roles/README.md) and
[versioned paper policy](paper-trading-policy.md). The goal is one traceable
opportunity-to-decision session without Trader, a model API, or new Go code.
An honest reject/defer decision completes the research workflow; an order and
fill are separate outcomes requiring their own evidence and authority.

## Session record

Create a durable folder or notebook with a unique session ID. Use this manifest
as the checklist; each artifact carries the shared metadata from the role
contracts. Keep snapshots alongside references so links alone are not evidence.

| Record | Owner | Required contents / next handoff |
| --- | --- | --- |
| Session brief | Research Director | Objective, universe, horizon, cutoff, policy status, participants → Market Analyst |
| Observations/candidates | Market Analyst | Prices, events, sources, timestamps, candidate hypothesis, gaps → Researcher |
| Proposal | Strategy Researcher | Thesis, falsifier, entry/stop/exit, quantity, price bound, expiry, costs → Challenger pass |
| Challenge | Strategy Researcher/Challenger | Counterevidence, alternative explanations, recommendation, reviewer identity → Risk Officer |
| Risk memo/check | Risk Officer + manual evaluator | Advisory memo separately from every rule's inputs/results → Rusty |
| Approval or refusal | Rusty | Exact proposal/policy revisions, account, order envelope, validity, signature/identity and time → Paper Trader |
| Paper decision/lifecycle | Paper Trader | Order/reject/defer reason; order ID, fills/unfilled status, monitoring and incidents → Performance Analyst |
| Session/weekly review | Director / Performance Analyst | Full candidate denominator, outcomes, missing data, lessons and follow-up questions |

Keep the approval slot explicitly `not requested`, `pending`, `refused`, or
`approved`; silence is never approval. If blocked, record the missing input,
responsible next role, and next review time. Retain all artifacts even when the
workflow stops early. A revised proposal repeats challenge, checks, and approval.

## Daily research and proposal evaluation

1. Record the brief and policy version/status. With an unapproved policy,
   explicitly label research exploratory and prohibit order submission.
2. Collect current observations and relevant event evidence from identified
   sources. Record event/publication/retrieval times and delay separately.
   Separate observed facts from the opportunity hypothesis.
3. Write a proposal and a separate challenge. Missing data produces a request
   for evidence or a defer decision, not invented analysis.
4. Attach the advisory risk memo and a rule-by-rule manual worksheet. Check
   pending orders as well as positions. Mark missing inputs `blocked` and
   breached limits `fail`; neither can become `pass` through LLM reasoning.
5. If every check passes under an approved policy, request Rusty's approval of
   the exact proposal. Otherwise record reject/defer and the reasons. Preserve
   the candidate even if it is unattractive or never reaches approval.

## Paper execution and monitoring

6. Before any paper entry, revalidate the policy, approval, evidence, and
   portfolio state; record the reservation and a unique order ID. A changed
   proposal/state or expired approval goes back for checks and approval.
7. Record submission separately from fills. Apply only the approved simulator
   or manual fill model to subsequent observations, preserving partial fills,
   unfilled remainders, costs, and timestamps. Never fill retrospectively using
   information unavailable at the decision time.
8. Monitor the approved stop, exit conditions, holding deadline, loss limits,
   and events at the cadence adopted with the policy. Cancel expired entries;
   reconcile before retries. On unavailable data or ambiguous state, freeze
   new exposure, record an incident, and follow the policy's exit rules.
9. Append the final paper decision and current position/order status. If no
   order is eligible, explicitly record zero orders and the reject/defer reason.
   Do not turn an unresolved position into a completed trade for reporting.

## Weekly performance review

Reconcile the ledger first. Report the number of observed candidates, proposals,
rejections, deferrals, submitted/unfilled orders, and closed/open positions.
Calculate realized and unrealized P&L separately with costs, mark sources/times,
and missing-data flags. Review exposure, drawdown, policy deviations, challenges
that changed decisions, and model/instruction versions. Avoid profitability
claims from an incomplete or small sample. Director receives new research
questions; proposed policy changes go to Rusty and never activate themselves.

## Worked decision: complete research, no authorized order

This is a **synthetic worksheet fixture**, not current market evidence, a real
instrument recommendation, an approved account, or a claim that research ran.
All times are fixture times in UTC on `2026-11-16`. The symbol `ETF-DEMO` and
source `fixture:opportunity-001` are fictional. Set model identity to `human`
and policy to `phase1-paper/0.1.0-draft` throughout.

| Artifact | Example contents |
| --- | --- |
| `demo-brief-1` at 15:00 | Director asks whether a proposed intraday breakout merits a paper entry |
| `demo-observation-1` at 15:01 | Synthetic quote 100, market time 15:00:30, retrieved 15:00:31; retained fixture; event source and group mapping unavailable |
| `demo-proposal-1` at 15:02 | Buy 100 whole shares at no more than 100, stop 98, exit by session deadline; hypothesized continuation; below-stop move falsifies thesis; hypothetical round-trip allowance 10 USD; expiry 15:07 |
| `demo-challenge-1` at 15:03 | Same human in separate challenger pass: breakout may fail; event risk and quote/fill assumptions unverified; recommend defer |
| `demo-risk-1` at 15:04 | Advisory memo flags gaps; worksheet below; no independent approval claimed |
| `demo-decision-1` at 15:04 | `defer: policy-unapproved, universe-unset, event-evidence-missing, execution-model-unset`; approval not requested; orders 0; fills 0 |
| `demo-review-1` at 15:05 | One candidate/proposal/defer, no trades, realized P&L not applicable; request policy decisions and missing evidence before a new proposal |

For arithmetic illustration only, use hypothetical equity and unreserved cash
of USD 100,000 with no positions or pending entries:

```text
planned loss = 100 * (100 - 98) + 10 = 210 USD
draft per-proposal threshold = 0.25% * 100,000 = 250 USD
notional = 100 * 100 = 10,000 USD = 10% of equity
draft gross threshold = 30,000 USD
draft aggregate planned-loss threshold = 750 USD
```

These arithmetic comparisons pass the illustrative thresholds. The overall
check remains **blocked**: policy approval, exact allowlist, group/event evidence,
cost/fill rules, approved session details, and execution authorization are absent.
Numbers that fit a draft cannot authorize an order. Preserve the worksheet and
defer decision; do not manufacture the missing fields. After Rusty adopts a
complete policy, a new session can follow the same checklist through approval,
paper submission, actual simulated fill evidence, monitoring, and review.

## Review scenarios

Before implementing these contracts, walk through:

- Complete evidence and adopted policy: manual checks and exact human approval
  permit a paper order, while a fill still requires subsequent evidence.
- Missing policy/data/event/group input: blocked check and recorded defer.
- Oversized or correlated proposal: failed check, no automatic resizing/waiver.
- Two proposals sharing capacity: reserve the first before evaluating the next.
- Changed price, quantity, policy, or portfolio: invalidate eligibility and
  repeat checks/approval; retain earlier revisions.
- Unknown submission result or duplicate order ID: reconcile, never duplicate.
- Stop gap, outage, or loss-limit breach: halt entries and preserve incident and
  exit records without inventing fills or enlarging risk.
- LLM requests a limit change or live order: refuse and record the attempt;
  Codex development instructions cannot grant trading authority.
