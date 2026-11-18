# Investor — Development Instructions

These instructions apply to all work in this repository. Read [docs/development.org](docs/development.org) before starting GitHub issues, branches, PRs, or reviews.

## Principles

- Do not repeat yourself (DRY): when the same logic appears a second time, factor it into a shared helper. This takes precedence over keeping the package count low; put the helper in the nearest common package before creating a new one.
- Prefer simple, idiomatic Go, few packages, and minimal dependencies.
- Keep Investor independent from Tulip, and use Trader only as described in [Investor and Trader](#investor-and-trader).
- Make the smallest change that satisfies the assigned issue; propose unrelated work separately.
- Preserve research evidence, timestamps, and paper-trading safety boundaries.
- Update tests and documentation when behavior changes.

## Investor and Trader

Trader is Investor's backend for market data, backtesting and trade execution.
Trader owns market data (acquisition, cleaning and canonical format) and trade
execution, paper and live: it emulates orders, stops and fills, except for paper
trading at a broker, where that broker owns the executions. Investor owns
research: running studies, testing and deriving strategies, finding
opportunities, the decisions that follow, and the research journal. Investor does
not implement or second-guess execution mechanics or data acquisition and
cleaning; it uses Trader for them by reading market data, running backtests and
requesting trades.

- **Boundary:** Trader's REST API. Trader's operations and persisted datasets
  cross it: Investor reads data, runs backtests and requests trades only through
  it. Never read Trader's files or databases, or call a broker directly.
- **Code reuse:** Investor may import Trader's dependency-free leaf packages, as
  shared value types only:
  `github.com/rustyeddy/trader/num` (exact decimals) and
  `github.com/rustyeddy/trader/marketdata` (bar model). Do not import any other
  Trader package (engine, broker, orders, account, storage, services) without
  Rusty's approval; adding one to this list is an AGENTS.md change. Records Investor
  persists, such as the journal, stay Investor-owned, versioned formats, not Trader
  types.
- **Testability:** Investor must build and pass its tests with no running Trader,
  network or credentials. Put Trader access behind small Investor-defined
  interfaces with fixture or mock adapters.
- **Needs from Trader:** when Trader lacks data, an endpoint or a field, open a
  Trader issue (or ask Rusty) instead of duplicating Trader's logic or working
  around it. Changes to Trader follow Trader's own rules and review process.
- **Provenance:** record the Trader data a decision used (source, fingerprint,
  build and availability times, gaps) so the decision can be reproduced.
- **Paper trading:** needs no human approval, per order or to enable it. A paper
  order may be requested automatically when it is made under strategy and
  risk-policy versions activated as the paper-trading policy requires, passes the
  deterministic Go risk checks, and is durably journaled before it is sent; a
  failed journal write blocks the order.
- **Live trading with real money:** disabled until a separate issue and Rusty's
  explicit authorization, with its own approval policy, say otherwise. No live
  order path may exist before then.
- **Credentials and policy:** Investor holds no broker credentials; Trader does.
  Never silently change trading risk policy.

## Git and review rules

- Start from an issue with explicit acceptance criteria; ask if the scope is unclear.
- Create a dedicated branch from current `main`. Never commit directly to `main`.
- Open a focused PR linked to the issue. Do not merge your own PR without Rusty's explicit approval.
- Run `go test -race ./...` and `go vet ./...` for Go changes; report failures and any checks not run.
- Review correctness, simplicity, boundaries, tests, and documentation. Address feedback before requesting approval.
- Do not force-push `main`, bypass protected workflows, or silently change trading risk policy.
- If permissions or required approval are missing, stop and ask.

## Pull Request Completion

- Every PR must reference its related GitHub issue.
- Use `Closes #N` in the PR description when the PR
  fully resolves the issue.
- Use `Refs #N` for partial work.
- Merge only after required reviews and tests pass.
- GitHub automatically closes linked issues on merge.
- Enable automatic deletion of merged feature branches
  in repository settings.
- Never automatically merge without required approval.

## Documentation ownership

- `README.md`: concise purpose, current capabilities, and development phases.
- `docs/development.org`: detailed issue → branch → PR → review → merge procedure.
- `roles/` and `skills/`: Investor trading-agent behavior, not Codex development rules.
- `skills/<name>/SKILL.md`: standard on-disk skill layout.
