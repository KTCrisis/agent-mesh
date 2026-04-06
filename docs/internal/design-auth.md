# Design: Authentication Strategy

> Status: design — not yet implemented
> Date: 2026-04-05

## Context

Agent-mesh sits between agents and tools. Today, agent identity is either:
- **MCP mode**: hardcoded via `--mcp-agent claude` (the sidecar process controls identity)
- **HTTP mode**: self-declared via `Authorization: Bearer agent:<id>` (no validation)

MCP mode is secure by design — the process boundary guarantees identity. HTTP mode is not — any client can claim any agent ID.

## When auth matters

| Mode | Transport | Identity source | Secure? |
|------|-----------|----------------|---------|
| MCP (stdio) | Process pipe | `--mcp-agent` flag | Yes — process boundary |
| HTTP (local) | localhost:9090 | Bearer header | Weak — any local process can spoof |
| HTTP (network) | exposed port | Bearer header | No — open to network |

**Conclusion**: auth is only needed for HTTP mode. MCP mode is already authenticated by the transport.

## Envoy's model (reference)

Envoy separates three layers:

1. **Authentication** — JWT filter (signature + expiration + issuer) or mTLS
2. **Authorization** — RBAC filter or ext_authz (delegates to external service)
3. **Rate limiting** — external rate limit service or local circuit breaker

Agent-mesh already has layers 2 (policy engine) and 3 (ratelimit). Layer 1 is missing for HTTP mode.

## Proposed: JWT HMAC-SHA256

### Why JWT over API keys

| | API key | JWT HMAC |
|-|---------|----------|
| Identity | Lookup in config | Embedded in token (self-contained) |
| Expiration | Manual revocation only | Built-in `exp` claim |
| Tamper-proof | No (shared secret = the key itself) | Yes (HMAC signature) |
| Stateless validation | No (requires config lookup) | Yes (verify signature + claims) |
| Standard | Proprietary | RFC 7519 |
| Recognizable | No | Yes — CISOs, auditors, devs know JWT |

API keys are simpler but don't carry expiration or resist tampering. For a governance proxy, the token itself should be verifiable without hitting a database.

### Why HMAC (symmetric) first

| | HMAC (HS256) | RSA/ECDSA (RS256/ES256) |
|-|-------------|------------------------|
| Key management | One shared secret in config | Key pair (private signs, public verifies) |
| Token generation | Sidecar CLI (`agent-mesh token`) | External issuer (Keycloak, Auth0, custom) |
| Use case | Single operator, local sidecar | Multi-tenant, federated identity |
| Complexity | ~100 lines, stdlib only | ~150 lines + JWKS endpoint/file |
| Dependencies | `crypto/hmac`, `crypto/sha256` | `crypto/rsa` or `crypto/ecdsa` + PEM parsing |

HMAC fits the sidecar model: one operator, one secret, tokens generated locally. RSA/ECDSA is for when tokens come from an external identity provider.

### Token format

```
Header:  {"alg": "HS256", "typ": "JWT"}
Payload: {"sub": "travel-agent", "iat": 1743868800, "exp": 1746460800}
```

Minimal claims:
- `sub` — agent ID (maps to policy engine's agent matching)
- `iat` — issued at
- `exp` — expiration (optional, tokens can be long-lived)

No scopes in the token. The policy engine already handles authorization. JWT handles authentication only — "this is really travel-agent and the token hasn't expired."

### Config

```yaml
# config.yaml
auth:
  secret: "change-me-in-production"   # HMAC-SHA256 signing key
  # If absent, HTTP mode falls back to Bearer agent:<id> (current behavior)
```

### CLI

```bash
# Generate a token
agent-mesh token --agent travel-agent --expires 720h
# eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...

# Generate a token that never expires
agent-mesh token --agent admin-agent

# Verify a token (debug)
agent-mesh token --verify eyJhbG...
# ✓ agent=travel-agent exp=2026-05-05T00:00:00Z
```

### Request flow change

```
HTTP request
  → Authorization: Bearer <jwt>
  → [NEW] Validate JWT signature + expiration → 401 if invalid
  → Extract agent ID from sub claim
  → Rate limit check → 429 if exceeded
  → Registry lookup → 404 if unknown tool
  → Policy evaluate → 403 if denied
  → Forward → trace → respond
```

HTTP 401 (Unauthorized) for auth failures, not 403 — standard separation of authn vs authz.

### Handler integration

```go
// In proxy/handler.go, before rate limit check:
if h.Auth != nil {
    agentID, err := h.Auth.Validate(authHeader)
    if err != nil {
        // 401 Unauthorized
    }
}
```

### Implementation scope

```
auth/
├── jwt.go       # Sign + Validate (HMAC-SHA256, ~80 lines)
└── jwt_test.go  # Round-trip, expiration, tampering, missing secret
```

One package, one file, stdlib only. No external JWT library.

## Future: asymmetric JWT (RS256/ES256)

When agent-mesh is deployed in multi-tenant or enterprise contexts where tokens are issued by an external identity provider:

```yaml
auth:
  # Option A: JWKS URL (fetched + cached)
  jwks_url: "https://auth.company.com/.well-known/jwks.json"
  
  # Option B: local public key file
  public_key: "/etc/agent-mesh/signing-key.pub"
  
  issuer: "https://auth.company.com"   # Validate iss claim
  audience: "agent-mesh"               # Validate aud claim
```

This is the Envoy JWT filter model. The sidecar validates tokens but doesn't issue them. Token lifecycle (generation, rotation, revocation) is managed by the identity provider.

**Not needed until**: multiple teams deploy agents against a shared agent-mesh, or an enterprise wants to integrate with their existing IAM.

## Future: ext_authz hook

For organizations that want to delegate auth decisions entirely to an external service (OPA, custom policy service):

```yaml
auth:
  ext_authz:
    url: "http://localhost:8181/v1/data/agent_mesh/allow"
    timeout: 500ms
    failure_mode: deny   # deny if auth service is down
```

The sidecar sends `{agent, tool, params}` to the external service and gets back `{allow: true/false}`. Same pattern as Envoy's ext_authz filter.

**Not needed until**: the organization has an existing policy-as-code system (OPA, Cedar) they want agent-mesh to defer to.

## Migration path

```
v0.3.x  Bearer agent:<id>           (current, no validation)
v0.4.0  JWT HMAC optional           (auth.secret in config enables it)
v0.5.x  JWT RSA/ECDSA + JWKS       (external identity providers)
v0.6.x  ext_authz hook              (full delegation)
```

Each step is additive. No breaking changes. If `auth` is not in the config, behavior stays the same.
