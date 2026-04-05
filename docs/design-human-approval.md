# Design: Human Approval

**Status**: Implemented (v0.3.2)
**Author**: Marc Verchiani
**Date**: 2026-04-05

---

## Problem

Agent-mesh gates tool calls with `human_approval` policies, but the gate is a dead end: the request is blocked and traced, but there is no mechanism for a human to approve, deny, or escalate. The agent receives a static message and cannot proceed.

For agent-mesh to deliver on its "Envoy of AI agents" promise, the approval flow must be **complete** — block, notify, wait, resolve, forward.

## Design Goals

1. **Zero agent code change** — the agent sees a slow response or a pending status, not a new protocol
2. **CLI-first** — primary UX is a developer in a terminal
3. **Agent-agnostic** — works identically for Claude Code, Cursor, LangChain, raw HTTP
4. **Autonomy-preserving** — agents that can handle async should not be blocked
5. **Fail-closed** — timeout = deny
6. **Auditable** — every approval/denial is traced with who, when, from where

## Async-First, One Store

All approval flows are **non-blocking**. The handler returns immediately with a pending approval ID. Resolution happens out-of-band via virtual MCP tools or HTTP API.

| Transport | Agent type | Behavior |
|-----------|------------|----------|
| **MCP stdio** | Interactive (Claude Code, Cursor) | Immediate response with approval ID, agent resolves via `approval.resolve` tool |
| **MCP stdio** | Interactive, TTY available | TTY prompt via `/dev/tty`, inline approve/deny (no store needed) |
| **HTTP** | Autonomous (pipelines, background agents) | 202 immediate, agent polls or receives callback |

The approval store is shared. Only the handler integration differs.

### MCP Mode — Interactive agents (v0.3.2)

```
Agent (Claude Code)                    agent-mesh                        Human
         |                                      |                               |
         |-- tools/call ----------------------->|                               |
         |                                      |-- policy: human_approval      |
         |                                      |-- create PendingApproval      |
         |<-- "Approval required (id: abc)" ----|                               |
         |                                      |                               |
         |-- (shows message to user)            |                               |
         |                                      |                               |
         |-- approval.resolve(abc, approve) --->|                               |
         |                                      |-- forward to upstream         |
         |<-- result (tool output) -------------|                               |
```

The agent receives a text response with the approval ID and instructions. An LLM-based agent (Claude Code) understands the message and calls `approval.resolve` to approve/deny — optionally after asking the human. The tool execution happens inside `approval.resolve`, which replays the original call and returns the upstream result.

Key insight: MCP is request/response, but **the response doesn't have to block**. Returning a pending message and letting the agent self-resolve via a virtual tool keeps the connection alive and the agent responsive.

### TTY Mode — Inline prompt (fallback for interactive terminals)

When agent-mesh detects a real terminal (not piped stdin), it prompts via `/dev/tty` before falling back to the store:

```
>> APPROVAL REQUIRED
   agent: claude  tool: filesystem.write_file
   [a]pprove / [d]eny ? a
```

This is skipped in MCP stdio mode (Claude Code) because `/dev/tty` would open in the agent-mesh process terminal, not the caller's terminal.

### HTTP Mode — Autonomous agents

```
Autonomous agent                       agent-mesh                        Human
      |                                      |                               |
      |-- POST /tool/write_file ------------>|                               |
      |<-- 202 {id: "abc", status: pending} -|-- notify -------------------->|
      |                                      |                               |
      |-- (continues other work)             |                               |
      |                                      |                               |
      |                                      |<-- mesh approve abc ----------|
      |                                      |-- forward write_file          |
      |                                      |-- store result                |
      |                                      |                               |
      |-- GET /approvals/abc/result -------->|                               |
      |<-- 200 {status: approved, result} ---|                               |
```

### The agent that replans

A well-designed agent adapts when it hits an approval gate:

