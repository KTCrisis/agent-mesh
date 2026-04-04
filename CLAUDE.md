# CLAUDE.md — Agent Mesh

## What is Agent Mesh

An **agent sidecar proxy** — the Envoy of AI agents. A lightweight process that sits between any AI agent and backend systems (APIs, events, tools), adding governance, policy enforcement, and observability without changing the agent code.

Agent Mesh is **not** an agent framework (that's LangChain/CrewAI). It's the infrastructure layer that controls what agents can do.

```
Without Agent Mesh:
  Agent → API (direct, ungoverned)

With Agent Mesh:
  Agent → Sidecar Proxy → API
              ↓
          auth · policy · trace · rate limit
```

## Positioning

Part of a consulting firm's open-source toolset (API + EDA + Agentic AI architecture).
The proxy is the product; consulting deploys and configures it.

Progressive adoption:
- **Level 1**: 3 APIs, SQLite traces, 30-minute setup
- **Level 2**: API gateway import, advanced policies, PostgreSQL
- **Level 3**: Kafka events, multi-agent orchestration, full mesh

## Architecture

```
agent-mesh/
├── main.go              # Entry point, HTTP server, wires everything
├── config/
│   └── config.go        # YAML config + policy definitions loader
├── registry/
│   └── openapi.go       # OpenAPI spec parser → tool catalog
├── policy/
│   └── engine.go        # Evaluate tool calls against YAML policies
├── proxy/
│   └── handler.go       # Core: auth → policy → forward → trace → respond
├── trace/
│   └── store.go         # In-memory trace store + query API
├── policies.yaml        # Sample config with policies
└── go.mod
```

### Request flow

```
POST /tool/{tool_name}
  → proxy/handler.go
    1. Extract agent credential from Authorization header
    2. Look up tool in registry (is it a known tool?)
    3. Evaluate policy (is this agent allowed to call this tool with these params?)
    4. Forward to backend API (the real endpoint)
    5. Log trace (agent, tool, params, result, policy decision, latency)
    6. Return response to agent
```

## Stack

- **Language**: Go (single binary, high performance, no runtime)
- **Dependencies**: minimal — stdlib + YAML parser + OpenAPI parser
- **No external infra for PoC**: in-memory trace store, file-based config
- **Later**: PostgreSQL traces, Kafka event emission, Redis cache

## Commands

```bash
go run .                                           # Run with defaults
go run . --config policies.yaml                    # Custom config
go run . --config policies.yaml --port 9090        # Custom port
go build -o agent-mesh .                           # Build binary
go test ./...                                      # Run all tests
```

## API endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/tool/{name}` | Proxy a tool call (the core endpoint) |
| GET | `/tools` | List all registered tools (from registry) |
| GET | `/traces` | Query trace history |
| GET | `/health` | Health check |
| POST | `/registry/load` | Load an OpenAPI spec into the registry |

## Tool call format

```json
POST /tool/get_order
Authorization: Bearer <agent-credential>

{
  "params": {
    "order_id": "ORD-123"
  }
}
```

Response:
```json
{
  "result": { ... },
  "trace_id": "abc-123",
  "policy": "allow",
  "latency_ms": 120
}
```

## Policy format (YAML)

```yaml
port: 9090

policies:
  - name: support-agent
    agent: "support-*"
    rules:
      - tools: ["get_order", "get_customer", "get_tracking"]
        action: allow
      - tools: ["create_refund"]
        action: allow
        condition:
          field: "params.amount"
          operator: "<"
          value: 500
      - tools: ["create_refund"]
        action: deny
        condition:
          field: "params.amount"
          operator: ">="
          value: 500
      - tools: ["delete_customer"]
        action: deny

  - name: default
    agent: "*"
    rules:
      - tools: ["*"]
        action: deny
```

## Design principles

- **Zero agent code change** — the agent calls the sidecar URL instead of the real API. That's the only change.
- **Framework agnostic** — works with LangChain, CrewAI, raw HTTP, MCP, anything that makes HTTP calls.
- **Single binary** — `go build` produces one binary, no runtime, no containers required.
- **Config as code** — policies are YAML, versionable in git, reviewable in PRs.
- **Trace everything** ��� every tool call is logged. No silent actions.
- **Fail closed** — if no policy matches, deny. Explicit allow required.

## Code conventions

- Go standard library preferred over external packages
- Error handling: always return errors, never panic
- Logging: `log/slog` (structured, stdlib)
- Tests: table-driven, in `_test.go` files next to source
- No global mutable state — pass dependencies via structs
- Context propagation: use `context.Context` for cancellation/timeouts

## Future extensions (not in PoC)

- MCP server mode (expose tools as MCP instead of REST)
- Agent credential format (JWT extension with scopes + budget)
- OpenTelemetry trace export
- Kafka event emission for traces
- AsyncAPI registry support (event-driven tools)
- Rate limiting per agent
- Cost tracking (token budget enforcement)
- Dashboard UI (Next.js, same stack as staffd/event7)
