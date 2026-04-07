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

### Agent frameworks and their built-in governance

Agent frameworks orchestrate LLM workflows. Most now include governance-adjacent features — callbacks, guardrails, hooks — but all are **in-process middleware**, locked to their own ecosystem.

| | **Anthropic (Agent SDK + Claude Code)** | **OpenAI Agents SDK** | **LangChain / LangGraph** | **Google ADK** | **CrewAI** | **HuggingFace smolagents** | **Amazon Bedrock** |
|---|---|---|---|---|---|---|---|
| **Governance model** | 18 lifecycle hooks, `can_use_tool`, deny-wins chaining. Claude Code: PreToolUse/PostToolUse shell hooks | Input/output/tool guardrails, fail-fast parallel execution | LangSmith Fleet: agent identity, ABAC, sandboxes. LangGraph 1.0: HITL middleware | 6 callbacks (before/after agent/model/tool) | Task guardrails: post-hoc validation after agent output | Sandbox (E2B/Docker) + basic HITL | 6 safeguards: content moderation, PII redaction, prompt attack detection, hallucination detection |
| **Architecture** | In-process (middleware) | In-process (middleware) | In-process + SaaS (LangSmith) | In-process (middleware) | In-process (middleware) | In-process (sandbox) | Platform service (AWS-managed) |
| **Scope** | Claude Agent SDK / Claude Code only | OpenAI SDK only | LangChain/LangGraph only | ADK only | CrewAI only | smolagents only | Bedrock only |
| **Policy definition** | Code (Python/hooks) | Code (Python) | Code (Python) | Code (Python/TS) | Code (Python decorators) | Code (Python) | AWS Console / API |
| **Cross-agent** | No | No | No | No | No | No | No |
| **Trace** | SDK logs | OpenAI platform | LangSmith (proprietary SaaS) | Google Cloud Trace | CrewAI logs | Local logs | CloudWatch |

**Key observations:**

- **Every framework** now has some form of governance hooks. This validates the need — but each solution is **framework-locked**.
- **Anthropic Agent SDK** has the richest hook system (18 lifecycle hooks, deny-wins), but only governs agents built with that SDK.
- **OpenAI Agents SDK** runs guardrails in parallel with agent execution (fail-fast) — good UX, but OpenAI-only.
- **Google ADK** callbacks are the subject of growing community adoption (cost/latency tracking via hooks), but ADK-only.
- **CrewAI guardrails** are **post-hoc** — they validate output after the action, not before. Quality control, not governance.
- **Amazon Bedrock** is the most comprehensive platform solution but is AWS-locked and focused on content-level guardrails (PII, toxicity), not semantic tool-call policy ("allow create_issue, deny delete_repo").
- **None** provide cross-agent governance. If you run Claude Code + Cursor + a LangChain bot, no single framework sees all three.

### Governance toolkits and standalone products

Beyond framework-embedded hooks, a new category of **standalone governance tools** is emerging — some as middleware, some as external services, some as sidecars.

| | **Microsoft AGT** | **Aegis** | **Galileo Agent Control** | **Cerbos** | **Agent Mesh** |
|---|---|---|---|---|---|
| **What it is** | 7-package middleware (Python/TS/Rust/Go/.NET) | Governance engine ("Istio + OPA for agents") | Open-source control plane (Apache 2.0) | Authorization engine (Go), expanded to AI/MCP | Sidecar governance proxy |
| **Architecture** | In-process middleware (hooks into framework callbacks) | Sidecar / forward proxy (Envoy ext_authz + OPA) | External service (centralized policy evaluation) | Sidecar / standalone PDP | Out-of-process sidecar proxy |
| **Policy model** | Code (Python/.NET), per-framework hooks | OPA policy bundles (Rego) | Declarative (deny/steer/warn/log/allow) | YAML (Cerbos policies) | YAML, first-match-wins, glob patterns |
| **Agent identity** | Zero-trust Ed25519 | Per-agent via OPA context | Per-agent | Per-principal | Per-agent via Bearer token or MCP flag |
| **Cross-agent** | Yes — across supported frameworks | Yes — protocol-level interception | Yes — vendor-neutral | Yes — any caller | Yes — any MCP or HTTP agent |
| **Approval workflows** | No | Slack/Teams approval, one-time override tokens | No | No | Built-in (HTTP + MCP + webhooks + callbacks) |
| **OWASP Agentic coverage** | 10/10 | Not specified | Not specified | Not specified | 6/10 |
| **Deployment** | In-process (per framework) | Kubernetes sidecar | SaaS + self-hosted | K8s sidecar, standalone, Lambda | Single binary, local-first, zero deps |
| **Best for** | Teams controlling agent code, wanting sub-ms latency | K8s-native teams, OPA shops | Enterprise multi-framework, centralized policy | Teams already using Cerbos for authz | Dev-first, heterogeneous agents, local + CI |