```
Agent: "I need to write config.yaml then run tests"
  1. write_file config.yaml → "Approval required (id: abc)"
  2. Agent asks user for confirmation, calls approval.resolve(abc, approve)
  3. Result comes back, agent continues with tests
```

This is the agent's responsibility, not the sidecar's. Agent-mesh exposes the constraint, the agent decides how to handle it. The sidecar doesn't prescribe agent behavior — it governs tool access.

## Core Concept: Approval Store

A concurrent-safe in-memory store that holds pending approvals. Each entry contains a Go channel that blocks the handler goroutine until a resolution arrives.

```go
// approval/store.go

type Status string

const (
    StatusPending  Status = "pending"
    StatusApproved Status = "approved"
    StatusDenied   Status = "denied"
    StatusTimeout  Status = "timeout"
)

type PendingApproval struct {
    ID         string
    TraceID    string            // link to trace entry
    AgentID    string
    Tool       string
    Params     map[string]any
    CreatedAt  time.Time
    Timeout    time.Duration
    Result     chan Resolution    // blocks handler until resolved
}

type Resolution struct {
    Status     Status
    ResolvedBy string            // "cli:marc", "http:10.0.0.1", "timeout"
    ResolvedAt time.Time
}

type Store struct {
    mu       sync.RWMutex
    pending  map[string]*PendingApproval
    resolved []ResolvedEntry     // audit log
    timeout  time.Duration       // default timeout
}

func (s *Store) Submit(agentID, tool string, params map[string]any, traceID string) *PendingApproval
func (s *Store) Approve(id string, by string) error
func (s *Store) Deny(id string, by string) error
func (s *Store) Pending() []*PendingApproval
func (s *Store) Get(id string) *PendingApproval
func (s *Store) Result(id string) (*ResolvedEntry, error)  // retrieve result after async approval
```

`Submit` creates a `PendingApproval` with a buffered channel (cap 1) and starts a timeout goroutine.

- **Sync path**: the handler reads from `pending.Result` — it blocks until `Approve`, `Deny`, or timeout writes to it. On approval, the handler forwards to upstream and returns the result inline.
- **Async path**: the handler returns 202 immediately. On approval, agent-mesh forwards to upstream, stores the result in a `ResolvedEntry`, and optionally calls back the agent. The agent retrieves the result via `GET /approvals/{id}/result`.

## Integration Points

### MCP Server (stdio) — Non-blocking (v0.3.2)

```go
// mcp/server.go — handleToolsCall

if decision.Action == "human_approval" {
    // Try TTY prompt first (interactive terminal)
    approved, resolvedBy := s.promptTTY(toolName, arguments)
    if approved != nil {
        // Resolved inline via /dev/tty — no store needed
        // ...
    }

    // No TTY — return immediately with pending approval ID
    pending := s.Approvals.Submit(s.AgentID, toolName, decision.Rule, arguments)
    remaining := pending.Remaining(s.Approvals.Timeout())
    return mcpText("Approval required (id: %s). Tool: %s. Timeout: %ds.\n"+
        "Use approval.resolve with id=%s and decision=approve or deny.",
        shortID, toolName, int(remaining.Seconds()), shortID), nil
}
```

The agent receives a text response immediately. An LLM agent (Claude Code) understands the message and calls `approval.resolve` to complete the flow. The `approval.resolve` virtual tool replays the original tool call on approve.

### Virtual MCP Tools

Two virtual tools are registered alongside the proxied tools (no policy evaluation):

| Tool | Description |
|------|-------------|
| `approval.resolve` | Approve or deny a pending request. On approve, replays the original tool call and returns the result. |
| `approval.pending` | List all pending approval requests with IDs, tools, and age. |

These are handled before registry lookup and policy evaluation — they're internal to agent-mesh.

### Built-in Tools (v0.3.3)

Two built-in tools provide capabilities not available from upstream MCP servers. Unlike virtual tools, these **go through the policy engine** like any proxied tool:

