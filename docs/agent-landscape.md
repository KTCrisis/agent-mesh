# AI Agent CLI Landscape — and why they need governance

**Most agent CLIs control access to tools. Agent Mesh controls what agents are allowed to do with those tools.**

## The problem all these tools share

Every AI agent CLI gives the LLM access to tools: filesystem, shell, browser, APIs. Most have some form of permission model — approval prompts, allow/deny lists, hooks, trusted folders, or sandboxing.

These mechanisms control **which tools** an agent can use. They don't control **what the agent does** with those tools.

```
User says: "fix the bug"
Agent decides: rm -rf node_modules && npm install
User sees: "Execute command?" [Yes/No]
User clicks: Yes (because it looks reasonable)
Result: works — but no trace, no semantic policy, no cross-agent audit
```

The real risk isn't the obvious destructive command. It's the subtle one that looks fine but shouldn't happen in this context, with this agent, at this time.

## The landscape

### Claude Code (Anthropic)

| | |
|---|---|
| **Tools** | Read, Write, Edit, Bash, Glob, Grep, WebSearch, WebFetch |
| **MCP support** | Yes — first-class, stdio + SSE |
| **Security model** | Permission modes (ask, auto-allow by type), hooks, project-level settings |
| **Governance** | Project-shareable permissions and hooks. No dedicated cross-agent semantic policy engine or centralized trace export |

Claude Code has a real permission system: project-level settings, hooks that run before/after tool calls, and MCP server configuration shareable via `.claude/settings.json`. These are meaningful controls. The gap is that they operate at the tool and project level, not as a semantic policy layer across agents and actions.

### Cursor

| | |
|---|---|
| **Tools** | File edit, terminal, browser, codebase search |
| **MCP support** | Yes — via `.cursor/mcp.json` |
| **Security model** | Human approval for file changes and terminal commands, `permissions.json` for allowlists |
| **Governance** | Permissions and auto-run allowlists exist, but limited to tool/command level. No semantic governance on action parameters |

Cursor has approval prompts and `permissions.json` for auto-run allowlists on terminal and MCP tools. These are useful for reducing friction on trusted operations. The gap is the same: permissions control which tools run, not what they do.

### Gemini CLI (Google)

| | |
|---|---|
| **Tools** | Filesystem, shell, web fetch, Google Search grounding |
| **MCP support** | Yes — MCP server integration with OAuth support for remote servers |
| **Security model** | Sandboxing, trusted folders, enterprise security options |
| **Governance** | Trusted folder policies scope where tools can act. No semantic governance on what the actions do within those folders |

Gemini CLI's sandboxing and trusted folders are a meaningful step beyond pure approval prompts — they limit the blast radius by restricting where tools can operate. The gap: controlling where is not the same as controlling what.

### OpenCode

| | |
|---|---|
| **Tools** | File ops, shell commands, LSP integration |
| **MCP support** | Yes — stdio MCP servers |
| **Security model** | Permission config (auto-allow, ask, deny per action type) |
| **Governance** | Permission system controls approval behavior per action type. No semantic policy on call parameters or cross-agent trace |

OpenCode has a structured permission model that goes beyond simple yes/no dialogs — it can auto-allow, ask, or deny based on action types. The gap: permission decisions are per-tool-type, not per-action-content.

### Cline (VS Code extension)

| | |
|---|---|
| **Tools** | File create/edit, terminal, browser, MCP tools |
| **MCP support** | Yes — can create and install MCP servers dynamically |
| **Security model** | Human-in-the-loop for every file change and command |
| **Governance** | Systematic human approval. Lower autonomy risk, higher operational friction. No programmatic policy layer |

Cline's strict human-in-the-loop model makes it one of the most conservative agent tools. Every action requires explicit approval, which reduces autonomy risk but doesn't scale. It also can create MCP servers on the fly, which is both a powerful capability and a trust surface.

### Pi (pi-coding-agent)

