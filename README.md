# Agent Mesh

**Guardrails for AI agents.**
Agent Mesh is an open-source policy and tracing layer for agent tool calls.

It sits between AI agents and the tools they use — MCP servers, REST APIs, CLI tools, and custom skills — without changing agent code.

One binary. One YAML config. Fail closed by default.

Works with Claude Code, Cursor, LangChain, CrewAI, or any agent that uses HTTP or MCP.

## At a glance

**Without Agent Mesh**

```
Agent → Tools

No policy
No trace
No control
```

**With Agent Mesh**

```
Agent → Agent Mesh → Tools

Policy
Trace
Control
```

## The problem

Today, when you connect tools directly to Claude Code, Cursor, or any other AI agent, the agent gets unguarded access to them.

For example, with direct MCP connections:

```bash
claude mcp add filesystem -- npx @modelcontextprotocol/server-filesystem /
claude mcp add github -- npx @modelcontextprotocol/server-github
claude mcp add database -- npx mcp-server-sqlite --db prod.db
```

That means:

- Claude can access the filesystem directly
- Claude can call GitHub tools directly
- Claude can query or modify a database directly

In practice, this can become:

- `filesystem` → full access, including destructive operations
- `github` → full access, including pushes to main
- `database` → full access, including dangerous write queries

**No policy. No trace. No control.**
If something goes wrong, you may not even know what happened.

## The solution

Put Agent Mesh between the agent and its tools.

Instead of wiring every tool directly into the agent, expose a controlled tool surface through Agent Mesh.

```bash
claude mcp add agent-mesh -- ./agent-mesh --mcp --config policies.yaml
```

Now the flow becomes:

```
Claude ──▶ agent-mesh ──▶ filesystem (read only)
                     ├──▶ github (read, create issues — no push)
                     └──▶ database (SELECT only — no DELETE)
                        ↓
                  policy · trace
```

The agent sees a normal tool surface.
Agent Mesh sits in between, enforcing policy and recording traces on every call.

## Why Agent Mesh

Agent Mesh focuses on one thing:
**controlling what agents are allowed to do when they call tools.**

| It is | It is not |
|-------|-----------|
| A policy layer for tool calls | An API gateway |
| A lightweight local binary | An agent framework |
| A governance sidecar / proxy | A cloud platform |
| Config-as-code with YAML | An MCP hosting service |
| | A dashboard-heavy control plane |

Think of it as **ESLint for agent actions**: lightweight, local-first, reviewable in Git, and designed to catch unsafe behavior before it happens.

## Use cases

### Solo developer — prevent accidents

You use Claude Code or another MCP-capable agent, but you want guardrails around tool usage.

```yaml
policies:
  - name: claude
    agent: "claude"
    rules:
      - tools: ["filesystem.read_file", "filesystem.list_directory"]
        action: allow
      - tools: ["filesystem.write_file", "filesystem.move_file"]
        action: deny
```

Result: the agent can read files, but cannot modify or move them.

### Team — shared, reviewable configuration

Instead of every developer wiring their own tools and permissions, the team shares one `policies.yaml`, versioned in Git and reviewed in pull requests.

```yaml
policies:
  - name: senior-devs
    agent: "senior-*"
    rules:
      - tools: ["*"]
        action: allow

  - name: junior-devs
    agent: "junior-*"
    rules:
      - tools: ["*.read_*", "*.list_*", "*.get_*"]
        action: allow
      - tools: ["*"]
        action: deny
```

Result: access control becomes explicit, reviewable, and reproducible.

### Production agents — auditable internal tool access

Autonomous agents built with LangChain, CrewAI, or custom runtimes can call internal APIs and tools through Agent Mesh, with every call traced.

```bash
curl http://localhost:9090/traces | jq
```

