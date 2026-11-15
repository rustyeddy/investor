# Investor

Investor is the Go orchestration layer for an AI assisted trading firm. It
defines agents, their skills and permitted tools, and the workflows they run.
The same engine runs from a command line or a foreground HTTP service.

This first implementation runs **dry-run workflows only**. It validates the
configuration and exercises agent sequencing and handoffs. It does not call an
LLM, analyze markets, access Trader, or place trades.

## Run

Requires Go 1.25.1 or newer; no external Go dependencies.

```sh
go build -o bin/investor ./cmd/investor
bin/investor validate
bin/investor plan -workflow research
bin/investor run -workflow research -objective 'Evaluate a trend-following hypothesis'
bin/investor serve -listen 127.0.0.1:8090
```

Each command accepts `-config path/to/firm.json`; the default is
`examples/firm.json`, relative to the working directory. CLI results are JSON
on stdout and logs go to stderr. Failed commands exit nonzero. The service
runs in the foreground for a process supervisor and shuts down on SIGINT or
SIGTERM.

```sh
curl http://127.0.0.1:8090/api/manager/agents
curl -X POST http://127.0.0.1:8090/api/manager/runs \
  -H 'Content-Type: application/json' \
  -d '{"workflow":"research","objective":"Evaluate a trend-following hypothesis"}'
```

HTTP endpoints: `GET /api/health`, `GET /api/manager/{agents,skills,tools,workflows}`,
and `POST /api/manager/runs`. Runs are synchronous, have a one-minute request
deadline, and return their results directly. There is no persistent run store,
background queue, scheduler, authentication, or API for modifying definitions
yet. The default listener is loopback; authenticated remote access is future work.

## Responsibilities

| Component                                     | Responsibility                                                                                                             |
|-----------------------------------------------|----------------------------------------------------------------------------------------------------------------------------|
| Investor                                      | Agent lifecycle, model runtimes, skills, tool permissions, workflow coordination, and eventually durable run/audit records |
| [Trader](https://github.com/rustyeddy/trader) | Historical/realtime data, deterministic backtesting, and eventual trade execution                                          |
| Tulip (`../tulip`)                            | GUI and backend-for-frontend consuming Investor's manager API                                                              |

Trader and Tulip are integration targets, not build dependencies. Investor
does not import Trader's private runtime or duplicate its trading engine.
Tulip already reserves `/api/manager/*`; its existing strategy/test mocks have
not been replaced, and the GUI is not wired to these new endpoints yet.

## Agent model

`examples/firm.json` defines research, backtest, risk, portfolio,
execution, and operations roles. The research workflow runs research →
backtest → risk → portfolio → operations. Execution is defined but not
scheduled.

- An **agent** has an ID, role, skill references, and a tool allowlist.
- A **skill** contains reusable instructions supplied to the runtime.
- A **tool** declares a capability; executable implementations are registered
  separately in Go. The example Trader tools are declarations only.
- A **workflow** is an ordered sequence of agents. Each receives the objective
  and preceding results. The first failure stops the workflow.
- A **runtime** implements `firm.Runtime`. The shipped `firm.DryRun` reports
  what would run without performing analysis. Model-backed runtimes are next.

The engine enforces tool allowlists and blocks all tools in dry-run
mode.  Runtime implementations are trusted Go code, not sandboxed
processes. Natural language risk reviews are advisory; future
execution must enforce structured approval and deterministic risk
policy outside the model.

## Next implementation steps

1. Select an AI runtime and add bounded model/tool iterations, timeouts, usage
   budgets, and structured agent outputs.
2. Agree on Trader's supported integration contract and connect historical
   data and backtests through adapters.
3. Persist runs, tool calls, artifacts, and audit events; introduce background
   jobs, recovery, and schedules.
4. Connect Tulip's backend to the manager API and add authenticated access.
5. Add structured portfolio/risk decisions and paper execution with
   reconciliation before considering live execution.

## Development

```sh
go test -race ./...
go vet ./...
```

Code lives in `cmd/investor` (CLI and process lifecycle), `internal/firm`
(definitions and engine), and `internal/service` (HTTP transport).
