# AI Agent CLI Landscape — and why they need governance

## The problem all these tools share

Every AI agent CLI gives the LLM access to tools: filesystem, shell, browser, APIs. The security model is almost always the same: **human-in-the-loop approval** or **allow/deny at the tool level**.

None of them have semantic governance on what the tool call actually does.

```
User says: "fix the bug"
Agent decides: rm -rf node_modules && npm install
User sees: "Execute command?" [Yes/No]
User clicks: Yes (because it looks reasonable)
Result: works — but no trace, no policy, no audit
```

The real risk isn't the obvious destructive command. It's the subtle one that looks fine but shouldn't happen in this context, with this agent, at this time.

## The landscape

### Claude Code (Anthropic)

| | |
|---|---|
| **Tools** | Read, Write, Edit, Bash, Glob, Grep, WebSearch, WebFetch |
| **MCP support** | Yes — first-class, stdio + SSE |
| **Security model** | Permission modes (ask, auto-allow by type), hooks |
| **Governance** | Per-session permissions, no policy-as-code, no trace export |

**Risk profile**: Moderate. Permission system is granular but session-scoped. No persistent policy file. No centralized trace. MCP servers get direct access once approved.

### Cursor

| | |
|---|---|
| **Tools** | File edit, terminal, browser, codebase search |
| **MCP support** | Yes — via `.cursor/mcp.json` |
| **Security model** | Human approval for file changes and terminal commands |
| **Governance** | None beyond approval prompts |

**Risk profile**: Moderate. Similar to Claude Code but less configurable. MCP servers are trusted once added.

### Gemini CLI (Google)

| | |
|---|---|
| **Tools** | Filesystem, shell, web fetch, Google Search grounding |
| **MCP support** | Yes — MCP server integration in settings |
| **Security model** | Sandboxing, trusted folders, enterprise security options |
| **Governance** | Trusted folder policies, no semantic tool governance |

**Risk profile**: Lower than average. Sandboxing and trusted folders are better than pure approval. But no policy on what the tool call does, just where it can run.

### OpenCode

| | |
|---|---|
| **Tools** | File ops, shell commands, LSP integration |
| **MCP support** | Yes — stdio MCP servers |
| **Security model** | Permission dialogs (single, session, deny) |
| **Governance** | None beyond approval prompts |

**Risk profile**: Moderate. Standard approve/deny pattern.

### Cline (VS Code extension)

| | |
|---|---|
| **Tools** | File create/edit, terminal, browser, MCP tools |
| **MCP support** | Yes — can create and install MCP servers dynamically |
| **Security model** | Human-in-the-loop for every file change and command |
| **Governance** | None — relies entirely on user approval |

**Risk profile**: Lower (human approves everything) but high friction. Can create MCP servers on the fly, which is both powerful and risky.

### OpenClaw

| | |
|---|---|
| **Tools** | exec, read, write, edit, browser, web_search, message, node.invoke |
| **MCP support** | No — uses Skills (SKILL.md prompt injection) and Plugins (npm) |
| **Security model** | tools.allow/deny, ask mode, DM pairing, allowlists |
| **Governance** | Tool-level allow/deny, tool profiles ("minimal", "coding") |

**Risk profile**: **High**. The most capable and the most dangerous:

- `exec` tool = arbitrary shell command execution
- Skills are prompts, not contracts — the LLM generates commands, not calls typed functions
- Connected to messaging platforms (WhatsApp, Slack, Telegram) = prompt injection surface
- ClawHub skills are installed dynamically = supply chain risk
- Memory persistence = memory poisoning possible

OpenClaw knows this — their Trust page is honest about the threat model. Their mitigations (`tools.deny`, `ask: always`, VirusTotal scanning) are real but operate at the **tool level**, not the **action level**.

The gap: you can block `exec` entirely (unusable) or allow it (dangerous). You cannot say "allow `gh issue list` but block `gh repo delete`" because both go through `exec`.

## Common patterns across all agents

| Pattern | Agents that do this | The problem |
|---------|-------------------|-------------|
| **Human-in-the-loop** | All of them | Doesn't scale. Users approve reflexively. No audit trail |
| **Tool-level allow/deny** | OpenClaw, Claude Code | Too coarse. Allow `exec` = allow everything. Deny `exec` = useless |
| **Session-scoped permissions** | Claude Code, OpenCode | No persistence across sessions. No version control |
| **No centralized trace** | All of them | You can't answer "what did my agents do yesterday?" |
| **Trust on add** | All MCP-capable agents | Once an MCP server is added, it's fully trusted |

## What's missing everywhere

None of these agents have:

1. **Semantic policy** — control based on what the action does, not which tool it uses
2. **Policy as code** — a versionable, reviewable config file that persists across sessions
3. **Centralized trace** — one place to see all tool calls across all agents
4. **Agent identity** — different agents get different permissions on the same tools
5. **Condition-based rules** — allow `create_refund` if amount < 500, deny if >= 500

## Where agent-mesh fits

```
Claude Code --\
Cursor       --+--> Agent Mesh --> GitHub / APIs / DB / tools
Gemini CLI   --+        |
OpenCode     --+   policy . trace
OpenClaw     --/
```

Agent-mesh doesn't replace any of these tools. It sits between them and the tools they call.

| What agent-mesh adds | How |
|---------------------|-----|
| Semantic policy | Rules on tool name + parameters, not just "allow exec" |
| Policy as code | One YAML file, versioned in git, reviewed in PRs |
| Centralized trace | Every call logged: who, what, params, decision, latency |
| Agent identity | Different policies per agent (`claude` vs `openclaw` vs `cursor`) |
| Fail closed | No matching rule = deny |
| Zero code change | Agent points to agent-mesh instead of the real backend |

## Integration by agent

| Agent | How to integrate | Difficulty |
|-------|-----------------|-----------|
| **Claude Code** | `claude mcp add agent-mesh -- ./agent-mesh --mcp --config policies.yaml` | 2 minutes |
| **Cursor** | Add to `.cursor/mcp.json` | 2 minutes |
| **Gemini CLI** | Add as MCP server in settings | 2 minutes |
| **OpenCode** | Add as MCP server in config | 2 minutes |
| **Cline** | Add as MCP server in VS Code | 2 minutes |
| **OpenClaw** | Plugin that wraps tools + `tools.deny: [exec]` | 1 day (plugin dev) |
| **HTTP agents** (LangChain, CrewAI) | Point to `http://localhost:9090/tool/{name}` | 5 minutes |

## The security spectrum

```
Less safe                                                    More safe
|                                                                 |
|  OpenClaw     Cline     Claude Code    Gemini CLI    agent-mesh |
|  (exec +      (approve  (permissions   (sandbox +    (semantic  |
|   prompts)     each)     + hooks)       folders)      policy)   |
```

No agent CLI is truly "safe" — they all give LLMs the ability to act in the real world. The question is how much control you have over what they do. Agent-mesh moves the cursor to the right without removing any capability.
