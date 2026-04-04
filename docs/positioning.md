# Positioning

> Most agent CLIs control access to tools. Gateways control access to APIs. Agent Mesh controls what agents are allowed to do with those tools.

## The two market forces

### Agent CLIs

Tools like Claude Code, Cursor, Gemini CLI, OpenCode, Cline, Pi, and OpenClaw give developers direct access to AI agents that can read files, execute commands, call APIs, and interact with external services.

Each has some form of control mechanism:

| Agent CLI | Built-in controls |
|-----------|------------------|
| **Pi** | None by default. Extensions can add permission gates and sandboxing |
| **OpenClaw** | tools.allow/deny, ask mode, tool profiles, DM pairing |
| **Cline** | Human approval for every file change and terminal command |
| **Cursor** | permissions.json allowlists, approval prompts |
| **Claude Code** | Permission modes, hooks, project-level settings, MCP config |
| **OpenCode** | Permission config (auto-allow, ask, deny per action type) |
| **Gemini CLI** | Sandboxing, trusted folders, enterprise security options |

These are real security mechanisms. They address real risks.

But their center of gravity is **tool-level access control**: which tools are available, whether to ask before running, where execution is allowed. They do not provide a unified layer for semantic governance of what those tools do — based on action, parameters, agent identity, and conditions — across agents.

### Gateways and MCP platforms

Tools like Kong, Gravitee, Obot, and Cloudflare provide infrastructure for API and MCP management: routing, authentication, hosting, catalogs, rate limiting, and access control.

| Platform | Focus |
|----------|-------|
| **Kong** | API/AI Gateway, enterprise distribution, MCP registry |
| **Gravitee** | Unified platform for APIs + events + agent governance |
| **Obot** | Open-source MCP control plane and gateway |
| **Cloudflare** | Remote MCP hosting, OAuth, edge delivery, developer portal |

Their center of gravity is **infrastructure and access control** — routing, auth, hosting, catalog, rate limiting. Not a dedicated layer of semantic governance for agent tool calls across multiple agents and tool surfaces.

## The shared gap

Neither agent CLIs nor gateways provide, out of the box:

1. **Semantic policy** — rules based on what the action does (tool + parameters + conditions), not just which tool is available
2. **Cross-agent policy-as-code** — one versionable, reviewable config that applies the same governance to Claude Code, Cursor, OpenClaw, Pi, and custom agents
3. **Centralized trace** — one place to see all tool calls across all agents, with who, what, params, decision, and latency
4. **Differentiated agent identity** — different agents get different permissions on the same tools
5. **Condition-based rules** — allow `create_refund` if amount < 500, deny if >= 500

Agent CLIs have the tools and local controls, but not cross-agent semantic governance.
Gateways have infrastructure and access control, but not a dedicated action-governance layer.

## Where Agent Mesh fits

```
                    Lightweight / Dev-first
                           |
                      agent-mesh
                           |
            +--------------+--------------+
            |                             |
      Agent CLIs                    Gateways
      (Pi, OpenClaw, Claude         (Kong, Gravitee,
       Code, Cursor, Gemini,         Obot, Cloudflare)
       OpenCode, Cline)
            |                             |
      Have tools and               Have infrastructure
      local controls               and access control
```

Agent Mesh is not an agent CLI and not a gateway. It sits between agents and the tools they call, adding a layer that neither side provides on its own.

## Comparison

| | Agent CLIs | Gateways | Agent Mesh |
|---|---|---|---|
| **Target** | Developer writing code | Platform / infra team | Both |
| **Scope** | One agent, one context | N APIs, routing | N agents, N tools |
| **Control level** | Tool-level (allow exec / deny exec) | Route-level (rate limit, auth) | Action-level (allow `create_issue`, deny `delete_repo`) |
| **Policy** | Session or project scoped | Gateway config | YAML, versioned in git, cross-agent |
| **Trace** | Local, ephemeral | Per API | Centralized, per agent + tool + params |
| **Identity** | Single user/session | API consumer | Per-agent (claude, cursor, openclaw) |
| **Adoption** | Built into the CLI | Infrastructure deployment | Lightweight, local-first, MCP or HTTP integration in minutes |

## Positioning statement

Agent Mesh is not an agent runtime and not an API gateway. It is a lightweight, open-source governance layer for agent tool calls: semantic policy-as-code, centralized trace, and differentiated agent identity — across agents and tool surfaces.

One binary. One YAML config. Works with any agent that speaks HTTP or MCP.
