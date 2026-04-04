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

### Mode 1: REST proxy (OpenAPI backend)

Proxy tool calls to a REST API discovered via an OpenAPI spec:

```bash
./agent-mesh \
  --config policies.yaml \
  --openapi https://petstore.swagger.io/v2/swagger.json

# List discovered tools
curl http://localhost:9090/tools | jq

# Allowed call (support agent, read)
curl -X POST http://localhost:9090/tool/get_pet_by_id \
  -H "Authorization: Bearer support-bot" \
  -d '{"params": {"petId": 1}}' | jq

# Denied call (support agent, delete)
curl -X POST http://localhost:9090/tool/delete_pet \
  -H "Authorization: Bearer support-bot" \
  -d '{"params": {"petId": 1}}' | jq

# View traces
curl http://localhost:9090/traces | jq
```

### Mode 2: MCP upstream (MCP servers as backends)

Connect to upstream MCP servers, aggregate their tools, and apply governance:

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

# List connected MCP servers and their tools
curl http://localhost:9090/mcp-servers | jq

# List all tools (MCP tools are namespaced: server.tool)
curl http://localhost:9090/tools | jq

# Call an MCP tool through the governance pipeline
curl -X POST http://localhost:9090/tool/filesystem.read_file \
  -H "Authorization: Bearer claude" \
  -d '{"params": {"path": "/tmp/test.txt"}}' | jq
```

### Mode 3: MCP server (expose as MCP)

Agent-mesh itself can be exposed as an MCP server (stdio JSON-RPC), making it pluggable into Claude Code, Cursor, or any MCP client:

```bash
# Use as MCP server (tools come from OpenAPI or upstream MCP servers)
./agent-mesh --mcp --openapi https://petstore.swagger.io/v2/swagger.json

# Or add directly to Claude Code
claude mcp add agent-mesh -- ./agent-mesh --mcp --config policies.yaml
```

## How it works

```
POST /tool/{tool_name}
  1. Extract agent ID from Authorization header
  2. Look up tool in registry (OpenAPI or MCP source)
  3. Evaluate policy (YAML rules → allow / deny / human_approval)
  4. Forward to backend (HTTP for REST, JSON-RPC for MCP)
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

Actions: `allow`, `deny`, `human_approval`

## API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/tool/{name}` | Proxy a tool call through policies |
| `GET` | `/tools` | List all registered tools (REST + MCP) |
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
│   ├── openapi.go         # OpenAPI spec → tool catalog
│   └── mcp.go             # MCP tool registration (LoadMCP/RemoveByServer)
├── policy/
│   └── engine.go          # Rule evaluation engine
├── proxy/
│   └── handler.go         # HTTP proxy (auth → policy → forward → trace)
├── mcp/
│   ├── server.go          # MCP server mode (stdio JSON-RPC)
│   ├── client.go          # MCP client (connect to upstream MCP servers)
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

# Run tests verbose
go test ./... -v

# Run a specific package
go test ./proxy/ -v

# Run a specific test
go test ./policy/ -run TestEvaluateMCPNamespacedTools -v
```

Coverage:

| Package | Tests | Covers |
|---------|-------|--------|
| `config` | 5 | YAML parsing, defaults, `mcp_servers`, conditions |
| `registry` | 8 | CRUD, MCP loading, namespacing, mixed sources |
| `policy` | 8 | allow/deny, conditions, wildcards, fail closed, MCP tools |
| `proxy` | 12 | REST + MCP tool calls, deny/error, all endpoints, agent ID |
| `trace` | 6 | record, filters, limit, eviction, stats |
| `mcp` | 4 | manager CRUD, statuses, error cases |

## CLI flags

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `policies.yaml` | Path to config/policies YAML |
| `--openapi` | | OpenAPI spec URL to load |
| `--backend` | | Backend base URL (overrides spec) |
| `--port` | from config or `9090` | Port override |
| `--mcp` | `false` | Run as MCP server (stdio JSON-RPC) |
| `--mcp-agent` | `claude` | Agent ID for MCP mode policy evaluation |

## Roadmap

- [x] REST proxy with OpenAPI discovery
- [x] Policy engine (allow/deny/human_approval + conditions)
- [x] Trace store with query API
- [x] MCP server mode (expose tools as MCP)
- [x] MCP upstream client (connect to MCP servers as backends)
- [x] `GET /mcp-servers` endpoint
- [x] Graceful shutdown
- [ ] SSE transport for MCP upstream
- [ ] Agent credential format (JWT with scopes + budget)
- [ ] AsyncAPI support (event-driven tools via Kafka)
- [ ] OpenTelemetry trace export
- [ ] Rate limiting per agent
- [ ] Cost tracking (token budget enforcement)
- [ ] Dashboard UI
- [ ] Persistent trace store (PostgreSQL)

## License

Apache 2.0
