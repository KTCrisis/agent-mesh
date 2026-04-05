# CLAUDE.md — Agent Mesh

## Current Status (April 5, 2026)

Last session: async non-blocking approval (v0.3.2), docs update
State: functional PoC, branchable as MCP server in Claude Code (filesystem + Gmail servers connected)
Repo: private

### Next actions (prioritized)
1. **JWT agent credentials** with scopes + budget
2. **Rate limiting** per agent
3. **First public demo** — README with architecture diagram, position vs Envoy analogy
4. **PostgreSQL traces** — replace in-memory store for persistence
5. **Temporal grants** — `mesh grant` for approval fatigue reduction

---

## What is Agent Mesh

An **agent sidecar proxy** — the Envoy of AI agents. A lightweight process that sits between any AI agent and backend systems (REST APIs, MCP servers, events), adding governance, policy enforcement, and observability without changing the agent code.

Agent Mesh is **not** an agent framework (that's LangChain/CrewAI). It's the infrastructure layer that controls what agents can do.

```
Without Agent Mesh:
  Agent → API / MCP server (direct, ungoverned)

With Agent Mesh:
  Agent → Sidecar Proxy → API / MCP servers
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
├── main.go                # Entry point, wires everything
├── config/
│   └── config.go          # YAML config + policy + MCP server definitions
├── registry/
│   ├── registry.go        # Tool/Param types, Registry (Get/All/Remove/LoadManual)
│   ├── openapi.go         # OpenAPI spec → tool catalog
│   └── mcp.go             # MCP tool registration (LoadMCP/RemoveByServer)
├── policy/
│   └── engine.go          # Evaluate tool calls against YAML policies
├── proxy/
│   └── handler.go         # HTTP proxy: auth → policy → forward (HTTP or MCP) → trace
├── mcp/
│   ├── server.go          # MCP server mode (expose tools via stdio JSON-RPC)
│   ├── client.go          # MCP client (connect to upstream MCP servers via stdio)
│   └── manager.go         # Manages N upstream MCP connections, implements MCPForwarder
├── trace/
│   └── store.go           # In-memory trace store + query API
├── policies.yaml          # Sample config with policies
└── go.mod
```

### Request flow

```
POST /tool/{tool_name}
  → proxy/handler.go
    1. Extract agent credential from Authorization header
    2. Look up tool in registry (OpenAPI or MCP source)
    3. Evaluate policy (is this agent allowed to call this tool with these params?)
    4. Forward to backend (HTTP for REST tools, JSON-RPC for MCP tools)
    5. Log trace (agent, tool, params, result, policy decision, latency)
    6. Return response to agent
```

## Stack

- **Language**: Go (single binary, high performance, no runtime)
- **Dependencies**: minimal — stdlib + YAML parser
- **No external infra for PoC**: in-memory trace store, file-based config
- **Later**: PostgreSQL traces, Kafka event emission, Redis cache

## Commands

```bash
go run .                                           # Run with defaults
go run . --config policies.yaml                    # Custom config
go run . --config policies.yaml --port 9090        # Custom port
go run . --mcp --openapi <spec-url>                # MCP server mode (stdio)
go build -o agent-mesh .                           # Build binary
go test ./...                                      # Run all tests
go test ./... -v                                   # Verbose
go test ./proxy/ -v                                # Single package
```

## API endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/tool/{name}` | Proxy a tool call (REST or MCP backend) |
| GET | `/tools` | List all registered tools (OpenAPI + MCP) |
| GET | `/mcp-servers` | List upstream MCP servers + status + tools |
| GET | `/traces` | Query trace history (`?agent=...&tool=...`) |
| GET | `/health` | Health check + stats |

## Config format (YAML)

```yaml
port: 9090

# Upstream MCP servers (tools are auto-discovered and namespaced)
mcp_servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]

policies:
  - name: support-agent
    agent: "support-*"
    rules:
      - tools: ["get_order", "get_customer"]
        action: allow
      - tools: ["filesystem.read_file"]    # MCP tools use server.tool naming
        action: allow
      - tools: ["filesystem.write_file"]
        action: deny
      - tools: ["create_refund"]
        action: allow
        condition:
          field: "params.amount"
          operator: "<"
          value: 500

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
- **Trace everything** — every tool call is logged. No silent actions.
- **Fail closed** — if no policy matches, deny. Explicit allow required.

## Code conventions

- Go standard library preferred over external packages
- Error handling: always return errors, never panic
- Logging: `log/slog` (structured, stdlib)
- Tests: in `_test.go` files next to source, run with `go test ./...`
- No global mutable state — pass dependencies via structs
- Context propagation: use `context.Context` for cancellation/timeouts
- Interface at consumer: `MCPForwarder` defined in `proxy/` to avoid import cycles

## Future extensions

- SSE transport for upstream MCP servers
- Agent credential format (JWT extension with scopes + budget)
- OpenTelemetry trace export
- Kafka event emission for traces
- AsyncAPI registry support (event-driven tools)
- Rate limiting per agent
- Cost tracking (token budget enforcement)
- Dashboard UI (Next.js, same stack as staffd/event7)
