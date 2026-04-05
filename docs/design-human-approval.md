# Design: Human Approval

**Status**: Draft
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

## Two Modes, One Store

The approval mechanism supports two execution modes. The mode is determined by the **transport**, not the policy — the same `human_approval` rule produces different behavior depending on how the agent connects.

| Mode | Transport | Agent type | Behavior |
|------|-----------|------------|----------|
| **Sync** | MCP stdio | Interactive (Claude Code, Cursor) | Connection held, agent waits, gets result when approved |
| **Async** | HTTP | Autonomous (pipelines, background agents) | 202 immediate, agent continues, polls or receives callback |

The approval store is shared. Only the handler integration differs.

### Sync Mode — Interactive agents

```
Agent (Claude Code / Cursor)               agent-mesh                        Human
         |                                      |                               |
         |-- tools/call ----------------------->|                               |
         |                                      |-- policy: human_approval      |
         |                                      |-- create PendingApproval      |
         |                                      |-- notify -------------------->|
         |                                      |                               |
         |          (connection held, agent waits)                              |
         |                                      |                               |
         |                                      |<-- mesh approve <id> ---------|
         |                                      |-- forward to upstream         |
         |<-- result ---------------------------|                               |
```

The agent never knows approval happened. It submitted a tool call and got a result — just slower than usual. This is the sidecar contract.

### Async Mode — Autonomous agents

```
Autonomous agent                       agent-mesh                        Human
      |                                      |                               |
      |-- POST /tool/write_file ------------>|                               |
      |<-- 202 {id: "abc", status: pending} -|-- notify -------------------->|
      |                                      |                               |
      |-- (continues other work)             |                               |
      |-- POST /tool/read_file ------------->|                               |
      |<-- 200 {result: ...} ---------------|                               |
      |                                      |                               |
      |                                      |<-- mesh approve abc ----------|
      |                                      |-- forward write_file          |
      |                                      |-- store result                |
      |                                      |-- POST callback (if set) ---->|  (to agent)
      |                                      |                               |
      |-- GET /approvals/abc/result -------->|                               |
      |<-- 200 {status: approved, result} ---|                               |
```

The agent is not blocked. It receives a `202 Accepted` with a pending approval ID, continues working on tasks that don't depend on the blocked action, and retrieves the result when ready.

### Why the transport dictates the mode

- **MCP stdio** is inherently synchronous: one JSON-RPC request, one response. The agent cannot proceed until it gets a response. Holding the response is the only option — and it works, because the human is sitting at the terminal.
- **HTTP** is inherently async-capable: the agent can fire requests, handle 202s, and poll. Blocking an HTTP connection for 5 minutes is fragile (timeouts, proxies, dropped connections). Returning 202 and letting the agent decide is more robust.

An HTTP client can opt into sync behavior by long-polling `GET /approvals/{id}/result?wait=true`, which blocks until resolution. This gives HTTP agents the choice.

### The agent that replans

A well-designed autonomous agent adapts when it hits an approval gate:

```
Agent: "I need to write config.yaml then run tests"
  1. write_file config.yaml → 202 pending (approval required)
  2. Agent recognizes pending, replans: "I'll prepare tests while waiting"
  3. read_file test_suite.go → 200 OK
  4. Analyze test code, prepare test plan
  5. Poll: GET /approvals/abc/result → approved, result attached
  6. Run tests with the written config
```

This is the agent's responsibility, not the sidecar's. Agent-mesh exposes the constraint (`202 pending`), the agent decides how to handle it. The sidecar doesn't prescribe agent behavior — it governs tool access.

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

### MCP Server (stdio) — Sync Mode

```go
// mcp/server.go — handleToolsCall

if decision.Action == "human_approval" {
    pending := s.Approvals.Submit(s.AgentID, toolName, arguments, traceID)
    slog.Warn("APPROVAL REQUIRED",
        "id", pending.ID, "tool", toolName, "timeout", pending.Timeout)

    // Block until resolved — sync mode, MCP is request/response
    resolution := <-pending.Result

    if resolution.Status != StatusApproved {
        return mcpText("Action %s: %s", resolution.Status, toolName), nil
    }

    // Approved — forward to upstream
    result, statusCode, err := s.Handler.Forward(tool, arguments)
    // ... trace + return result
}
```

The MCP JSON-RPC response is held open. The agent (Claude Code) simply waits. From Claude Code's perspective, the tool call is taking a while — no special handling needed.

### HTTP Proxy — Async Mode

The HTTP handler returns `202 Accepted` immediately. The approval store holds the pending entry. When a human approves, agent-mesh forwards to upstream and stores the result. The agent retrieves it later.

```go
// proxy/handler.go — handleToolCall

if decision.Action == "human_approval" {
    pending := h.Approvals.Submit(agentID, toolName, req.Params, entry.TraceID)

    // Extract optional callback URL
    callbackURL := r.Header.Get("X-Callback-URL")
    if callbackURL != "" {
        h.Approvals.SetCallback(pending.ID, callbackURL)
    }

    // Async mode — return 202 immediately
    writeJSON(w, 202, ToolCallResponse{
        TraceID:    entry.TraceID,
        ApprovalID: pending.ID,
        Policy:     "human_approval",
        Error:      "action requires human approval",
    })
    return
}
```

When the approval resolves, the store triggers the forwarding:

