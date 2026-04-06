# Middleware vs Sidecar: two ways to govern AI agents

I've spent the last decade building governance layers — schema registries for Kafka, policies for Kong, contract validation for event-driven systems. Always the same job: sit between components, enforce rules, keep a trace.

When I started running multiple AI agents (Claude Code, Cursor, custom LangChain bots), I noticed the same gap I'd seen before. Every agent has some permission model. None of them talk to each other. No shared policy. No unified trace. Sound familiar? It's 2018 microservices all over again, before service meshes became a thing.

So I built [agent-mesh](https://github.com/flux7art/agent-mesh) — a sidecar proxy for agent tool calls. YAML policy, centralized traces, per-agent identity. One Go binary, works with anything that speaks MCP or HTTP.

Four days ago, Microsoft dropped the [Agent Governance Toolkit](https://github.com/microsoft/agent-governance-toolkit). Open-source, MIT. Same problem space. Different architecture.

Worth comparing honestly — especially now that OWASP gave us a shared vocabulary for what "agent security" actually means.

Full disclosure: Microsoft AGT dropped while I was already building agent-mesh. I hadn't mapped against OWASP until I saw their claim of covering all 10. That comparison forced me to think harder about what a sidecar actually covers — and what it doesn't.

## The two approaches

**Microsoft AGT** is a middleware. It runs *inside* your agent's process — Python callbacks for LangChain, decorators for CrewAI, plugins for ADK. Sub-millisecond policy checks. Deep integration. You add a few lines of code and your agent is governed.

**agent-mesh** is a sidecar proxy. It runs *next to* your agents — intercepts tool calls at the MCP/HTTP level. No code changes. Your agent points to agent-mesh instead of the real backend. Agent-mesh checks the policy, logs the trace, forwards or denies.

If you've worked with API gateways, you already know this pattern. Microsoft AGT is Express middleware. agent-mesh is Envoy.

## When each approach wins

Middleware wins when:
- You own the agent's code
- You're all-in on one framework (LangChain, CrewAI, Semantic Kernel)
- You want sub-millisecond overhead
- Your agents are Python or .NET

Sidecar wins when:
- You run heterogeneous agents (CLI tools + frameworks + custom bots)
- You can't or won't modify agent code
- You want one YAML policy for all agents
- Your agents are Claude Code, Cursor, Gemini CLI, or anything MCP-native

That last point matters. Claude Code, Cursor, Gemini CLI, Cline, OpenCode — none of these expose Python callback hooks. Microsoft AGT can't govern them. A sidecar proxy can, because it operates at the protocol level.

## What about LangChain and CrewAI?

LangChain has LangSmith Fleet — agent identity, permissions, sandboxes. Real features. But scoped to LangChain. If you also run Claude Code and a custom HTTP agent, LangSmith doesn't see them.

CrewAI has task guardrails — validation after the agent produces output. That's quality control, not governance. You can't block a dangerous action before it happens.

Neither provides cross-agent policy-as-code.

## What about Kong, Gravitee, Apigee?

Different layer entirely. These are AI gateways — they govern north-south traffic between your apps and LLMs. Rate limiting, PII redaction, semantic routing, model abstraction. Important stuff, but not the same problem.

They don't know which *agent* made the call, don't enforce *action-level* policy (allow `create_issue`, deny `delete_repo`), and don't differentiate permissions per agent identity.

Gateways and governance meshes are complementary. You can run Gravitee in front for LLM routing and agent-mesh behind for tool call governance. Different layers, different jobs.

```
Agent CLI / Framework
      │
  AI Gateway (Kong/Gravitee/Apigee) ← LLM routing, PII, rate limits
      │
  Governance Mesh (agent-mesh) ← semantic policy, agent identity, trace
      │
  Tools (GitHub, DB, APIs)
```

## Mapping to OWASP Agentic Top 10

OWASP published the [Top 10 for Agentic Applications](https://genai.owasp.org/resource/owasp-top-10-for-agentic-applications-for-2026/) — the first serious attempt at cataloging what can go wrong when AI agents act in the real world. It's peer-reviewed by 100+ security researchers. If you build or run agents, you should read it.

Here's how the landscape maps against it:

| OWASP Risk | What it means | Who covers it |
|---|---|---|
| **ASI01 — Goal Hijacking** | Poisoned input redirects agent objectives | Prompt-level defenses (none of the governance tools below) |
| **ASI02 — Tool Misuse** | Agent uses tools in unintended or dangerous ways | **agent-mesh** (semantic policy on params), **Microsoft AGT** (action interception) |
| **ASI03 — Identity & Privilege Abuse** | Agent abuses its tokens, roles, sessions | **agent-mesh** (per-agent identity, temporal grants), **Microsoft AGT** (zero-trust Ed25519) |
| **ASI04 — Delegated Trust** | Blind trust between agents | **agent-mesh** (policy-as-code between agents), **Microsoft AGT** (trust scoring) |
| **ASI05 — Uncontrolled Autonomy** | Critical decisions without human validation | **agent-mesh** (human approval workflow), CLI tools (approval prompts) |
| **ASI06 — Memory Poisoning** | Persistent memory is poisoned with malicious data | Not directly addressed by governance layers — needs runtime sandboxing |
| **ASI07 — Multi-Agent Comms** | Inter-agent communication is not secured | **agent-mesh** (JWT auth on sidecar), **Microsoft AGT** (Agent Mesh component) |
| **ASI08 — Cascading Failures** | One agent failing triggers a chain reaction | **agent-mesh** (rate limiting, loop detection), gateways (circuit breaking) |
| **ASI09 — Emergent Behavior** | Agent develops unanticipated behavior | Observability + traces (LangSmith, agent-mesh traces, Microsoft AGT audit) |
| **ASI10 — Rogue Agents** | Compromised agent diverges from intended behavior | **agent-mesh** (policy enforcement, deny-by-default), **Microsoft AGT** (trust scoring) |

A few things stand out:

**No single tool covers everything.** ASI01 (goal hijacking) is a prompt-level problem — no governance proxy or middleware can fix it. ASI06 (memory poisoning) needs runtime isolation, not policy.

**The middleware vs sidecar split shows up clearly.** Microsoft AGT and agent-mesh cover roughly the same risks (ASI02-05, ASI07-08, ASI10), but from different positions in the stack. Microsoft intercepts inside the process. agent-mesh intercepts at the protocol boundary. Same coverage, different blast radius if the governance layer itself is compromised.

**Gateways (Kong, Gravitee, Apigee) cover almost none of this.** They weren't designed for it. They govern LLM traffic, not agent behavior. That's fine — it's a different layer. But if someone tells you their AI gateway handles agentic security, check which ASIs it actually addresses.

## The real question

The agentic ecosystem is splitting into two governance models:

1. **Framework-level** — deep, fast, but locked to one stack
2. **Protocol-level** — universal, zero code change, but adds a hop

My bet is that protocol-level wins long-term. The same way Envoy won over framework-specific circuit breakers — because in practice, nobody runs just one framework. You'll have Claude Code for coding, a LangChain pipeline for data, a custom agent for ops. You need one policy layer that sees all of them.

But I'm biased. I built the sidecar.

---

*[agent-mesh](https://github.com/flux7art/agent-mesh) is open-source, written in Go. One binary, one YAML config. Works with any agent that speaks MCP or HTTP.*
