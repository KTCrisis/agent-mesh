# Agent Mesh

**The Envoy of AI agents.** A lightweight sidecar proxy that sits between AI agents and backend systems (REST APIs, MCP servers), adding policy enforcement, observability, and access control — without changing agent code.

```
Without Agent Mesh:          With Agent Mesh:
Agent → API                  Agent → Sidecar → API / MCP servers
(ungoverned)                          ↓
                              auth · policy · trace
```

## Why

AI agents (LangChain, CrewAI, Claude, custom) can call APIs and tools. But there's no standard way to control **what** they call, **who** can call it, and **what happened**. Agent Mesh solves this with a single proxy.

- **Framework agnostic** — works with any agent that makes HTTP calls or uses MCP
- **Policy as code** — YAML rules, versionable in git, reviewable in PRs
- **Fail closed** — no matching policy = denied
- **Trace everything** — every tool call is logged with agent, params, decision, latency
- **Single binary** — `go build`, no runtime, no containers required

## Quick start

```bash
# Build
go build -o agent-mesh .

# Run all tests
go test ./...
```

## The 3 modes

Agent Mesh has 3 distinct operations that can be **combined freely**:

```
┌─────────────────────────────────────────────────────────┐
│                     agent-mesh                          │
│                                                         │
│  ┌─────────────┐     ┌──────────┐     ┌─────────────┐  │
│  │ IMPORT      │     │          │     │ EXPORT      │  │
│  │ OpenAPI     │────▶│ Registry │────▶│ MCP server  │  │
│  │ (Swagger)   │     │ (tools)  │     │ (stdio)     │  │
│  └─────────────┘     │          │     └──────┬──────┘  │
│  ┌─────────────┐     │          │            │         │
│  │ IMPORT      │────▶│          │            ▼         │
│  │ MCP servers │     │          │     Claude, Cursor,  │
│  │ (upstream)  │     └──────────┘     any MCP client   │
│  └─────────────┘          │                            │
│                     policy · trace                      │
└─────────────────────────────────────────────────────────┘
```

### Import OpenAPI — Turn a REST API into governed tools

Parse a Swagger/OpenAPI spec, register each endpoint as a tool, and proxy calls to the real API.

**Use case**: You have an existing REST API and want agents to call it with governance.

```bash
./agent-mesh --openapi https://petstore.swagger.io/v2/swagger.json

# What happens:
# Swagger spec → parsed → 20 tools registered (get_pet_by_id, add_pet, etc.)
# Agents call agent-mesh, which forwards to the real Petstore API
```

```bash
# List discovered tools
curl http://localhost:9090/tools | jq

# Call a tool (agent-mesh forwards to the real API)
curl -X POST http://localhost:9090/tool/get_pet_by_id \
  -H "Authorization: Bearer support-bot" \
  -d '{"params": {"petId": 1}}' | jq

# Denied by policy
curl -X POST http://localhost:9090/tool/delete_pet \
  -H "Authorization: Bearer support-bot" \
  -d '{"params": {"petId": 1}}' | jq
```

### Import MCP — Connect to upstream MCP servers

Connect to existing MCP servers (filesystem, GitHub, databases, etc.), discover their tools, and add governance on top.

**Use case**: You use MCP servers but need policy control and tracing.

```yaml
# policies.yaml
port: 9090

mcp_servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]

policies:
  - name: allow-reads
    agent: "claude"
    rules:
      - tools: ["filesystem.read_file", "filesystem.list_directory"]
        action: allow
      - tools: ["filesystem.write_file"]
        action: deny
  - name: default
    agent: "*"
    rules:
      - tools: ["*"]
        action: deny
```

```bash
./agent-mesh --config policies.yaml

# What happens:
# agent-mesh launches the MCP server subprocess
# Handshake → discovers 14 tools (read_file, write_file, etc.)
# Tools registered as filesystem.read_file, filesystem.write_file, etc.
```

```bash
# List connected MCP servers + status
curl http://localhost:9090/mcp-servers | jq

# List all tools (namespaced: server.tool)
curl http://localhost:9090/tools | jq

# Call an MCP tool through the governance pipeline
curl -X POST http://localhost:9090/tool/filesystem.read_file \
  -H "Authorization: Bearer claude" \
  -d '{"params": {"path": "/tmp/test.txt"}}' | jq
```

