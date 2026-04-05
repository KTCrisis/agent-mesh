---
name: approve
description: List pending approvals and approve/deny them
user-invocable: true
argument-hint: "[approve|deny] [id]"
allowed-tools:
  - Bash
  - mcp__agent-mesh__approval_pending
  - mcp__agent-mesh__approval_resolve
---

Manage agent-mesh approval requests.

If no arguments: list all pending approvals using the approval_pending tool.

If arguments provided:
- $0 = decision (approve or deny)
- $1 = approval ID

Use the approval_resolve tool with the given decision and ID.

Display the result clearly.
