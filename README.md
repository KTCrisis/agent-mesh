# Agent Mesh

**Guardrail for AI agents.**
An open-source sidecar proxy that sits between AI agents and their tools — adding policy, human approval, and tracing without changing agent code.

One binary. One YAML config. Fail closed by default.

Works with Claude Code, Cursor, LangChain, CrewAI, or any agent that uses HTTP, MCP, or CLI tools.

## Architecture

```
                           agent-mesh (sidecar proxy)
                    ┌──────────────────────────────────────────┐
                    │                                          │
 ┌──────────┐      │  ┌──────────┐  ┌────────┐  ┌─────────┐  │      ┌──────────────────┐
 │  Claude   │─MCP─│─>│ Registry │─>│ Policy │─>│ Forward │──│─────>│ MCP servers       │
 │  Code     │<────│──│ (tools)  │  │ Engine │  │         │<─│─────<│ (filesystem,      │
 └──────────┘      │  └──────────┘  └────────┘  └─────────┘  │      │  gmail, weather,  │
                    │       ^            │            │         │      │  flights, ...)    │
 ┌──────────┐      │       │            v            │         │      └──────────────────┘
 │  CrewAI  │─HTTP─│─>     │       ┌────────┐        │         │
 │  agents  │<────│──      │       │Approval│        │         │      ┌──────────────────┐
 └──────────┘      │       │       │ Store  │        │         │      │ REST APIs         │
                    │       │       └────────┘        │──────── │─────>│ (OpenAPI specs)   │
 ┌──────────┐      │       │            │            │         │      └──────────────────┘
 │  Any     │─HTTP─│─>     │            v            │         │
 │  agent   │<────│──      │       ┌────────┐        │         │
 └──────────┘      │       │       │ Trace  │        │         │
                    │       │       │ Store  │        │         │
                    │       │       └────────┘        │         │
                    │       │                                   │
                    │  Import:          Export:                 │
                    │  - OpenAPI spec   - MCP server (stdio)   │
                    │  - MCP servers    - HTTP proxy (:port)   │
                    │    (stdio + SSE)                          │
                    │  - CLI tools                              │
                    │    (terraform,                            │
                    │     kubectl, ...)                         │
                    └──────────────────────────────────────────┘
```

### Request flow

Every tool call follows the same path, regardless of transport:

```
Agent calls tool
  → Extract agent identity
  → Rate limit check (calls/min, total budget, loop detection)
  → Look up tool in registry
  → Evaluate policy (allow / deny / human_approval)
  → Forward to upstream backend (HTTP, MCP, or CLI)
  → Record trace (agent, tool, params, decision, latency)
  → Return response
```

## The problem

When you connect tools directly to an AI agent, the agent gets unguarded access:

```bash
claude mcp add filesystem -- npx @modelcontextprotocol/server-filesystem /
claude mcp add github -- npx @modelcontextprotocol/server-github
claude mcp add database -- npx mcp-server-sqlite --db prod.db
```

No policy. No trace. No control. If something goes wrong, you may not even know what happened.

## The solution

Put Agent Mesh between the agent and its tools:

```bash
claude mcp add agent-mesh -- ./agent-mesh --mcp --config config.yaml
```

```
Claude ──> agent-mesh ──> filesystem (read: allow, write: approval, delete: deny)
                     ├──> gmail      (read: allow, send: approval, delete: deny)
                     ├──> weather    (allow)
                     └──> flights    (allow)
                        │
                  policy · approval · trace
```

The agent sees a normal tool surface. Agent Mesh enforces policy and records traces on every call.

## Install

### Download binary (recommended)

Grab the latest release for your platform:

```bash
# Get latest version tag
VERSION=$(curl -s https://api.github.com/repos/KTCrisis/agent-mesh/releases/latest | grep tag_name | cut -d '"' -f4)

# Linux (amd64)
curl -L "https://github.com/KTCrisis/agent-mesh/releases/download/${VERSION}/agent-mesh_${VERSION#v}_linux_amd64.tar.gz" | tar xz
sudo mv agent-mesh /usr/local/bin/

# Linux (arm64)
curl -L "https://github.com/KTCrisis/agent-mesh/releases/download/${VERSION}/agent-mesh_${VERSION#v}_linux_arm64.tar.gz" | tar xz
sudo mv agent-mesh /usr/local/bin/

# macOS (Apple Silicon)
curl -L "https://github.com/KTCrisis/agent-mesh/releases/download/${VERSION}/agent-mesh_${VERSION#v}_darwin_arm64.tar.gz" | tar xz
sudo mv agent-mesh /usr/local/bin/

# macOS (Intel)
curl -L "https://github.com/KTCrisis/agent-mesh/releases/download/${VERSION}/agent-mesh_${VERSION#v}_darwin_amd64.tar.gz" | tar xz
sudo mv agent-mesh /usr/local/bin/
```

All releases: [github.com/KTCrisis/agent-mesh/releases](https://github.com/KTCrisis/agent-mesh/releases)

### Build from source

Requires Go 1.24+:

```bash
git clone https://github.com/KTCrisis/agent-mesh.git
cd agent-mesh
go build -o agent-mesh .
```

## Quick start

### 1. Generate a config

```bash
# From an OpenAPI spec — turns any REST API into governed MCP tools
./agent-mesh discover --openapi https://petstore.swagger.io/v2/swagger.json --generate-policy > config.yaml

# Or from MCP servers already defined in a config
./agent-mesh discover --config config.yaml --generate-policy
```

This discovers all available tools and generates a safe starter policy (reads allowed, writes denied).

Or write one manually:

```yaml
# config.yaml
mcp_servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/home/me/projects"]

policies:
  - name: claude
    agent: "claude"
    rules:
      - tools: ["filesystem.read_*", "filesystem.list_*", "filesystem.search_*"]
        action: allow
      - tools: ["filesystem.write_file", "filesystem.edit_file"]
        action: human_approval
      - tools: ["filesystem.*"]
        action: deny

  - name: default
    agent: "*"
    rules:
      - tools: ["*"]
        action: deny
```

### 2. Plug into Claude Code

```bash
claude mcp add agent-mesh -- agent-mesh --mcp --config config.yaml
```

### 3. Use normally

Restart Claude Code. The agent sees the tools. Agent Mesh enforces the rules. Every call is traced.

## Features

### Policy engine

YAML-based, first-match-wins. Supports glob patterns for both agents and tools.

```yaml
policies:
  - name: support-agent
    agent: "support-*"
    rules:
      - tools: ["*.read_*", "*.list_*", "*.get_*"]
        action: allow
      - tools: ["create_refund"]
        action: allow
        condition:
          field: "params.amount"
          operator: "<"
          value: 500
      - tools: ["*"]
        action: deny
```

**Actions:**

| Action | Behavior |
|--------|----------|
| `allow` | Forward to backend, return result |
| `deny` | Block the call, return denial |
| `human_approval` | Require human approval before forwarding |

**Patterns:** `*` matches everything, `filesystem.*` matches all filesystem tools, `gmail.gmail_read_*` matches read operations. Uses Go's `filepath.Match` glob syntax.

**Fail closed:** no matching rule = deny.

### Rate limiting

Per-agent call limits to prevent runaway agents and infinite loops:

```yaml
policies:
  - name: support-agent
    agent: "support-*"
    rate_limit:
      max_per_minute: 30    # sliding window
      max_total: 1000       # lifetime budget (process lifetime)
    rules:
      - tools: ["get_order", "get_customer"]
        action: allow
```

Three protections:

| Protection | What it stops | Response |
|------------|--------------|----------|
| `max_per_minute` | Agent calling too fast (runaway loop) | HTTP 429 |
| `max_total` | Agent exhausting its budget over time | HTTP 429 |
| Loop detection | Same tool + same params > 3x in 10s | HTTP 429 `loop_detected` |

Loop detection is always active. Rate limits are optional per policy. Both show up in traces as `"rate_limited"`.

### Human approval

When a policy requires `human_approval`, the flow is **non-blocking**:

```
Claude calls filesystem.write_file
  → agent-mesh returns: "Approval required (id: a1b2c3d4)"
  → Claude shows the message, asks user for confirmation
  → Claude calls approval.resolve(id: a1b2c3d4, decision: approve)
  → agent-mesh replays the original tool call
  → Result returned to Claude
```

No freeze, no second terminal needed. The agent stays responsive.

Virtual MCP tools handle governance:

| Tool | Description |
|------|-------------|
| `approval.resolve` | Approve or deny a pending request. On approve, replays the original tool call. |
| `approval.pending` | List all pending approval requests. |

Approvals can also be resolved via:
- **CLI:** `mesh approve <id>` from another terminal
- **HTTP API:** `POST /approvals/{id}/approve`

Configurable timeout (default 5 min). Timeout = deny.

### Temporal grants

When you're approving the same tool repeatedly, create a temporary override — like `sudo` for agents:

```
You: "Grant filesystem.write_* for 30 minutes"
Claude: grant.create {tools: "filesystem.write_*", duration: "30m"}
  → Grant a1b2c3d4 created, expires in 30m
  → All filesystem.write_* calls now bypass approval
  → Traced as "grant:a1b2c3d4" (full audit trail)
```

Three virtual MCP tools:

| Tool | Description |
|------|-------------|
| `grant.create` | Create a temporal grant (tools pattern + duration) |
| `grant.list` | List all active grants |
| `grant.revoke` | Revoke a grant by ID |

Also available via HTTP API:

```bash
# Create a grant
curl -X POST http://localhost:9090/grants \
  -d '{"agent":"claude","tools":"filesystem.write_*","duration":"30m"}'

# List active grants
curl http://localhost:9090/grants

# Revoke
curl -X DELETE http://localhost:9090/grants/a1b2c3d4
```

Grants expire automatically. No config change needed. Every call that uses a grant is traced with the grant ID.

### CLI tool governance

Wrap any CLI binary (terraform, kubectl, docker, gh, aws…) behind policy, approval, and tracing. Three modes:

```yaml
cli_tools:
  # Simple — all subcommands, default_action applies
  - name: gh
    bin: gh
    default_action: allow

  # Fine-tuned — specific commands with overrides
  - name: terraform
    bin: terraform
    default_action: human_approval
    commands:
      plan:
        timeout: 120s
      apply:
        allowed_args: ["-target"]

  # Strict — only declared commands, everything else denied
  - name: kubectl
    bin: kubectl
    strict: true
    commands:
      get:
        allowed_args: ["-n", "--namespace", "-o", "--output"]
```

Security: no shell execution (`exec.Command()` directly), argument allowlists, metacharacter rejection, timeout enforcement, env isolation, output cap (1MB).

Agents call CLI tools like any other MCP tool — `terraform.plan`, `kubectl.get`, `gh.pr.list`. Same policy rules, same approval flow, same traces.

See [docs/cli-tools.md](docs/cli-tools.md) for full documentation.

### Supervisor protocol

Agent Mesh exposes a rich approval API so that external **supervisor agents** can review and resolve approvals on behalf of humans — handling the 80% of routine decisions and escalating the rest.

The supervisor is not built into agent-mesh. It's any external process that polls `GET /approvals` and calls `POST /approvals/{id}/approve` or `/deny`. Agent-mesh provides the protocol; you bring the logic.

**Tool filtering** — domain-specific supervisors watch only their scope:

```bash
# Filesystem supervisor
curl http://localhost:9090/approvals?status=pending&tool=filesystem.*

# Gmail supervisor
curl http://localhost:9090/approvals?status=pending&tool=gmail.*
```

**Context enrichment** — `GET /approvals/{id}` includes the agent's recent trace history and active grants, giving the supervisor evaluation context:

```bash
curl http://localhost:9090/approvals/a1b2c3d4 | jq '.recent_traces, .active_grants'
```

**Structured verdicts** — approve or deny with reasoning and confidence:

```bash
curl -X POST http://localhost:9090/approvals/a1b2c3d4/approve \
  -d '{"resolved_by":"agent:supervisor","reasoning":"path within sandbox","confidence":0.95}'
```

Reasoning and confidence are stored in traces for audit and calibration.

**Content isolation** — when `supervisor.expose_content: false`, raw param content is replaced with structural metadata (length, SHA256, type). The supervisor sees structure, not content — shrinking the prompt injection attack surface:

```yaml
supervisor:
  expose_content: false
```

**Injection detection** — every approval view includes an `injection_risk` flag when suspicious patterns are found in tool params (e.g., "ignore previous instructions", "override policy").

See [docs/supervisor-protocol.md](docs/supervisor-protocol.md) for the full protocol reference, evaluation guidelines, and how to build your own supervisor.

### Tracing

Every tool call is logged with agent, tool, params, policy decision, latency, and approval metadata.

```bash
# Query via HTTP API
curl http://localhost:9090/traces?agent=claude&tool=filesystem.write_file | jq

# Read from JSONL file
cat traces.jsonl | jq 'select(.policy == "deny")'

# Approval analytics
cat traces.jsonl | jq 'select(.approval_status == "approved")'
```

### OpenTelemetry export

Export every trace as OTLP spans — to a file, stdout, or any OTLP-compatible backend (Jaeger, Grafana Tempo, Datadog).

```yaml
# JSONL file (zero infra, scriptable)
otel_endpoint: /path/to/traces-otel.jsonl

# OTLP HTTP (Jaeger, Tempo, OTEL Collector)
otel_endpoint: http://localhost:4318

# Debug (spans on stderr)
otel_endpoint: stdout
```

Zero new dependencies. Each span includes `agent.id`, `tool.name`, `policy.action`, `approval.*`, and `llm.token.*` attributes. See [docs/otel.md](docs/otel.md) for details.

### Tool discovery

Auto-discover tools from upstream sources and generate starter policies:

```bash
# Discover tools from MCP servers in config
./agent-mesh discover --config config.yaml

# Generate a safe starter policy (reads allowed, writes denied)
./agent-mesh discover --config config.yaml --generate-policy

# Discover from an OpenAPI spec
./agent-mesh discover --openapi https://petstore.swagger.io/v2/swagger.json --generate-policy
```

### CLI (`mesh`)

Manage approvals from a separate terminal:

```bash
mesh pending                    # List pending approvals
mesh show a1b2c3d4              # Full details with params
mesh approve a1b2c3d4           # Approve by ID prefix
mesh deny a1b2c3d4              # Deny by ID prefix
mesh watch                      # Interactive mode — approve/deny as they come
```

## The 3 modes

```
┌─────────────────────────────────────────────────────────┐
│                     agent-mesh                          │
│                                                         │
│  ┌─────────────┐     ┌──────────┐     ┌─────────────┐  │
│  │ IMPORT      │     │          │     │ EXPORT      │  │
│  │ OpenAPI     │────>│ Registry │────>│ MCP server  │  │
│  │ (Swagger)   │     │ (tools)  │     │ (stdio)     │  │
│  └─────────────┘     │          │     └──────┬──────┘  │
│  ┌─────────────┐     │          │            │         │
│  │ IMPORT      │────>│          │            v         │
│  │ MCP servers │     │          │     Claude, Cursor,  │
│  │ (stdio/SSE) │     └──────────┘     any MCP client   │
│  └─────────────┘          │                            │
│                     policy · approval · trace           │
└─────────────────────────────────────────────────────────┘
```

### Import OpenAPI

Turn any REST API into governed tools:

```bash
./agent-mesh --mcp --openapi https://petstore.swagger.io/v2/swagger.json --config config.yaml
```

### Import MCP

Connect to upstream MCP servers (stdio or SSE), discover tools, add governance:

```yaml
mcp_servers:
  - name: filesystem
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/home/me"]

  - name: remote-service
    transport: sse
    url: "https://mcp-server.example.com/sse"
    headers:
      Authorization: "Bearer <token>"
```

### Export MCP

Expose all governed tools as a standard MCP server for Claude Code, Cursor, or any MCP client:

```bash
claude mcp add agent-mesh -- ./agent-mesh --mcp --config config.yaml
```

All modes compose. Import REST + MCP, apply one policy, export as unified MCP.

## Example: travel agent

A multi-tool agent with zero API keys, zero code — just YAML:

```yaml
mcp_servers:
  - name: weather
    transport: stdio
    command: npx
    args: ["-y", "open-meteo-mcp-server"]

  - name: flights
    transport: stdio
    command: npx
    args: ["-y", "google-flights-mcp-server"]

  - name: travel
    transport: stdio
    command: npx
    args: ["-y", "travel-mcp"]

policies:
  - name: claude
    agent: "claude"
    rules:
      - tools: ["weather.*", "flights.*", "travel.*"]
        action: allow

  - name: default
    agent: "*"
    rules:
      - tools: ["*"]
        action: deny
```

Result: Claude searches flights, checks weather, estimates budgets — all traced through agent-mesh.

## API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/tool/{name}` | Proxy a tool call through policy |
| `GET` | `/tools` | List all registered tools |
| `GET` | `/mcp-servers` | List connected upstream MCP servers |
| `GET` | `/traces` | Query trace history (`?agent=...&tool=...`) |
| `GET` | `/approvals` | List approvals (`?status=pending`, `?tool=filesystem.*`) |
| `GET` | `/approvals/{id}` | Approval detail with agent context (recent traces, active grants) |
| `POST` | `/approvals/{id}/approve` | Approve (optional: `reasoning`, `confidence`) |
| `POST` | `/approvals/{id}/deny` | Deny (optional: `reasoning`, `confidence`) |
| `GET` | `/grants` | List active temporal grants |
| `POST` | `/grants` | Create a temporal grant |
| `DELETE` | `/grants/{id}` | Revoke a grant |
| `GET` | `/health` | Health check and stats |

## CLI flags

```bash
./agent-mesh [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `config.yaml` | Path to YAML config |
| `--openapi` | | OpenAPI spec URL |
| `--backend` | | Backend base URL override |
| `--port` | from config or `9090` | Port override |
| `--mcp` | `false` | Export MCP mode (stdio) |
| `--mcp-agent` | `claude` | Agent ID for MCP-mode policy evaluation |

## Project structure

```
agent-mesh/
├── main.go                # Entry point, wires everything
├── discover.go            # Discover subcommand + policy generation
├── config/
│   └── config.go          # YAML config + policy + MCP server definitions
├── registry/
│   ├── registry.go        # Tool/Param types, Registry (Get/All/Remove)
│   ├── openapi.go         # Import OpenAPI → tool catalog
│   ├── mcp.go             # Import MCP → tool catalog
│   └── cli.go             # Import CLI tools → tool catalog
├── exec/
│   └── runner.go          # Secure CLI execution (no shell, arg validation, timeout)
├── policy/
│   └── engine.go          # Rule evaluation engine (glob patterns, conditions)
├── grant/
│   └── store.go           # Temporal grants (sudo for agents, TTL-based)
├── ratelimit/
│   └── limiter.go         # Per-agent rate limiting + loop detection
├── supervisor/
│   ├── content.go         # Content isolation (redact params → metadata)
│   └── injection.go       # Prompt injection detection in tool params
├── proxy/
│   └── handler.go         # HTTP proxy (auth → rate limit → policy → forward → trace)
├── mcp/
│   ├── server.go          # Export MCP (stdio JSON-RPC, virtual approval tools)
│   ├── client.go          # Import MCP (connect to upstream, stdio + SSE)
│   ├── manager.go         # Manages N upstream MCP connections
│   ├── transport.go       # Transport abstraction
│   └── transport_sse.go   # SSE transport implementation
├── approval/
│   └── store.go           # Channel-based approval store with timeout
├── trace/
│   ├── store.go           # In-memory trace store + JSONL persistence
│   └── otel.go            # OpenTelemetry OTLP exporter (file, stdout, HTTP)
├── cmd/mesh/              # CLI binary (pending/approve/deny/watch)
├── examples/              # Example config files
│   ├── filesystem.yaml    # Filesystem governance (read/write/deny)
│   ├── petstore.yaml      # OpenAPI import demo (Petstore)
│   ├── travel-agent.yaml  # Multi-tool travel agent
│   └── cli-tools/         # CLI tool governance (terraform, kubectl, gh)
└── docs/
    ├── agent-landscape.md # AI agent CLI landscape survey
    ├── otel.md            # OpenTelemetry export guide
    └── positioning.md     # Market positioning and comparisons
```

## Tests

```bash
go test ./...              # Run all tests
go test ./... -race        # With race detector
go test ./proxy/ -v        # One package
```

222 tests across 14 packages:

| Package | Tests | Covers |
|---------|-------|--------|
| `config` | 16 | YAML parsing, defaults, MCP servers, conditions, CLI tools, supervisor config |
| `registry` | 16 | CRUD, loading, namespacing, concurrent access, CLI modes, ResolveCLI |
| `policy` | 9 | Allow/deny, conditions, wildcards, globs, fail-closed |
| `proxy` | 36 | REST, MCP, CLI calls, approval flows, supervisor protocol, content redaction |
| `exec` | 30 | Arg validation, shell injection, timeout, output cap, env isolation |
| `grant` | 8 | Create, check, revoke, expiration, cleanup, glob matching |
| `ratelimit` | 8 | Per-minute, total budget, loop detection, agent isolation |
| `trace` | 14 | Record, filter, eviction, stats, JSONL persistence, supervisor fields, OTEL export |
| `mcp` | 33 | Client lifecycle, timeouts, SSE transport, approval flow, supervisor mode |
| `supervisor` | 30 | Content redaction, type detection, injection detection (positive/negative) |
| `approval` | 17 | Submit, resolve, timeout, prefix match, concurrent, notify |

## Roadmap

- [x] Import OpenAPI
- [x] Import MCP (stdio + SSE)
- [x] Export MCP (stdio)
- [x] Policy engine with glob patterns
- [x] Human approval (non-blocking, virtual MCP tools)
- [x] Approval CLI (`mesh` binary)
- [x] Trace store with query API + JSONL persistence
- [x] Tool discovery + policy generation
- [ ] JWT agent credentials (scopes + budget)
- [x] Rate limiting per agent (sliding window + total budget + loop detection)
- [x] Temporal grants (sudo for agents — `grant.create` MCP tool + HTTP API)
- [x] Async approval (202 + poll via MCP virtual tools, HTTP API)
- [x] CLI tool governance (terraform, kubectl, etc. — 3 modes, arg validation, secure exec)
- [x] Supervisor agent protocol (structured verdicts, content isolation, injection detection)
- [x] OpenTelemetry trace export (OTLP JSON — file, stdout, HTTP)
- [ ] Dashboard UI

## Why "Agent Mesh"

The same way Envoy sits between microservices and adds observability, auth, and rate limiting without changing service code — Agent Mesh sits between AI agents and their tools.

Agents don't know the proxy exists. They call tools, get results. The governance layer is invisible to the agent, visible to the operator.

| It is | It is not |
|-------|-----------|
| A policy + governance layer for tool calls | An API gateway |
| A lightweight local sidecar binary | An agent framework |
| Config-as-code with YAML | A cloud platform |
| An observability layer for agent actions | An MCP hosting service |

## License

Apache 2.0