```json
[
  {
    "agent": "support-bot",
    "tool": "create_refund",
    "params": { "amount": 450 },
    "policy": "allow"
  },
  {
    "agent": "support-bot",
    "tool": "create_refund",
    "params": { "amount": 5000 },
    "policy": "deny"
  }
]
```

Result: every action is visible and auditable.

### REST APIs — governance without building an MCP server

You already have a REST API with an OpenAPI/Swagger spec. You want agents to use it with policy and trace, without writing a custom MCP server.

```bash
./agent-mesh --mcp --openapi https://your-api.com/swagger.json --config policies.yaml
```

Result: Agent Mesh imports the API, applies policies, and can re-expose it as MCP for Claude Code or Cursor.

## Core principles

- **For any developer** — install in minutes, one binary, one YAML file
- **For any agent** — Claude Code, Cursor, LangChain, CrewAI, raw HTTP, or custom agents
- **For any tool surface** — MCP servers, REST APIs, CLI tools, custom skills
- **Zero agent code change** — point the agent at Agent Mesh instead of the real backend
- **Policy as code** — YAML rules, versionable in Git, reviewable in PRs
- **Fail closed** — no matching policy means deny
- **Trace everything** — every call is logged with agent, params, decision, and latency
- **Single binary** — no mandatory runtime, no mandatory containers

## Quick start

### 1. Build

```bash
go build -o agent-mesh .
```

### 2. Discover tools and generate a starter policy

If you do not know the tool names yet, use `discover`.

```bash
# Discover tools from MCP servers defined in config
./agent-mesh discover --config policies.yaml

# Generate a starter policy (read-only defaults)
./agent-mesh discover --config policies.yaml --generate-policy

# Discover tools from an OpenAPI spec
./agent-mesh discover --openapi https://petstore.swagger.io/v2/swagger.json --generate-policy
```

The `discover` command:

- connects to upstream tool sources
- lists tools with descriptions
- classifies them as read or write operations
- generates a safe starter policy you can refine

### 3. Write a policy

```yaml
mcp_servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/home/me/projects"]

policies:
  - name: safe-mode
    agent: "*"
    rules:
      - tools:
          [
            "filesystem.read_file",
            "filesystem.list_directory",
            "filesystem.search_files",
          ]
        action: allow
      - tools: ["*"]
        action: deny
```

### 4. Plug it into your agent

**Claude Code:**

```bash
claude mcp add agent-mesh -- ./agent-mesh --mcp --config policies.yaml
```

Claude now sees the filesystem tools through Agent Mesh, but only within the policy you defined.

**Cursor:** add Agent Mesh to `.cursor/mcp.json`.

**HTTP agents:**

```bash
./agent-mesh --config policies.yaml --port 9090
```

Then point your agents to `http://localhost:9090/tool/{name}`.

### 5. Inspect traces

```bash
curl http://localhost:9090/traces | jq
```

## The 3 modes

Agent Mesh has three composable operations:

```
┌─────────────────────────────────────────────────────────┐
│                     agent-mesh                          │
│                                                         │
│  ┌──���──────────┐     ┌──────────┐     ┌───��─────────┐  │
│  │ IMPORT      │     │          │     │ EXPORT      │  │
��  │ OpenAPI     │────▶│ Registry ���────▶│ MCP server  │  │
│  │ (Swagger)   │     │ (tools)  │     │ (stdio)     │  │
│  └─────────────┘     │          │     └──────┬──────┘  │
│  ┌────────────��┐     │          │            │         │
│  │ IMPORT      │────▶│          │            ▼         │
│  │ MCP servers │     │          │     Claude, Cursor,  │
│  │ (upstream)  │     └──────────┘     any MCP client   │
│  └───────��─────┘          │                            │
│                     policy · trace                      │
└────���────────────────────────────────────────────────────┘
```

### 1. Import OpenAPI

Turn a REST API into governed tools.

**Use case:** you have an existing REST API and want agents to call it safely.

```bash
./agent-mesh --openapi https://petstore.swagger.io/v2/swagger.json
```