| | |
|---|---|
| **Tools** | `read`, `write`, `edit`, `bash`, `grep`, `find`, `ls` |
| **MCP support** | Explicitly refused by design. Can be added via community extensions. [Why no MCP](https://mariozechner.at/posts/2025-11-02-what-if-you-dont-need-mcp/) |
| **Security model** | "No permission popups. Run in a container, or build your own confirmation flow with extensions." |
| **Governance** | None built-in. Extensions can add permission gates and path protection. Packages carry a warning: "Pi packages run with full system access" |

Pi takes the most radical position in the landscape: maximal extensibility, minimal built-in governance. The philosophy is that permissions, sandboxing, MCP, sub-agents, and plan mode are all things you can build yourself via TypeScript extensions or install from third-party packages.

This makes Pi extremely flexible, but governance becomes the user's responsibility entirely. The extension system can do anything — including replacing built-in tools, adding permission gates, and integrating MCP — but nothing is enforced by default.

Pi is also the engine behind OpenClaw: OpenClaw uses `pi-agent-core` as its SDK, adding channels (WhatsApp, Slack), a gateway, and ClawHub on top. Pi's RPC mode (`--mode rpc`) uses stdin/stdout JSONL framing, similar in spirit to MCP but a different protocol.

### OpenClaw

| | |
|---|---|
| **Tools** | exec, read, write, edit, browser, web_search, message, node.invoke |
| **MCP support** | No — uses Skills (SKILL.md prompt injection) and Plugins (npm) |
| **Security model** | tools.allow/deny, ask mode, DM pairing, allowlists, tool profiles |
| **Governance** | Tool-level allow/deny with profiles ("minimal", "coding"). Explicit trust model documented on their Trust page |

OpenClaw has one of the highest raw capability surfaces of any agent tool:

- `exec` tool enables arbitrary shell command execution
- Skills are prompt-based — the LLM generates commands rather than calling typed functions
- Connected to messaging platforms (WhatsApp, Slack, Telegram) — a real prompt injection surface
- ClawHub skills are installed dynamically — supply chain considerations apply
- Memory persistence introduces memory poisoning as a threat vector

OpenClaw is transparent about this — their Trust page documents the threat model honestly, and their mitigations (`tools.deny`, `ask: always`, VirusTotal scanning on ClawHub) are real. But these controls operate at the **tool level**: you can block `exec` entirely (which makes the agent much less useful) or allow it (which grants broad capability). You cannot say "allow `gh issue list` but block `gh repo delete`" because both go through `exec`.

## Common patterns

Most agent CLIs share some combination of these control mechanisms:

| Pattern | Where it appears | What it doesn't cover |
|---------|-----------------|----------------------|
| **Human-in-the-loop** | All agents to varying degrees | Doesn't scale. Users approve reflexively over time. No audit trail |
| **Tool-level allow/deny** | OpenClaw, Claude Code, Cursor, OpenCode | Controls which tools are available, not what they do |
| **Session/project permissions** | Claude Code, OpenCode, Cursor | Useful but scoped to one agent and one context |
| **Sandboxing / trusted folders** | Gemini CLI | Controls where, not what |
| **Trust on add** | All MCP-capable agents | Once an MCP server is connected, its tools are generally trusted |

These are all valid security mechanisms. They address real risks. But none of them, individually or combined, provide a **unified semantic governance layer** that works across agents.

## The gap

What no single agent CLI provides today:

1. **Semantic policy** — rules based on what the action does (tool + parameters + conditions), not just which tool it uses
2. **Cross-agent policy-as-code** — one versionable, reviewable YAML config that applies the same governance to Claude Code, Cursor, OpenClaw, and custom agents
3. **Centralized trace** — one place to see all tool calls across all agents, with who, what, params, decision, and latency
4. **Differentiated agent identity** — different agents get different permissions on the same tools
5. **Condition-based rules** — allow `create_refund` if amount < 500, deny if >= 500

## Where agent-mesh fits

```
Claude Code --\
Cursor       --+--> Agent Mesh --> GitHub / APIs / DB / tools
Gemini CLI   --+        |
OpenCode     --+   policy . trace
OpenClaw     --/
```

Agent-mesh doesn't replace any of these tools. It doesn't compete with their built-in permission systems. It sits between them and the tools they call, adding a layer that none of them provide on their own.

| What agent-mesh adds | How |
|---------------------|-----|
| Semantic policy | Rules on tool name + parameters, not just "allow exec" |
| Policy as code | One YAML file, versioned in git, reviewed in PRs |
| Centralized trace | Every call logged: who, what, params, decision, latency |
| Agent identity | Different policies per agent (`claude` vs `openclaw` vs `cursor`) |
| Fail closed | No matching rule = deny |
| Zero code change | Agent points to agent-mesh instead of the real backend |

## Integration by agent

| Agent | How to integrate | Effort |
|-------|-----------------|--------|
| **Claude Code** | `claude mcp add agent-mesh -- ./agent-mesh --mcp --config policies.yaml` | 2 minutes |
| **Cursor** | Add to `.cursor/mcp.json` | 2 minutes |
| **Gemini CLI** | Add as MCP server in settings | 2 minutes |
| **OpenCode** | Add as MCP server in config | 2 minutes |
| **Cline** | Add as MCP server in VS Code | 2 minutes |
| **Pi** | Extension that redirects tool calls through agent-mesh HTTP, or MCP extension | 1 day (extension dev) |
| **OpenClaw** | Plugin that wraps tools + `tools.deny: [exec]` | 1 day (plugin dev) |
| **HTTP agents** (LangChain, CrewAI) | Point to `http://localhost:9090/tool/{name}` | 5 minutes |

## Simplified governance maturity spectrum

This is a rough, simplified view — not a definitive ranking. Each agent has different strengths and different trade-offs. The spectrum illustrates control model differences, not overall quality.

```
Minimal controls                                        Semantic governance
|                                                                        |
|  Pi          OpenClaw     Cline      Cursor    Claude Code  Gemini CLI |  + agent-mesh
|  (none by    (tool deny   (approve   (perms    (hooks +     (sandbox + |  (semantic
|   default)    + ask)       each)      + mcp)    settings)    folders)  |   policy)
```

No agent CLI is inherently "unsafe" — they all have security mechanisms, and they all give LLMs the ability to act in the real world. The question is at which level of abstraction you control what they do. Agent-mesh adds semantic, cross-agent governance without removing any existing capability.
