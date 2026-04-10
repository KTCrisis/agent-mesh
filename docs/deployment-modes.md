# Deployment Modes

Agent-mesh can run in different configurations depending on how many agents and services need to connect.

## Mode 1: Embedded (current default)

Claude Code (or any MCP client) launches agent-mesh as a stdio subprocess. Agent-mesh starts an HTTP API on `:9090` in the background.

```
Claude Code ──stdio──> agent-mesh ──> upstream MCP servers
                           │
                      :9090 HTTP (background)
```

**Pros:** Zero setup, single process, dies with Claude.
**Cons:** Ephemeral — stops when Claude quits. Only one instance per port.

### With supervisor

The supervisor polls the background HTTP API while Claude is running:

```
Claude Code ──stdio──> agent-mesh :9090 ──> filesystem, gmail, ollama...
                           │
                    supervisor (poll GET /approvals)
                    ├── rules → approve/deny (0ms)
                    └── ollama → evaluate (~20s)
```

**How to run:**
```bash
# Terminal 1: Claude Code (launches agent-mesh automatically)
claude

# Terminal 2: Supervisor
cd ~/agent7
python -m backend.app.services.supervisor --config supervisor.local.yaml
```

The supervisor retries every `poll_interval` until agent-mesh is up. When Claude quits, agent-mesh stops and the supervisor waits for reconnection.

## Mode 2: Standalone HTTP

Agent-mesh runs as a standalone HTTP server. Agents connect via HTTP.

```
Agent A ──HTTP──> agent-mesh :9090 ──> upstream MCP servers
Agent B ──HTTP──┘        │
                    supervisor (poll)
```

**How to run:**
```bash
agent-mesh --config config.yaml
```

**Pros:** Persistent, shared across agents.
**Cons:** Agents must use the HTTP API, not MCP stdio.

## Mode 3: Multiple instances (port isolation)

Each agent gets its own agent-mesh instance on a different port.

```
Claude Code ──stdio──> agent-mesh :9090
Agent B ──HTTP──────> agent-mesh :9091
                           │
                    supervisor (polls both? or one?)
```

**How to run:**
```bash
# Instance 1 (launched by Claude via MCP)
# port: 9090 in config

# Instance 2
agent-mesh --config config-agent-b.yaml --port 9091
```

**Pros:** Full isolation, no conflicts.
**Cons:** No shared governance — each instance has its own policies, traces, approvals.

## Port conflicts

Two agent-mesh instances on the same port will fail. This happens when:

- Claude Code launches agent-mesh (`:9090`), and you manually launch another
- Two Claude sessions with the same agent-mesh MCP config run simultaneously

**Detection:**
```bash
lsof -i :9090
```

**Fix:** Use different ports per instance, or use a single shared instance.

## Future: Daemon mode

The ideal architecture for multi-agent + supervisor:

```
                    agent-mesh (daemon, persistent)
                    ┌─────────────────────────────────┐
Claude Code ──MCP──>│                                 │──> filesystem
Agent B ────HTTP──>│  registry · policy · approval   │──> gmail
Agent C ────HTTP──>│                                 │──> ollama
                    └────────────┬────────────────────┘
                                 │
                          supervisor (poll)
```

Agent-mesh runs as a persistent background service (`agent-mesh serve`). All agents — Claude Code, LangChain, CrewAI, custom scripts — connect to the same instance. One set of policies, one trace store, one approval queue, one supervisor.

**What's needed:**

| Feature | Status |
|---------|--------|
| HTTP mode (standalone server) | Done |
| MCP stdio mode (embedded in Claude) | Done |
| Background HTTP in MCP mode | Done (`:9090` alongside stdio) |
| Daemon mode (`agent-mesh serve`) | Not yet |
| MCP client reconnection (Claude → running daemon) | Not yet |
| Multi-instance supervisor (poll N meshes) | Not yet |

### Why daemon mode matters

- **Supervisor stability** — supervisor needs a persistent endpoint, not one that dies with Claude
- **Shared governance** — all agents governed by the same policies and approval queue
- **Trace aggregation** — one trace store for all agent activity
- **Grant sharing** — a temporal grant applies to all agents, not just one session
- **Approval dedup** — one approval queue, one supervisor, no duplicates

### Implementation sketch

```bash
# Start daemon (background, persistent)
agent-mesh serve --config config.yaml --pid-file /tmp/agent-mesh.pid

# Claude Code connects as MCP client to running daemon
claude mcp add agent-mesh -- agent-mesh connect --url http://localhost:9090

# Supervisor polls the same daemon
python -m backend.app.services.supervisor --config supervisor.yaml

# Stop daemon
agent-mesh stop --pid-file /tmp/agent-mesh.pid
```

`agent-mesh connect` would be a thin MCP stdio proxy that translates MCP JSON-RPC to HTTP calls against the running daemon. Claude sees normal MCP tools; the daemon handles everything.