```go
// approval/store.go — on Approve()

func (s *Store) Approve(id string, by string) error {
    // ... resolve the pending entry ...

    // Forward to upstream (async — runs in goroutine)
    go func() {
        result, statusCode, err := s.handler.Forward(pending.Tool, pending.Params)
        s.storeResult(id, result, statusCode, err)

        // Callback if configured
        if pending.CallbackURL != "" {
            s.postCallback(pending.CallbackURL, id, result)
        }
    }()

    // Also unblock any sync waiters (MCP mode)
    pending.Result <- Resolution{Status: StatusApproved, ResolvedBy: by, ResolvedAt: time.Now()}
    return nil
}
```

### Result Retrieval — Async Agents

Async agents retrieve results through a new endpoint:

```
GET /approvals/{id}/result

# Pending — not yet resolved
404 {"status": "pending", "remaining": "3m22s"}

# Resolved — result available
200 {
  "status": "approved",
  "approved_by": "cli:marc",
  "result": { ... },          // upstream response
  "latency_ms": 142,
  "approval_ms": 34200
}

# Denied or timed out
200 {"status": "denied", "resolved_by": "cli:marc"}
200 {"status": "timeout", "resolved_by": "system"}
```

Optional long-poll for agents that prefer to wait:

```
GET /approvals/{id}/result?wait=true&timeout=60s

# Blocks until resolved or client timeout, whichever comes first
```

### Callback — Push notification to agent

If the agent sets `X-Callback-URL` on the original request, agent-mesh POSTs the result to that URL when the approval resolves:

```
POST {callback_url}
Content-Type: application/json

{
  "approval_id": "abc123",
  "status": "approved",
  "tool": "filesystem.write_file",
  "result": { ... }
}
```

This is optional. Agents that don't set the header use polling. Agents that do get push notification — zero change to the approval store, just an extra HTTP call on resolution.

## Approval Channels

The resolution signal (`Approve`/`Deny`) can come from multiple sources. Agent-mesh exposes these in priority order:

### 1. HTTP API (always available)

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

### 2. CLI (`mesh` subcommands)

Thin wrapper over the HTTP API. Runs in a separate terminal.

```bash
# List pending
mesh pending
# ID        AGE    AGENT   TOOL                     TIMEOUT
# a1b2c3d4  12s    claude  filesystem.write_file     4m48s
# e5f6g7h8  3s     claude  gmail.gmail_send_email    4m57s

# Approve with context
mesh show a1b2c3d4
# Agent:   claude
# Tool:    filesystem.write_file
# Params:
#   path:    /home/user/important.txt
#   content: "human approval test — agent-mesh sidecar proxy"
# Policy:  claude / rule #3

mesh approve a1b2c3d4
# Approved: a1b2c3d4 (filesystem.write_file)

mesh deny e5f6g7h8
# Denied: e5f6g7h8 (gmail.gmail_send_email)

# Approve all pending (with confirmation)
mesh approve --all
# 2 pending approvals. Approve all? [y/N] y
# Approved: a1b2c3d4 (filesystem.write_file)
# Approved: e5f6g7h8 (gmail.gmail_send_email)
```

### 3. TUI (`mesh watch`)

Live terminal UI showing pending approvals. Interactive approve/deny with `a`/`d` keys. Useful for sessions where many approvals are expected.

```
mesh watch

  AGENT-MESH APPROVALS                              2 pending

  > a1b2c3d4  claude  filesystem.write_file    12s  [a]pprove [d]eny [v]iew
    e5f6g7h8  claude  gmail.gmail_send_email    3s  [a]pprove [d]eny [v]iew

  ── resolved ──
    f9g0h1i2  claude  filesystem.edit_file    APPROVED by cli:marc  45s ago
```

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

## Implementation Phases

### Phase 1 — Approval store + sync mode + HTTP API

- `approval/store.go` — PendingApproval, channel-based blocking, timeout goroutine
- HTTP endpoints: `GET /approvals`, `POST /approvals/{id}/approve`, `POST /approvals/{id}/deny`
- Wire sync mode into `mcp/server.go` (block on channel)
- Wire async mode into `proxy/handler.go` (return 202)
- Trace enrichment with approval fields
- stderr logging for pending approvals (minimal notification)

### Phase 2 — Async result flow + CLI

- `GET /approvals/{id}/result` endpoint (poll + long-poll)
- `X-Callback-URL` support — POST result to agent on resolution
- Forward-on-approve: upstream call happens when human approves, result stored
- `mesh pending` / `mesh approve` / `mesh deny` / `mesh show`
- Thin HTTP client calling the Phase 1 API
- Colored terminal output, human-readable

### Phase 3 — Temporal grants

- `approval/grant.go` — grant store with TTL
- Policy engine integration (check grants before rules)
- `mesh grant` / `mesh grants` / `mesh revoke`

### Phase 4 — TUI + Webhooks

- `mesh watch` — live terminal UI (bubbletea or similar)
- Webhook notification on pending approval
- Slack/Discord integration templates

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

- **Sync agents** (Claude Code via MCP) experience a slow tool call. They don't know approval exists.
- **Async agents** (HTTP pipelines) receive `202 pending`. They choose to wait, replan, or do other work.
- **Smart agents** replan around the constraint: start independent tasks while waiting for approval.
- **Simple agents** poll in a loop until resolved.

All of these are valid. The sidecar governs _access_, the agent decides _workflow_. This separation is what makes agent-mesh framework-agnostic — it works with any agent, regardless of how sophisticated its planning is.

## Non-Goals

- **Web dashboard** — out of scope for v1, HTTP API enables it later
- **Multi-user RBAC** — single operator model for now (the human at the terminal)
- **Approval chains** — no multi-level approval (manager then director), single gate
- **Undo after approval** — the approval gate is pre-execution, not a rollback mechanism
- **Agent-side SDK** — no client library for handling 202; agents use standard HTTP