| Tool | Description | Parameters |
|------|-------------|------------|
| `filesystem.delete_file` | Delete a file from the filesystem | `path` (string, required) |
| `http.fetch` | Make an HTTP request and return the response | `url` (string, required), `method` (GET/POST/PUT/DELETE, default GET), `body` (string, optional) |

`http.fetch` returns `{status, headers, body}` with a 1MB response limit and 30s timeout. This fills the "sidecar proxy that doesn't proxy network" gap — agents can now reach external APIs, subject to policy.

### HTTP Proxy — Async Mode (planned)

The HTTP handler will return `202 Accepted` immediately with a pending approval ID. The agent polls `GET /approvals/{id}/result` for the result, or sets `X-Callback-URL` for push notification.

This is not yet implemented (Phase 3 in roadmap).

## Approval Channels

The resolution signal (`Approve`/`Deny`) can come from multiple sources. Agent-mesh tries them in cascade — the first available channel wins:

| # | Channel | Context | Friction | How it works |
|---|---------|---------|----------|-------------|
| **1** | **TTY prompt** | Interactive terminal (not piped) | Lowest — type `a` + enter | Prompt via `/dev/tty`, bypasses stdin/stdout |
| **2** | **`approval.resolve` MCP tool** | MCP mode (Claude Code, Cursor) | Low — agent self-resolves | Agent calls virtual tool after showing user the pending message |
| **3** | **CLI (`mesh`)** | Second terminal | Low — one command | `mesh approve <id>` calls HTTP API |
| **4** | **HTTP API** | Autonomous agents, scripts, webhooks | None (programmatic) | `POST /approvals/{id}/approve` |

The cascade:
1. **TTY available (not piped)?** → prompt inline via `/dev/tty`, resolve immediately, no store needed
2. **Piped stdin, store configured?** → return pending message immediately, agent resolves via `approval.resolve` tool, HTTP API, or CLI
3. **No store?** → return static "approval required" error (fallback)

### 1. TTY Prompt (interactive terminal, non-piped)

When agent-mesh runs with a real terminal (not piped stdin), it prompts via `/dev/tty`:

```
>> APPROVAL REQUIRED
   agent: claude  tool: filesystem.write_file
   path: /home/user/project/main.go
   [a]pprove / [d]eny ? a
   Approved
→ tool call completes immediately
```

**Skipped in MCP stdio mode** (Claude Code) because `/dev/tty` would open in the agent-mesh process terminal, not the caller's terminal — causing an invisible deadlock (fixed in v0.3.1).

### 2. `approval.resolve` Virtual Tool (MCP mode, primary path)

When TTY is not available (piped stdin), the handler returns immediately with a pending message. The agent calls `approval.resolve` to complete the flow:

```
Claude calls filesystem.write_file →
← "Approval required (id: a1b2c3d4). Use approval.resolve with id=a1b2c3d4 and decision=approve or deny."

Claude shows the message to the user, then calls:
  approval.resolve(id: "a1b2c3d4", decision: "approve")
← upstream result (file written)
```

This is the **primary path for Claude Code** since v0.3.2. No freeze, no second terminal, the agent stays responsive throughout.

### 2. HTTP API (always available)

New endpoints on the existing admin port:

| Method | Path | Description |
|--------|------|-------------|
| GET | `/approvals` | List pending approvals |
| GET | `/approvals/{id}` | Get approval details (tool, params, age) |
| POST | `/approvals/{id}/approve` | Approve — triggers upstream forward |
| POST | `/approvals/{id}/deny` | Deny |
| GET | `/approvals/{id}/result` | Get result after approval (async agents) |
| GET | `/approvals/{id}/result?wait=true` | Long-poll until resolved (opt-in sync for HTTP) |

Response includes the full tool call context so the approver can make an informed decision:

```json
GET /approvals

[
  {
    "id": "a1b2c3d4",
    "agent": "claude",
    "tool": "filesystem.write_file",
    "params": {
      "path": "/home/user/important.txt",
      "content": "..."
    },
    "created_at": "2026-04-05T14:30:00Z",
    "timeout": "5m0s",
    "remaining": "3m22s"
  }
]
```

### 3. CLI (`mesh` subcommands)

Thin wrapper over the HTTP API. Runs in a separate terminal. Supports prefix matching on approval IDs (8 chars is enough).

```bash
# One-shot commands
mesh pending                    # List pending approvals
mesh show a1b2c3d4              # Full details with params
mesh approve a1b2c3d4           # Approve by ID prefix
mesh deny e5f6g7h8              # Deny by ID prefix

# Interactive watch mode — polls every 2s, prompts for each new approval
mesh watch
# mesh watch — waiting for approvals (ctrl+c to quit)
#
# >> NEW  a1b2c3d4  claude  filesystem.write_file
#         path: /home/user/project/main.go
#         content: package main...
#         remaining: 4m58s
#   [a]pprove / [d]eny / [s]kip ? a
#   Approved
```

`mesh watch` is the recommended way to handle approvals from a second terminal. It shows context and prompts inline — no need to copy-paste IDs.

### 4. Webhook (outbound notification)

Optional. Agent-mesh notifies an external system when an approval is pending. The external system calls back via HTTP API.

```yaml
# In config YAML
approval:
  timeout: 5m
  notify:
    - type: webhook
      url: https://hooks.slack.com/services/T.../B.../xxx
      template: |
        {
          "text": ":warning: Approval needed: {{.Tool}} by {{.Agent}} — {{.MeshURL}}/approvals/{{.ID}}"
        }
```

## Temporal Grants (`mesh grant`)

Approval fatigue is the main risk. If a developer is iterating with an agent on a file, approving every `write_file` call is friction without value.

Temporal grants create a **time-boxed policy override** — like `sudo` for agents.

```bash
mesh grant claude "filesystem.write_file" --duration 15m
# Granted: claude can call filesystem.write_file until 14:45:00 (15m)

mesh grant claude "filesystem.*" --duration 30m --scope "params.path:/home/user/project/**"
# Granted: claude can call filesystem.* for paths matching /home/user/project/** until 15:00:00

mesh grants
# AGENT   TOOL                  SCOPE                              EXPIRES
# claude  filesystem.write_file (any)                              14:45:00 (12m left)
# claude  filesystem.*          path:/home/user/project/**         15:00:00 (27m left)

mesh revoke claude filesystem.write_file
# Revoked: claude / filesystem.write_file
```

### Implementation

Grants are stored in the approval store and checked **before** the pending flow:

```go
func (e *Engine) Evaluate(agentID, toolName string, params map[string]any) Decision {
    // 1. Check active grants first (new)
    if grant := e.grants.Match(agentID, toolName, params); grant != nil {
        return Decision{Action: "allow", Rule: "grant:" + grant.ID, Reason: "temporal grant"}
    }
    // 2. Normal policy evaluation
    // ...
}
```

Grants are:
- **Time-boxed** — auto-expire, no permanent escalation
- **Scoped** — can be restricted to specific param patterns (e.g., file paths)
- **Audited** — every grant creation, usage, and expiry is traced
- **Revocable** — `mesh revoke` kills a grant immediately

## Configuration

```yaml
# policies.yaml

approval:
  timeout: 5m              # default timeout for pending approvals
  default_deny: true       # timeout = deny (fail-closed)
  notify:                  # optional notification channels
    - type: log            # always: log to stderr
    - type: webhook        # optional: notify external system
      url: https://...

policies:
  - name: claude
    agent: "claude"
    rules:
      - tools: ["filesystem.write_file", "filesystem.edit_file"]
        action: human_approval
      # ... rest of policy
```

## Trace Integration

Approval events extend the existing trace entry:

