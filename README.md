# Investor

Investor is the Go orchestration layer for an AI-assisted trading research firm. It coordinates agents, skills, permitted tools, workflows, and **the research journal**. The same engine runs from the CLI or a foreground HTTP service.

The journal is central: hypotheses, market observations, opportunities (including rejections), paper orders, decisions, and outcomes must be traceable to their evidence. Without durable records, we cannot tell whether a trading idea works.

The current implementation is **dry-run only**. It validates definitions and exercises agent handoffs; it does not call LLMs, analyze markets, access Trader, or place trades.

## Development phases

1. **Foundation:** define roles, policy, skills (`skills/<name>/SKILL.md`), workflow contracts, and journal records.
2. **Forward research:** connect LLMs and current market data, find opportunities, record paper trades, monitor outcomes, and review performance—**without Trader**.
3. **Trader integration:** add historical data, backtests, studies, and reproducible strategy evaluation through Trader adapters.
4. **Operations:** expand scheduling, recovery, Tulip integration, and controls only as required by working workflows.

No live-money execution is authorized in these phases.

## Run

Requires Go 1.25.1 or newer; no external Go dependencies.

```sh
go build -o bin/investor ./cmd/investor
bin/investor validate
bin/investor plan -workflow research
bin/investor run -workflow research -objective 'Evaluate a trend-following hypothesis'
bin/investor serve -listen 127.0.0.1:8090
```

Each command accepts `-config path/to/firm.json` (default: `examples/firm.json`, relative to the working directory). CLI results are JSON on stdout; logs are on stderr; failures exit nonzero. The foreground service shuts down on SIGINT/SIGTERM.

```sh
curl http://127.0.0.1:8090/api/manager/agents
curl -X POST http://127.0.0.1:8090/api/manager/runs \
  -H 'Content-Type: application/json' \
  -d '{"workflow":"research","objective":"Evaluate a trend-following hypothesis"}'
```

Endpoints: `GET /api/health`, `GET /api/manager/{agents,skills,tools,workflows}`, and `POST /api/manager/runs`. Runs are synchronous with a one-minute request deadline. Persistence, queues, scheduling, authentication, and definition updates are not implemented. The listener defaults to loopback.

## Responsibilities

| Component | Responsibility |
| --- | --- |
| **Investor** | Research agents, model runtimes, skills, tool permissions, workflows, paper-trade coordination, and authoritative journal/audit records |
| [Trader](https://github.com/rustyeddy/trader) | Market data, deterministic analysis, historical backtests/studies, and eventual execution |
| **Tulip** (`../tulip`) | GUI and backend-for-frontend using Investor and Trader APIs |

Trader and Tulip are integration targets, not build dependencies. Investor does not duplicate Trader's engine. Tulip reserves `/api/manager/*`, but its mocks are not yet connected to Investor.

## Agent model

`examples/firm.json` currently defines research, backtest, risk, portfolio, execution, and operations roles. Its sample workflow runs research → backtest → risk → portfolio → operations; this is a **dry-run prototype**, not the Phase 1 forward-paper workflow.

- **Agent:** role, skill references, and tool allowlist.
- **Skill:** reusable instructions; move toward standard `skills/<name>/SKILL.md` files.
- **Tool:** declared capability, separately implemented in Go; example Trader tools are declarations only.
- **Workflow:** ordered agent steps receiving the objective and prior results; the first failure stops the run.
- **Runtime:** `firm.Runtime`; the shipped `firm.DryRun` shows what would execute.

Dry-run blocks all tools; allowlists are enforced. Go runtimes are trusted code, not sandboxes. LLM risk reviews are advisory; deterministic policy must govern all paper execution.

## Development

Follow [AGENTS.md](AGENTS.md) and [docs/development.org](docs/development.org) for the issue → branch → PR → review → merge process.

```sh
go test -race ./...
go vet ./...
```

Source packages: `cmd/investor` (CLI), `internal/firm` (engine), `internal/service` (HTTP).
