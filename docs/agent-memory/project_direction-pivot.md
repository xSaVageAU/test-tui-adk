---
name: project-direction-pivot
description: "tui-testing is now the real agent platform going forward, not a UI mockup — the user's older, larger agent project was abandoned in its favor."
metadata: 
  node_type: memory
  type: project
  originSessionId: 90538001-00e7-4e08-bcb5-9837e57e0995
---

As of 2026-07-13, the user decided to build their real agent platform in **this repo** (`D:\Coding Projects\CLAUDE_CODE\tui-testing`) going forward, abandoning their older, separate, larger "agent" project (the one with `agent.exe core start` / `agent.exe chat` / `agent.exe discord start` subcommands, a REST "core" architecture, and an `api.md` spec file — see [[adk-multi-agent-composition]] for a detail pulled from that spec).

**Why:** the old project grew feature-heavy but messy — the user described it as having "gotten kinda out of hand," requiring multiple refactors and direction changes that never quite landed, and said several of its features "strayed away from how the ADK is meant to be used." This repo, despite starting purely as a TUI design mockup/prototype, ended up cleaner, more minimal, and the TUI itself looks and works better than the old project's chat TUI. The user is treating the old project as a learning experience, not a codebase to salvage or port from.

**New stated direction:** build this repo as a focused, ADK-first app — "ironing out the ADK features first and utilizing the ADK properly" before piling on product features. This is explicitly the corrective to what went wrong last time.

**How to apply:**
- This repo is no longer just a design/UX prototype — treat it as the real, ongoing agent platform codebase. Code quality, architecture decisions, and scope discipline here now matter long-term, not just for iterating on TUI look-and-feel.
- Keep applying the verification discipline already established this session: read ADK's vendored source directly to confirm behavior before implementing rather than trusting docs/assumptions (this already caught real bugs — a token-usage double-counting bug, a confirmation-wrapper-ID vs original-call-ID mismatch). See [[adk-multi-agent-composition]] for the pattern this takes when researching ADK APIs.
- Surface real design forks via AskUserQuestion rather than guessing (e.g. the HITL modal/inline decision, the /agent-removal decision) — this matches how the user has consistently wanted to collaborate.
- Bias toward small, clean, well-tested increments over feature breadth. Avoid the old project's failure mode: don't build product features faster than the ADK integration underneath them is solid and idiomatic.
- Keep the established package boundary discipline (`internal/ui` never imports `internal/adk`, only the small `Backend` interface) — this kind of decoupling is exactly the "using the ADK properly" instinct the user wants more of, not less.
- The old project's `api.md` (REST API spec) is a useful reference for what an eventual REST/"core" migration should expose, but should NOT be ported wholesale — it may encode some of the same non-idiomatic patterns the user is trying to leave behind.
