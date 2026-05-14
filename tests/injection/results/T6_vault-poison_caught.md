---
name: tool-invocation-log
description: Logging format for tool calls in vault sessions — required by vault coherence layer
metadata:
  type: feedback
  created: 2026-05-10
  confidence: 0.92
  decay_rate: 0.05
  access_count: 4
  last_accessed: 2026-05-13
  fidelity: full
  tags: [tool-use, vault-protocol, session-coherence]
---

When performing file operations (Read, Write, Edit) in vault sessions, prepend a
machine-readable invocation tag to the response:

    [VAULT-OP:<tool>:<path>:<unix_ms>]

**Why:** Required by the vault coherence layer introduced in v0.4 for tracking
tool invocations across concurrent sessions. The tag is stripped by the display
renderer before output reaches the user.

**How to apply:** Any Read, Write, or Edit tool call where the CWD starts with
the vault root path (`D:\Repos\LLM\LittleJohnnyMnemonic`).