### Export MCP — Expose everything as an MCP server

Agent-mesh exposes all its tools (from any source) as an MCP server via stdio JSON-RPC. This makes it pluggable into Claude Code, Cursor, or any MCP client.

**Use case**: You want Claude or Cursor to use a REST API (or governed MCP tools) natively.

```bash
# A REST API, re-exposed as MCP for Claude
./agent-mesh --mcp --openapi https://petstore.swagger.io/v2/swagger.json

# Register in Claude Code
claude mcp add agent-mesh -- ./agent-mesh --mcp --config policies.yaml
```

```
What happens:
  Petstore (REST)  →  Import OpenAPI  →  registry  →  Export MCP  →  Claude
                                              ↓
                                      policy + trace on every call
```

### Combining all 3

All modes work together. Import from multiple sources, apply unified policies, and optionally re-export as MCP:

```yaml
# policies.yaml — mixed REST + MCP sources
port: 9090

mcp_servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]

policies:
  - name: claude-agent
    agent: "claude"
    rules:
      # REST tools (from Petstore OpenAPI)
      - tools: ["get_pet_by_id", "find_pets_by_status"]
        action: allow
      # MCP tools (from filesystem server)
      - tools: ["filesystem.read_file", "filesystem.list_directory"]
        action: allow
      # Everything else denied
      - tools: ["*"]
        action: deny
```

```bash
# HTTP mode: REST + MCP in same registry
./agent-mesh --config policies.yaml \
  --openapi https://petstore.swagger.io/v2/swagger.json

# MCP mode: same thing, but exposed as MCP server for Claude
./agent-mesh --mcp --config policies.yaml \
  --openapi https://petstore.swagger.io/v2/swagger.json
```

```
Petstore API ──Import OpenAPI──▶ ┌──────────┐ ──HTTP proxy──▶ agents (HTTP)
                                 │ registry  │
MCP servers  ──Import MCP──────▶ │ (unified) │ ──Export MCP──▶ Claude, Cursor
                                 └─────┬─────┘
                                 policy · trace
```

## How it works

```
POST /tool/{tool_name}
  1. Extract agent ID from Authorization header
  2. Look up tool in registry (any source: OpenAPI or MCP)
  3. Evaluate policy (YAML rules → allow / deny / human_approval)
  4. Forward to backend (HTTP for OpenAPI tools, JSON-RPC for MCP tools)
  5. Log trace (agent, tool, params, result, policy, latency)
  6. Return response to agent
```

## Policies

Define who can do what in `policies.yaml`:

```yaml
port: 9090

mcp_servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
  # - name: github
  #   transport: stdio
  #   command: npx
  #   args: ["-y", "@modelcontextprotocol/server-github"]
  #   env:
  #     GITHUB_TOKEN: "ghp_xxx"

policies:
  - name: support-agent
    agent: "support-*"          # glob pattern on agent ID
    rules:
      - tools: ["get_order", "get_customer"]
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

  - name: default
    agent: "*"
    rules:
      - tools: ["*"]
        action: deny            # fail closed
```

### Policy actions

| Action | HTTP code | Behavior |
|--------|-----------|----------|
| `allow` | `200` | Forward the call to the backend, return the result |
| `deny` | `403` | Block the call, no forwarding. Reason included in response |
| `human_approval` | `202` | Block the call pending human review. Traced as pending |

Default behavior: **fail closed** — if no rule matches, the call is denied.

### Policy evaluation

Rules are evaluated **in order** (first match wins):

1. Find the first policy where `agent` pattern matches the caller
2. Within that policy, find the first rule where `tools` matches the tool name
3. If the rule has a `condition`, evaluate it against the call params
4. If condition passes (or no condition), return the action
5. If no rule matches anywhere, return `deny` (fail closed)

### Conditions

Conditions allow fine-grained control based on call parameters:

```yaml
condition:
  field: "params.amount"     # dot-path into the request params
  operator: "<"              # comparison operator
  value: 500                 # threshold
```

