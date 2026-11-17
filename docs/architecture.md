# Investor application boundaries

The transport collects configuration and request parameters, invokes a
transport-neutral service, and formats the result for a human or another agent.

```text
CLI   internal/transports (HTTP)   MCP (future)
  \        |        /
       internal/service
               |
       internal/firm (workflow orchestration)
               |
       domain services / permitted tools
               |
       structured result -> originating transport
```

The HTTP transport lives in `internal/transports/http.go` and depends on the
service; shared outbound provider HTTP remains in `agents/http.go`.

The service is the entry point for application use cases; the firm engine
retains ownership of workflow ordering, agent tool allowlists, and dry-run
permissions. Transports must not bypass the service for workflow operations.

`agents.Agent` is a narrow provider-independent model interface.
`agents/openai` uses the Responses API; `agents/claude` uses the Messages API.
These adapters are opt-in and are **not** wired into the dry-run engine yet.
They currently support text-only requests, not tool calls or sessions.
Choose a provider and model explicitly when constructing an adapter;
supply credentials via `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` in the caller.

Adding an MCP transport should expose the same service operations, not create
a parallel workflow engine. Never expose trading execution or tools merely
because an AI provider or transport supports them. Paper-trading policy and
future risk checks remain deterministic and enforced by Investor.
