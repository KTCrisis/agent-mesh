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
| **Kong AI Gateway** | API/AI Gateway, semantic routing, 60+ AI plugins, MCP server auto-generation |
| **Gravitee AI Gateway** | Unified platform for APIs + events + MCP proxy native, auto-generates MCP tools from OpenAPI specs |
| **Apigee (Google)** | Google Cloud native AI gateway, Model Armor, semantic caching, ADK integration |
| **OpenClaw** | Agent gateway, multi-channel (Telegram, Discord, Slack, WhatsApp), governance dashboard |
| **Obot** | Open-source MCP control plane and gateway |
| **Cloudflare** | Remote MCP hosting, OAuth, edge delivery, developer portal |

Their center of gravity is **infrastructure and access control** — routing, auth, hosting, catalog, rate limiting. Not a dedicated layer of semantic governance for agent tool calls across multiple agents and tool surfaces.

### The stack in layers

These tools operate at fundamentally different layers. MCP is the protocol — not a product. The others are products at different levels of the stack:

```
┌───────────────────────────────────────────────────────┐
│  PROTOCOL            MCP (+ A2A)                      │  standard — how agents talk to tools
├───────────────────────────────────────────────────────┤
│  AI GATEWAY          Kong / Gravitee / Apigee         │  north-south — app/user → LLM/tools
├───────────────────────────────────────────────────────┤
│  AGENT GATEWAY       OpenClaw                         │  multi-channel — user → agents
├───────────────────────────────────────────────────────┤
│  GOVERNANCE MESH     Agent Mesh                       │  east-west — agent ↔ agent, semantic policy
└───────────────────────────────────────────────────────┘
```

### Gateway-by-gateway comparison

| | **Kong AI Gateway** | **Gravitee AI Gateway** | **Apigee** | **OpenClaw** | **Agent Mesh** |
|---|---|---|---|---|---|
| **Layer** | AI Gateway (north-south) | AI Gateway + MCP Proxy (north-south) | AI Gateway (north-south) | Agent Gateway (user→agents) | Service Mesh (east-west) |
| **Analogie classique** | Kong / Nginx | Gravitee APIM | Apigee classic | Twilio multi-channel | Istio / Envoy sidecar |
| **Primary focus** | LLM traffic governance, semantic routing, PII sanitization | MCP method-level governance, OpenAPI→MCP auto-generation | Google Cloud LLM governance, Model Armor, Vertex AI native | Multi-channel agent delivery (Telegram, Slack, Discord, WhatsApp) | Inter-agent governance, semantic policy-as-code |
| **MCP support** | Auto-generates MCP servers from APIs | Native MCP Proxy — inspects at method level (tool discovery, execution) | MCP support via proxy, REST→MCP bridge | No — uses Skills (SKILL.md) and Plugins (npm) | MCP first-class (stdio + SSE) |
| **Policy model** | Route-level: rate limit, PII redaction, allow/deny lists | MCP method-level: auth, rate limits per MCP method | Route-level: token limiting, Model Armor threat detection | Tool-level: tools.allow/deny, ask mode, tool profiles | Action-level: tool + parameters + conditions, per-agent identity |
| **Deployment** | Cloud / self-hosted, enterprise licensing | Cloud / self-hosted, enterprise licensing | Google Cloud managed | Self-hosted, open-source | Single binary, local-first, zero dependencies |
| **Best for** | Teams routing LLM traffic across multiple providers | Teams wanting MCP governance without changing existing APIs | Google Cloud shops needing LLM governance + Vertex AI integration | Products delivering agent capabilities to end-users on messaging platforms | Teams running multiple agents that need cross-agent semantic governance |

### Why MCP is not a competitor

MCP (Model Context Protocol) is the **communication standard**, not a governance product. It defines how clients discover and call tools. Every product above can use MCP — just as every API gateway uses HTTP. Comparing agent-mesh to MCP is like comparing Istio to TCP.

### Complementarity, not competition

These layers compose naturally:

```
User on Telegram
      │
      ▼
  OpenClaw (agent gateway — routes to the right agent)
      │
      ▼
  Kong / Gravitee / Apigee (AI gateway — LLM routing, PII, rate limiting)
      │
      ▼
  Agent Mesh (governance mesh — semantic policy between agents and tools)
      │
      ▼
  Tools (GitHub, DB, APIs, filesystem)
```

A team could use Gravitee to govern LLM traffic, OpenClaw to serve agents on Slack, and Agent Mesh to enforce that agent-A can create issues but not delete repos — all in the same stack. They solve different problems at different layers.

### Agent frameworks (LangChain, CrewAI) and governance toolkits

Agent frameworks orchestrate LLM workflows. Some have governance-adjacent features, but none provide a dedicated, cross-agent semantic governance layer.

| | **LangChain** | **CrewAI** | **Microsoft Agent Governance Toolkit** | **Agent Mesh** |
|---|---|---|---|---|
| **What it is** | Agent framework + LangSmith observability | Multi-agent orchestration framework | In-process policy engine (open-source, MIT) | Sidecar governance proxy |
| **Governance model** | LangSmith Fleet: agent identity, permissions, sandboxes. Dynamic tool selection based on auth state | Task guardrails: validation checks after agent output | Agent OS: intercepts every action before execution, sub-ms latency. Zero-trust Ed25519 identity | Semantic policy-as-code: tool + parameters + conditions, per-agent identity |
| **Scope** | LangChain agents only | CrewAI agents only | Framework-specific hooks (LangChain callbacks, CrewAI decorators, ADK plugins) | Any agent speaking MCP or HTTP — framework-agnostic |
| **Policy definition** | Code (Python) | Code (Python decorators) | Code (Python/.NET) | YAML, git-versionable |
| **Architecture** | In-process | In-process | In-process (middleware) | Out-of-process (sidecar proxy) |
| **Cross-agent** | No — LangChain only | No — CrewAI only | Yes — across supported frameworks | Yes — any agent, any framework |
| **Trace** | LangSmith (proprietary SaaS) | CrewAI logs | Built-in audit trail | Centralized trace, per agent + tool + params |

**Key distinctions:**

**LangChain** has real governance features (Fleet, sandboxes, dynamic tool selection) but they are **framework-locked**. If you run LangChain + Claude Code + Cursor, LangSmith only sees the LangChain agents.

**CrewAI guardrails** are **post-hoc validation** — they check the output after the agent acts, not before. This is quality control, not governance. You can't prevent a dangerous action; you can only reject its result.

**Microsoft Agent Governance Toolkit** (released April 2, 2026) is the closest comparable to Agent Mesh. The core difference is architectural:

- **Microsoft AGT** = **middleware** — runs inside the agent's process, hooks into framework callbacks. Sub-millisecond latency, deep integration, but coupled to supported frameworks (LangChain, CrewAI, ADK, Semantic Kernel).
- **Agent Mesh** = **sidecar proxy** — runs outside the agent's process, intercepts at protocol level. Works with any agent without code changes, but adds a network hop (even if local).

This is the same distinction as Express middleware vs Envoy proxy. Both are valid. Microsoft AGT is better when you control the agent's code and want minimal latency. Agent Mesh is better when you have heterogeneous agents (CLI tools, custom bots, third-party frameworks) and need a single governance layer without modifying each one.

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