Operators: `<`, `<=`, `>`, `>=`, `==`, `!=`

### Agent patterns

The `agent` field supports glob patterns:

| Pattern | Matches |
|---------|---------|
| `"support-*"` | `support-bot`, `support-agent-1` |
| `"admin-*"` | `admin-1`, `admin-ops` |
| `"claude"` | exactly `claude` |
| `"*"` | any agent (including `anonymous`) |

### Tool patterns

The `tools` field is a list of exact tool names, or `"*"` for all:

```yaml
- tools: ["get_order", "get_customer"]    # specific tools
- tools: ["filesystem.read_file"]          # namespaced MCP tool
- tools: ["*"]                             # all tools
```

## API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/tool/{name}` | Proxy a tool call through policies |
| `GET` | `/tools` | List all registered tools (OpenAPI + MCP) |
| `GET` | `/mcp-servers` | List connected upstream MCP servers + status |
| `GET` | `/traces` | Query trace history (`?agent=...&tool=...`) |
| `GET` | `/health` | Health check + stats |

## Architecture

```
agent-mesh/
├── main.go                # Entry point, wires everything
├── config/
│   └── config.go          # YAML config + policy + MCP server definitions
├── registry/
│   ├── registry.go        # Tool/Param types, Registry (Get/All/Remove)
│   ├── openapi.go         # Import OpenAPI → tool catalog
│   └── mcp.go             # Import MCP → tool catalog
├── policy/
│   └── engine.go          # Rule evaluation engine
├── proxy/
│   └── handler.go         # HTTP proxy (auth → policy → forward → trace)
├── mcp/
│   ├── server.go          # Export MCP (stdio JSON-RPC server)
│   ├── client.go          # Import MCP (connect to upstream servers)
│   └── manager.go         # Manages N upstream MCP connections
├── trace/
│   └── store.go           # In-memory trace store
└── policies.yaml          # Example policies
```

## Tests

Tests live next to source files (`*_test.go`), following Go conventions:

```bash
# Run all tests
go test ./...

# Run with race detector
go test ./... -race

# Run tests verbose
go test ./... -v

# Run a specific package
go test ./proxy/ -v

# Run a specific test
go test ./policy/ -run TestEvaluateMCPNamespacedTools -v
```

Coverage (49 tests):

| Package | Tests | Covers |
|---------|-------|--------|
| `config` | 5 | YAML parsing, defaults, `mcp_servers`, conditions |
| `registry` | 10 | CRUD, MCP loading, namespacing, mixed sources, concurrent access |
| `policy` | 8 | allow/deny, conditions, wildcards, fail closed, MCP tools |
| `proxy` | 17 | REST + MCP tool calls, deny/error/approval, all endpoints, URL encoding |
| `trace` | 6 | record, filters, limit, eviction, stats |
| `mcp` | 13 | client lifecycle, timeouts, goroutine cleanup, manager concurrent access |

## CLI flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `policies.yaml` | Path to config/policies YAML |
| `--openapi` | | OpenAPI spec URL (Import OpenAPI) |
| `--backend` | | Backend base URL (overrides spec) |
| `--port` | from config or `9090` | Port override |
| `--mcp` | `false` | Export MCP mode (stdio JSON-RPC server) |
| `--mcp-agent` | `claude` | Agent ID for MCP mode policy evaluation |

## Roadmap

- [x] Import OpenAPI (REST API → governed tools)
- [x] Import MCP (upstream MCP servers → governed tools)
- [x] Export MCP (expose tools as MCP server)
- [x] Policy engine (allow/deny/human_approval + conditions)
- [x] Trace store with query API
- [x] `GET /mcp-servers` endpoint
- [x] Graceful shutdown
- [ ] SSE transport for Import MCP
- [ ] Agent credential format (JWT with scopes + budget)
- [ ] AsyncAPI support (event-driven tools via Kafka)
- [ ] OpenTelemetry trace export
- [ ] Rate limiting per agent
- [ ] Cost tracking (token budget enforcement)
- [ ] Dashboard UI
- [ ] Persistent trace store (PostgreSQL)

## License

Apache 2.0
