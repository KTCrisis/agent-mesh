# Agent Mesh

**The Envoy of AI agents.** A lightweight sidecar proxy that sits between AI agents and backend APIs, adding policy enforcement, observability, and access control — without changing agent code.

```
Without Agent Mesh:          With Agent Mesh:
Agent → API                  Agent → Sidecar → API
(ungoverned)                          ↓
                              auth · policy · trace
```

## Why

AI agents (LangChain, CrewAI, Claude, custom) can call APIs. But there's no standard way to control **what** they call, **who** can call it, and **what happened**. Agent Mesh solves this with a single proxy.

- **Framework agnostic** — works with any agent that makes HTTP calls
- **Policy as code** — YAML rules, versionable in git, reviewable in PRs
- **Fail closed** — no matching policy = denied
- **Trace everything** — every tool call is logged with agent, params, decision, latency
- **Single binary** — `go build`, no runtime, no containers required

## Quick start

```bash
# Build
go build -o agent-mesh .

# Run with the Petstore demo API
./agent-mesh \
  --config policies.yaml \
  --openapi https://petstore.swagger.io/v2/swagger.json

# In another terminal:

# List discovered tools (20 from Petstore)
curl http://localhost:9090/tools | jq

# Allowed call (support agent, read)
curl -X POST http://localhost:9090/tool/get_pet_by_id \
  -H "Authorization: Bearer support-bot" \
  -d '{"params": {"petId": 1}}' | jq

# Denied call (support agent, delete)
curl -X POST http://localhost:9090/tool/delete_pet \
  -H "Authorization: Bearer support-bot" \
  -d '{"params": {"petId": 1}}' | jq

# Denied call (anonymous — fail closed)
curl -X POST http://localhost:9090/tool/get_pet_by_id \
  -d '{"params": {"petId": 1}}' | jq

# View traces
curl http://localhost:9090/traces | jq

# Health + stats
curl http://localhost:9090/health | jq
```

## How it works

```
POST /tool/{tool_name}
  1. Extract agent ID from Authorization header
  2. Look up tool in registry (OpenAPI spec → tool catalog)
  3. Evaluate policy (YAML rules → allow / deny / human_approval)
  4. Forward request to backend API
  5. Log trace (agent, tool, params, result, policy, latency)
  6. Return response to agent
```

## Policies

Define who can do what in `policies.yaml`:

```yaml
port: 9090

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
| `GET` | `/tools` | List all registered tools |
| `GET` | `/traces` | Query trace history (`?agent=...&tool=...`) |
| `GET` | `/health` | Health check + stats |

## Architecture

```
agent-mesh/
├── main.go              # Entry point, wires everything
├── config/config.go     # YAML config + policy loader
├── registry/openapi.go  # OpenAPI → tool catalog
├── policy/engine.go     # Rule evaluation engine
├── proxy/handler.go     # HTTP proxy (auth → policy → forward → trace)
├── trace/store.go       # In-memory trace store
└── policies.yaml        # Example policies
```

## Roadmap

- [ ] MCP server mode (expose tools as MCP for Claude, Cursor, etc.)
- [ ] Agent credential format (JWT with scopes + budget)
- [ ] AsyncAPI support (event-driven tools via Kafka)
- [ ] OpenTelemetry trace export
- [ ] Rate limiting per agent
- [ ] Cost tracking (token budget enforcement)
- [ ] Dashboard UI
- [ ] Persistent trace store (PostgreSQL / Kafka)

## License

Apache 2.0