**Key distinctions:**

**Microsoft AGT** (April 2026) is the most comprehensive solution and covers all 10 OWASP Agentic risks. But it is **middleware** — it hooks into each framework's callbacks (LangChain, CrewAI, ADK, Semantic Kernel). If an agent doesn't have a supported hook, AGT can't govern it.

**Aegis** is the closest architectural match to Agent Mesh — sidecar proxy, protocol-level interception, "Istio + OPA for agents." The key differences: Aegis is **Kubernetes-oriented** (heavier infra), uses **OPA/Rego** for policy (steeper learning curve vs YAML), and has no built-in human approval workflow.

**Galileo Agent Control** (March 2026) takes a centralized control-plane approach with enterprise partnerships (CrewAI, Cisco, Strands). Vendor-neutral in theory, but still requires per-framework integration hooks for interception. Enterprise SaaS model behind the open-source layer.

**Cerbos** is a mature authorization engine that recently expanded to AI agent and MCP authorization. Strong as a policy decision point, but it's a **generic authz engine** — no agent-specific features like approval workflows, trace, or loop detection.

**Agent Mesh** = **sidecar proxy** — runs outside the agent's process, intercepts at protocol level. Works with any agent without code changes, but adds a network hop (even if local). The unique combination: single binary + YAML policy + built-in approval/trace/rate-limiting + MCP-native + zero dependencies.

### The middleware vs sidecar divide

The governance landscape splits along the same architectural line as the service mesh world:

| | **Middleware** (in-process) | **Sidecar** (out-of-process) |
|---|---|---|
| **Examples** | Microsoft AGT, all framework hooks | Agent Mesh, Aegis, Cerbos |
| **Latency** | Sub-millisecond | Network hop (local: ~1ms) |
| **Integration** | Code changes per framework | Zero code changes, protocol-level |
| **Scope** | Supported frameworks only | Any agent speaking HTTP or MCP |
| **Failure mode** | Crashes with the agent | Independent process, fail-closed |
| **Best for** | Homogeneous fleet, controlled code | Heterogeneous agents, CLI tools, third-party |

Microsoft AGT is better when you control the agent's code and want minimal latency. Agent Mesh is better when you have heterogeneous agents (CLI tools, custom bots, third-party frameworks) and need a single governance layer without modifying each one.

## The adoption journey

Most teams building agents today follow a predictable path. Governance needs emerge at each transition — and the right tool depends on the phase.

### The three entry points

**Agent CLIs** (Claude Code, Cursor, Gemini CLI) — A developer, one agent, their code. The dev approves everything manually. This is single-player mode and represents the vast majority of agent usage today.

**Agent frameworks** (LangChain, CrewAI, Agent SDK) — A team building a custom agent that runs in production, calls APIs, performs actions on behalf of users. This is multi-agent or agent-as-service territory.

**Cloud platforms** (Bedrock, Vertex AI) — Managed, cloud-native, governance built-in but vendor-locked. Enterprise teams start here.

### When governance becomes a need

```
Phase 1: "I code with Claude Code"
         → No governance needed — I'm right here approving everything

Phase 2: "I have 2-3 agents (Claude Code + Cursor + a LangChain bot)"
         → I want the same rules for all of them
         → AGENT MESH enters here

Phase 3: "My agent runs in prod without a human"
         → I MUST have governance
         → AGENT MESH, framework hooks, or AGT

Phase 4: "I have 10 agents in prod, K8s, compliance requirements"
         → Aegis, AGT, Bedrock Guardrails, or enterprise solutions
```

### Agent Mesh's sweet spot: Phase 1 → 2 → 3

The critical moment is when a developer adds a second agent or lets the first one run autonomously. The scenarios:

| Scenario | Without Agent Mesh | With Agent Mesh |
|---|---|---|
| Solo dev + Claude Code | Approval mode is enough | Not necessary |
| Claude Code + Cursor on the same project | Each has its own rules, no unified view | 1 YAML, same rules, 1 trace |
| LangChain bot running autonomously | Code Python guardrails inside the bot | Put the bot behind the mesh, YAML policy |
| Agent SDK with human approval | Code the webhook yourself | Built-in (HTTP + MCP + webhooks) |
| Multiple agents, different trust levels | Configure each one separately | One config, per-agent identity and permissions |

The pattern: Agent Mesh is the thing you put in front of your agents in 5 minutes — before the lack of governance becomes a problem. It grows with you from local dev to production, without requiring a platform migration.

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