```go
type Entry struct {
    // ... existing fields ...

    // Approval fields (populated when policy = human_approval)
    ApprovalID   string `json:"approval_id,omitempty"`
    ApprovalStatus string `json:"approval_status,omitempty"` // approved, denied, timeout
    ApprovedBy   string `json:"approved_by,omitempty"`
    ApprovalMs   int64  `json:"approval_ms,omitempty"`       // time spent waiting
}
```

This enables queries like:
```bash
# Average approval wait time
curl localhost:9090/traces?policy=human_approval | jq '[.[].approval_ms] | add / length'

# Who approves the most
curl localhost:9090/traces?policy=human_approval | jq 'group_by(.approved_by) | map({by: .[0].approved_by, count: length})'
```

## Implementation Status

### Done (v0.3.0–v0.3.2)

- `approval/store.go` — PendingApproval, channel-based blocking, timeout goroutine, prefix match
- HTTP endpoints: `GET /approvals`, `GET /approvals/{id}`, `POST /approvals/{id}/approve`, `POST /approvals/{id}/deny`
- MCP virtual tools: `approval.resolve` (approve/deny + replay), `approval.pending` (list)
- TTY prompt via `/dev/tty` with auto-skip in piped mode (v0.3.1)
- Non-blocking MCP handler: return pending message immediately (v0.3.2)
- `approval.resolve` replays original tool call on approve, returns upstream result
- `mesh` CLI: `pending`, `show`, `approve`, `deny`, `watch` subcommands
- Trace enrichment: approval_id, status, approved_by, approval_ms
- HTTP server runs in background alongside MCP mode

### Next

- Async HTTP proxy mode (202 + poll + `GET /approvals/{id}/result`)
- `X-Callback-URL` support
- Temporal grants (`mesh grant` — sudo for agents)
- Webhook notification on pending approval

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| **Timeout too short** | Agent gets denied before human can react | Default 5m, configurable per-rule, log remaining time |
| **Approval fatigue** | Human rubber-stamps everything | Temporal grants reduce volume; trace analytics surface patterns |
| **Connection drop (sync)** | MCP agent disconnects while waiting | Detect closed stdin, auto-deny, trace as "disconnected" |
| **Result storage bloat (async)** | Resolved results accumulate | TTL on stored results (1h default), evict oldest |
| **Concurrent approvals** | Multiple pending, human loses track | TUI mode, `mesh pending` with clear listing |
| **Stale grants** | Forgotten grant stays active | Mandatory TTL, no permanent grants, `mesh grants` shows expiry |

## Design Principle: The Sidecar Doesn't Prescribe Agent Behavior

Agent-mesh exposes constraints, it does not dictate how agents handle them.

- **LLM agents** (Claude Code via MCP) receive "Approval required" and understand the message. They can ask the user for confirmation, then call `approval.resolve`. The approval is visible, not hidden.
- **Async agents** (HTTP pipelines) receive `202 pending`. They choose to wait, replan, or do other work.
- **Simple agents** poll `approval.pending` in a loop until resolved.

All of these are valid. The sidecar governs _access_, the agent decides _workflow_. This separation is what makes agent-mesh framework-agnostic — it works with any agent, regardless of how sophisticated its planning is.

**Design shift (v0.3.2)**: The original design assumed MCP mode should be sync/blocking ("the agent never knows approval happened"). In practice, blocking the MCP connection froze Claude Code for up to 5 minutes, making it unusable. The async approach is better: the agent sees the approval gate, participates in resolving it, and stays responsive. Transparency beats invisibility for LLM agents.

## Non-Goals

- **Web dashboard** — out of scope for v1, HTTP API enables it later
- **Multi-user RBAC** — single operator model for now (the human at the terminal)
- **Approval chains** — no multi-level approval (manager then director), single gate
- **Undo after approval** — the approval gate is pre-execution, not a rollback mechanism
- **Agent-side SDK** — no client library for handling 202; agents use standard HTTP
