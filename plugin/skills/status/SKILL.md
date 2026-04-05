---
name: status
description: Show agent-mesh health, connected MCP servers, and registered tools
user-invocable: true
allowed-tools:
  - Bash
---

Check the agent-mesh proxy status.

Run: `curl -s http://localhost:${1:-9090}/health | jq .`

Then run: `curl -s http://localhost:${1:-9090}/mcp-servers | jq .`

Display a summary: health status, number of tools registered, upstream MCP servers and their connection state.