What happens:

- the OpenAPI spec is parsed
- each endpoint is registered as a tool
- calls are proxied to the real backend
- policies and traces are applied uniformly

```bash
curl http://localhost:9090/tools | jq

curl -X POST http://localhost:9090/tool/get_pet_by_id \
  -H "Authorization: Bearer support-bot" \
  -d '{"params": {"petId": 1}}' | jq

curl -X POST http://localhost:9090/tool/delete_pet \
  -H "Authorization: Bearer support-bot" \
  -d '{"params": {"petId": 1}}' | jq
```

### 2. Import MCP

Connect to upstream MCP servers, discover their tools, and add governance on top.

**Use case:** you already use MCP servers, but need policy control and tracing.

```yaml
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
```

What happens:

- Agent Mesh launches the MCP subprocess
- performs the handshake
- discovers upstream tools
- registers them as namespaced tools such as `filesystem.read_file`
- applies policy and trace to every call

```bash
curl http://localhost:9090/mcp-servers | jq
curl http://localhost:9090/tools | jq

curl -X POST http://localhost:9090/tool/filesystem.read_file \
  -H "Authorization: Bearer claude" \
  -d '{"params": {"path": "/tmp/test.txt"}}' | jq
```

### 3. Export MCP

Expose all governed tools as an MCP server over stdio JSON-RPC.

**Use case:** you want Claude Code or Cursor to use REST APIs or governed tools through a normal MCP interface.

```bash
./agent-mesh --mcp --openapi https://petstore.swagger.io/v2/swagger.json
```

Register it in Claude Code:

```bash
claude mcp add agent-mesh -- ./agent-mesh --mcp --config policies.yaml
```

```
Petstore (REST) → Import OpenAPI → Registry → Export MCP → Claude
                                        ↓
                                 policy + trace
```

### Combining modes

All modes can be combined.

You can:

- import REST APIs
- import MCP servers
- apply one policy engine
- expose the resulting unified tool surface over HTTP or MCP

```yaml
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
      - tools: ["get_pet_by_id", "find_pets_by_status"]
        action: allow
      - tools: ["filesystem.read_file", "filesystem.list_directory"]
        action: allow
      - tools: ["*"]
        action: deny
```

HTTP mode:

```bash
./agent-mesh --config policies.yaml \
  --openapi https://petstore.swagger.io/v2/swagger.json
```

MCP mode:

```bash
./agent-mesh --mcp --config policies.yaml \
  --openapi https://petstore.swagger.io/v2/swagger.json
```

## How it works

For each call to `POST /tool/{tool_name}`, Agent Mesh:

1. Extracts the agent identity from the request
2. Looks up the tool in the registry
3. Evaluates the policy
4. Forwards to the backend (HTTP for OpenAPI tools, JSON-RPC for MCP tools)
5. Records a trace
6. Returns the response

## Policies

Policies are defined in `policies.yaml`.

```yaml
port: 9090

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
        action: deny
```

### Policy actions

| Action | HTTP code | Behavior |
|--------|-----------|----------|
| `allow` | `200` | Forward the call to the backend and return the result |
| `deny` | `403` | Block the call and return a denial reason |
| `human_approval` | `202` | Mark the call as pending approval |

Default behavior is **fail closed**: if no rule matches, the call is denied.

### Evaluation model

Rules are evaluated **in order**. First match wins.

1. Find the first policy whose `agent` pattern matches the caller
2. Within that policy, find the first matching tool rule
3. If the rule has a `condition`, evaluate it
4. Return the rule action
5. If nothing matches, deny

### Conditions

```yaml
condition:
  field: "params.amount"
  operator: "<"
  value: 500
```

Supported operators: `<`, `<=`, `>`, `>=`, `==`, `!=`

### Agent patterns

