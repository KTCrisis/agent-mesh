# Deployment Modes

Agent-mesh can run in different configurations depending on how many agents and services need to connect.

## Mode 1: Embedded (Claude Code default)

Claude Code launches agent-mesh as a stdio subprocess. Agent-mesh starts an HTTP API on `:9090` in the background.

```
Claude Code ──stdio──> agent-mesh ──> upstream MCP servers
                           │
                      :9090 HTTP (background)
```

**Pros:** Zero setup, single process.
**Cons:** Ephemeral — stops when Claude quits. Only one instance per port.

## Mode 2: Supervisor-managed (recommended)

The supervisor is the persistent process. It spawns agent-mesh automatically, monitors it, and restarts it on crash. Agent-mesh lives as long as the supervisor lives — independent of Claude sessions.

```
supervisor (always alive)
  │
  ├── spawn/restart ──> agent-mesh :9090 ──> filesystem, gmail, ollama-mcp, memory-mcp
  │                          │
  ├── poll ─────────────── GET /approvals
  ├── evaluate ─────────── rules (0ms) or ollama (~20s)
  ├── resolve ─────────── POST /approvals/{id}/approve|deny
  ├── store ───────────── POST /tool/memory.memory_store
  └── recall ──────────── POST /tool/memory.memory_recall (on startup)
                             │
Claude Code ──stdio or HTTP──┘  (connects to running agent-mesh)
Agent B ──────────HTTP───────┘
```

### How to run

**One terminal — that's it:**

```bash
cd ~/agent7
python -m backend.app.services.supervisor --config supervisor.local.yaml
```

The supervisor:

1. Starts and checks if agent-mesh is running on `:9090`
2. Not there → **spawns** `agent-mesh --config <path>`
3. Waits for health check (`GET /health`) to respond
4. **Recalls** previous decisions from memory-mcp
5. Enters poll loop — evaluates and resolves approvals
6. Agent-mesh crashes → **auto-restarts** within seconds
7. Stores every decision in memory-mcp for context across sessions
8. Claude Code connects whenever it starts — agent-mesh is already running

### Configuration

```yaml
supervisor:
  mesh_url: http://localhost:9090
  agent_id: my-supervisor
  poll_interval: 2s

  # Auto-spawn agent-mesh
  mesh_process:
    enabled: true
    command: agent-mesh
    config: /path/to/my-flow.local.yaml
    restart_delay: 5

  # Store/recall decisions via memory-mcp
  memory:
    enabled: true
    store_decisions: true
    recall_on_start: true
    recall_limit: 20
    tags: ["supervisor", "decision"]

  # LLM fallback for ambiguous cases
  ollama:
    enabled: true
    url: http://localhost:11434
    model: qwen3:14b

  # Fast path rules (no LLM needed)
  project_dirs: [/home/user]
  rules:
    - name: home-writes
      condition: "params.path starts_with /home/user"
      action: approve
      confidence: 0.95
    - name: deny-system
      condition: "params.path starts_with /etc"
      action: deny
      confidence: 0.99
    # catch-all → Ollama evaluates (auto-appended)
```

### What happens when Claude quits

| Before (Mode 1) | After (Mode 2) |
|-----------------|-----------------|
| agent-mesh dies with Claude | agent-mesh stays alive (supervisor manages it) |
| Supervisor loses connection, waits | Supervisor never loses connection |
| Context lost between sessions | Decisions stored in memory-mcp, recalled on next start |
| Next Claude session spawns new agent-mesh | Next Claude session connects to existing agent-mesh |

### Decision flow

```
Approval arrives
  │
  ├─ injection_risk? ──────────────────→ ESCALATE (0ms)
  │
  ├─ Rule matches? ────────────────────→ APPROVE/DENY (0ms, no LLM)
  │
  ├─ Ollama enabled? ──→ LLM evaluate
  │   ├─ confidence ≥ threshold ───────→ APPROVE/DENY (~20s)
  │   ├─ confidence < threshold ───────→ ESCALATE
  │   └─ Ollama down ─────────────────→ ESCALATE
  │
  └─ Ollama disabled? ────────────────→ ESCALATE (human decides)
```

Every decision is:
- Logged to `supervisor-decisions.jsonl` (JSONL audit trail)
- Stored in memory-mcp (recalls on next startup)
- Traced in agent-mesh (with `supervisor_reasoning` and `supervisor_confidence`)

## Mode 3: Standalone HTTP

Agent-mesh runs as a standalone HTTP server without a supervisor. Agents connect via HTTP directly.

```
Agent A ──HTTP──> agent-mesh :9090 ──> upstream MCP servers
Agent B ──HTTP──┘
```

**How to run:**
```bash
agent-mesh --config config.yaml
```

**Pros:** Simple, persistent.
**Cons:** No automatic approval resolution. All `human_approval` requests wait for manual resolution via CLI (`mesh approve`) or HTTP API.

## Mode 4: Multiple instances (port isolation)

Each agent gets its own agent-mesh instance on a different port.

```
Claude Code ──stdio──> agent-mesh :9090
Agent B ──HTTP──────> agent-mesh :9091
```

**How to run:**
```bash
# Instance 1 (config: port 9090)
agent-mesh --config config-a.yaml

# Instance 2
agent-mesh --config config-b.yaml --port 9091
```

**Pros:** Full isolation, no conflicts.
**Cons:** No shared governance — each instance has its own policies, traces, approvals. Run one supervisor per instance if needed.

## Port conflicts

Two agent-mesh instances on the same port will fail. This happens when:

- Claude Code launches agent-mesh (`:9090`), and the supervisor also spawns one
- Two Claude sessions with the same agent-mesh MCP config run simultaneously

**Detection:**
```bash
lsof -i :9090
```

**Prevention:** Use supervisor-managed mode (Mode 2). The supervisor checks if agent-mesh is already running before spawning. If Claude already launched agent-mesh, the supervisor simply connects to it instead of spawning a new one.

## Architecture summary

| Component | Role | Lifecycle |
|-----------|------|-----------|
| **Ollama** | Local LLM | System daemon (always running) |
| **agent-mesh** | Policy + approval + trace proxy | Managed by supervisor (or Claude) |
| **filesystem, gmail, ollama-mcp, memory-mcp** | Upstream MCP servers | Launched by agent-mesh as subprocesses |
| **supervisor** | Approval evaluator + process manager | Persistent (user launches once) |
| **Claude Code** | AI agent | Ephemeral (user sessions) |

```
                    supervisor (persistent)
                         │
                    ┌────┴────┐
                    │ spawn   │ poll/resolve
                    ▼         ▼
              agent-mesh :9090
              ┌──────────────────────────────────┐
              │  registry · policy · approval    │
              │  trace · grants · rate limiting  │
              └──┬───┬───┬───┬───┬───┬──────────┘
                 │   │   │   │   │   │
                 ▼   ▼   ▼   ▼   ▼   ▼
           fs  gmail weather ollama memory ...
                 ▲               ▲      ▲
                 │               │      │
              Claude Code    supervisor (LLM eval + decision store)
```
