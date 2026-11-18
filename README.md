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

Phase 1 design: [research roles and authority](roles/README.md),
[draft paper-trading policy](docs/paper-trading-policy.md),
[manual research session](docs/manual-research-session.md), and
[operational workflow specification](docs/workflows.md), and the
[research journal and evidence contract](docs/research-journal.md). The policy is
unapproved; its proposed limits do not authorize paper orders. These documents
guide manual research and future implementation, not the current dry-run engine.

## Run

Requires Go 1.25.1 or newer. YAML skill frontmatter uses `gopkg.in/yaml.v3`.

```sh
go build -o bin/investor ./cmd/investor
bin/investor validate
bin/investor plan --workflow research
bin/investor run --workflow research --objective 'Evaluate a trend-following hypothesis'
bin/investor serve --listen 127.0.0.1:8090
```

### Make targets

`make help` lists them. `make build` builds `bin/investor`; `make run` builds and runs a workflow (dry-run by default; pass extra flags with `ARGS`, for example `make run ARGS='--runtime model --objective "Evaluate EUR/USD"'`); `make validate`, `make plan` and `make serve` wrap the matching commands; `make test` runs `go test -race ./...`; `make vet` runs `go vet ./...`; `make check` runs gofmt-check, vet and tests (the checks required before a PR); `make fmt` formats; `make clean` removes `bin/`. Override the config with `CONFIG=path/to/firm.json`.

### Research journal

`run` appends the run, each agent step (including failures and stops) and each model call attempt to an append-only directory of JSON files (see `docs/research-journal.md`). Records are never overwritten; a run's outcome is a second revision, so a run interrupted mid-way stays visible as `running`. A failed journal write fails the run. The directory is `journal_dir` in the config file (relative paths resolve against the config file, like `skills_dir`; the example uses `../journal`), overridden by `run --journal DIR`. With neither, dry-run workflows are not recorded, and `--runtime model` refuses to start: paid, model-backed research must leave a durable record. `validate`, `plan` and `serve` never open the journal. The journal holds research evidence; keep it out of version control (`/journal/` is gitignored).

### Model runtime

`run --runtime model` sends each role's authority, complete skill instructions and the objective to the LLM configured for that role, using a fixed **synthetic** market snapshot. The default `--runtime dry-run` makes no model calls. Choose LLMs in the config file: a default `llm`, optionally overridden per role under `roles` (keyed by the agent's `role` name):

```json
"llm": {"provider": "claude", "model": "claude-sonnet-5-5", "max_tokens": 4096, "timeout_seconds": 120},
"roles": {
  "Strategy Challenger": {"provider": "openai", "model": "<openai model>", "api_key_env": "OPENAI_API_KEY"}
}
```

Providers are `claude` and `openai`. Per-run safeguards, with defaults shown:

```json
"limits": {"max_calls_per_run": 25, "max_tokens_per_run": 200000, "max_cost_usd_per_run": 0},
"llm": {"provider": "claude", "model": "...", "max_tokens": 4096, "timeout_seconds": 60, "max_retries": 2,
        "input_usd_per_mtok": 0, "output_usd_per_mtok": 0}
```

Every attempt, including retries, counts as a call. Every call has an explicit output bound, `max_tokens` (default 4096), sent to both providers. Limits are **best-effort caps on observed usage, not hard ceilings or guarantees**: they are checked before each call against usage already reported, so one call can overshoot by its input plus output tokens (input size is not bounded), and a failed attempt that was billed but returned no usage is not counted in tokens or cost. Reaching a limit fails the run with `model usage limit reached`; no more calls are made. `max_cost_usd_per_run` is optional and requires positive input and output prices on every LLM in use (a zero rate would undercount spend); set them from your provider's current price list, and treat the result as an estimate. A provider response without token usage fails closed. Only transient provider errors (HTTP 429, 5xx, connection failures) are retried, at most `max_retries` (default 2, maximum 5) times with doubling backoff; timeouts, auth errors and invalid or truncated output are never retried. Each call logs its token usage and cost to stderr. API keys are never stored in config: they are read from `api_key_env` (default `ANTHROPIC_API_KEY` / `OPENAI_API_KEY`). A missing key, a role with no LLM, malformed or invalid output, or a timeout fails the run with a clear error and no fabricated result. Each agent must return one JSON object with `status` (`ready|blocked|rejected`), `reason` and `findings`; model prose is never treated as a risk-policy pass. Investor assigns the artifact ID (`<run id>/<agent id>`) and fixes workflow order, so models cannot name artifacts or route the workflow. A `blocked` or `rejected` result stops the workflow: its artifact is kept as evidence, later roles do not run, and the run ends with status `stopped` and a `stop_reason`. Stopped is neither success nor approval. Tools are not offered to models and no order capability exists.

Skills are loaded at startup from `skills_dir`, relative to the configuration file (the example uses `../skills`). Missing or malformed referenced skills fail validation before a run or service starts.

Each command accepts `--config path/to/firm.json` (default: `examples/firm.json`, relative to the working directory). CLI results are JSON on stdout; logs are on stderr; failures exit nonzero. The foreground service shuts down on SIGINT/SIGTERM.

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

`examples/firm.json` previews analyst → researcher → challenger → risk → performance using the six file-backed skills. This remains a **dry-run prototype**, not an implemented research or paper-trading workflow.

- **Agent:** role, skill references, and tool allowlist.
- **Skill:** reusable instructions loaded from `skills/<name>/SKILL.md` by stable ID. See [skill loading and migration](docs/skill-files.md); inline prompts are deprecated.
- **Tool:** declared capability, separately implemented in Go; the example requires no tool integrations.
- **Workflow:** ordered agent steps receiving the objective and prior results; the first failure stops the run.
- **Runtime:** `firm.Runtime`; the shipped `firm.DryRun` shows what would execute.

Dry-run blocks all tools; allowlists are enforced. Go runtimes are trusted code, not sandboxes. LLM risk reviews are advisory; deterministic policy must govern all paper execution.

## Development

Follow [AGENTS.md](AGENTS.md) and [docs/development.org](docs/development.org) for the issue → branch → PR → review → merge process.

```sh
go test -race ./...
go vet ./...
```

Source packages: `cmd/investor` (CLI), `internal/firm` (engine), `internal/service` (transport-neutral application service), `internal/transport` (HTTP).
