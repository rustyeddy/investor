# Investor application boundaries

The transport collects configuration and request parameters, invokes a
transport-neutral service, and formats the result for a human or another agent.

```text
CLI   internal/transport (HTTP)   MCP (future)
  \        |        /
       internal/service
               |
       internal/firm (workflow orchestration)
               |
       domain services / permitted tools
               |
       structured result -> originating transport
```

The HTTP transport lives in `internal/transport/http.go` and depends on the
service; shared outbound provider HTTP remains in `agents/http.go`.
`cmd/investor` is the composition root: it loads configuration, builds the
firm engine and service, and hands the service to the CLI or HTTP transport.
`internal/service` knows nothing about transports.

The service is the entry point for application use cases; the firm engine
retains ownership of workflow ordering, agent tool allowlists, and dry-run
permissions. Transports must not bypass the service for workflow operations.

`agents.Agent` is a narrow provider-independent model interface.
`agents/openai` uses the Responses API; `agents/claude` uses the Messages API.
These adapters are opt-in and are **not** wired into the dry-run engine yet.
They currently support text-only requests, not tool calls or sessions.
Adapters fail closed: truncated or incomplete provider output (Claude
`stop_reason` other than `end_turn`/`stop_sequence`, OpenAI `status` other than
`completed`) is an error, never a successful `agents.Response`.
Choose a provider and model explicitly when constructing an adapter;
supply credentials via `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` in the caller.

`internal/model` implements `firm.Runtime` on top of `agents.Agent`. The
configuration's `llm` (default) and `roles` (role name -> LLM) sections select
the provider and model for each role; `cmd/investor` builds it for
`run --runtime model`. It supplies the role authority, complete skill
instructions and objective as trusted inputs, labels prior role outputs as
untrusted evidence, applies a per-call timeout, and accepts only a strictly
validated JSON artifact. A `blocked` or `rejected` artifact is returned with `firm.ErrStopped`: the engine keeps it as evidence, skips later agents, and ends the run `stopped` (no error, no approval). Investor assigns artifact IDs. Transient provider errors are retried with a bounded count and backoff; each
run has call, token and optional cost limits (best-effort caps on observed usage, not hard ceilings) tracked in a per-run ledger that the
engine releases through `firm.RunFinisher`. Limits fail the run (`failed`, not
`stopped`) because they are an operational stop, not a research verdict.

Adding an MCP transport should expose the same service operations, not create
a parallel workflow engine. Never expose trading execution or tools merely
because an AI provider or transport supports them. Paper-trading policy and
future risk checks remain deterministic and enforced by Investor.