| Pattern | Matches |
|---------|---------|
| `"support-*"` | `support-bot`, `support-agent-1` |
| `"admin-*"` | `admin-1`, `admin-ops` |
| `"claude"` | exactly `claude` |
| `"*"` | any agent |

### Tool patterns

```yaml
- tools: ["get_order", "get_customer"]     # specific tools
- tools: ["filesystem.read_file"]           # namespaced MCP tool
- tools: ["*"]                              # all tools
```

## API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/tool/{name}` | Proxy a tool call through policy evaluation |
| `GET` | `/tools` | List all registered tools |
| `GET` | `/mcp-servers` | List connected upstream MCP servers |
| `GET` | `/traces` | Query trace history (`?agent=...&tool=...`) |
| `GET` | `/health` | Health check and stats |

## CLI

### Main command

```bash
./agent-mesh [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `policies.yaml` | Path to YAML config |
| `--openapi` | | OpenAPI spec URL |
| `--backend` | | Backend base URL override |
| `--port` | from config or `9090` | Port override |
| `--mcp` | `false` | Export MCP mode |
| `--mcp-agent` | `claude` | Agent ID used for MCP-mode policy evaluation |

### Discover command

```bash
./agent-mesh discover [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | | Config YAML containing MCP servers |
| `--openapi` | | OpenAPI spec URL to inspect |
| `--backend` | | Backend base URL override |
| `--generate-policy` | `false` | Generate a suggested policy with read-only defaults |

## Project structure

```
agent-mesh/
├── main.go                # Entry point, wires everything
├── discover.go            # Discover subcommand
├── config/
│   └── config.go          # YAML config + policy + MCP server definitions
├── registry/
│   ├── registry.go        # Tool/Param types, Registry (Get/All/Remove)
│   ├── openapi.go         # Import OpenAPI → tool catalog
│   └── mcp.go             # Import MCP → tool catalog
├─��� policy/
│   └── engine.go          # Rule evaluation engine
���── proxy/
│   └── handler.go         # HTTP proxy (auth → policy → forward → trace)
├── mcp/
│   ├── server.go          # Export MCP (stdio JSON-RPC server)
│   ├── client.go          # Import MCP (connect to upstream servers)
│   └── manager.go         # Manages N upstream MCP connections
├── trace/
│   └─�� store.go           # In-memory trace store
└── policies.yaml          # Example policies
```

## Tests

Tests live next to source files, following normal Go conventions.

```bash
go test ./...                                          # Run all tests
go test ./... -race                                    # Run with race detector
go test ./... -v                                       # Run verbose
go test ./proxy/ -v                                    # Run one package
go test ./policy/ -run TestEvaluateMCPNamespacedTools -v  # Run one test
```

Coverage (49 tests):

| Package | Tests | Covers |
|---------|-------|--------|
| `config` | 5 | YAML parsing, defaults, MCP servers, conditions |
| `registry` | 10 | CRUD, loading, namespacing, mixed sources, concurrent access |
| `policy` | 8 | Allow/deny, conditions, wildcards, fail-closed behavior |
| `proxy` | 17 | REST and MCP calls, deny/error/approval flows, endpoints |
| `trace` | 6 | Record, filter, limit, eviction, stats |
| `mcp` | 13 | Client lifecycle, timeouts, cleanup, concurrent manager access |

## Roadmap

- [x] Import OpenAPI
- [x] Import MCP
- [x] Export MCP
- [x] Policy engine
- [x] Trace store with query API
- [x] `/mcp-servers` endpoint
- [x] Graceful shutdown
- [x] Discover command with policy generation
- [ ] SSE transport for Import MCP
- [ ] Agent credential format (JWT with scopes and budget)
- [ ] AsyncAPI support
- [ ] OpenTelemetry export
- [ ] Rate limiting per agent
- [ ] Cost tracking / token budget enforcement
- [ ] Dashboard UI
- [ ] Persistent trace store (PostgreSQL)

## License

Apache 2.0
