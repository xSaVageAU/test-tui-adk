---
name: user-engineering-values
description: "User values minimal, idiomatic code over feature breadth and will abandon sunk-cost work for a cleaner foundation."
metadata: 
  node_type: memory
  type: user
  originSessionId: 90538001-00e7-4e08-bcb5-9837e57e0995
---

The user is building an AI agent platform (Go, Google ADK v2, Bubble Tea TUI) and has shown a consistent preference for **minimal, clean, idiomatic code over feature breadth**.

Evidence: on 2026-07-13 the user abandoned a larger, more feature-complete prior project in favor of continuing in this repo (tui-testing) — see [[project-direction-pivot]] — specifically because the old project had "gotten kinda out of hand" and its features had "strayed away from how the ADK is meant to be used," despite having more functionality than this repo. The user explicitly chose the smaller, cleaner codebase over the one with more features already built.

**How to apply:**
- Don't equate "more features" with progress in this user's eyes — a clean, correct, idiomatic implementation of less is preferred over a large surface area built on shaky foundations.
- When a design choice has an "ADK-idiomatic" option and a "custom workaround" option, prefer surfacing the idiomatic one and explain the tradeoff, rather than defaulting to whatever's fastest to build — this user has explicitly framed straying from idiomatic framework usage as a past mistake to avoid repeating.
- This user responds well to (and has asked for) rigorous verification: reading vendored dependency source directly to confirm real behavior rather than trusting docs, building/vetting/smoke-testing every change, writing temporary throwaway tests to verify non-obvious logic (then deleting them). Keep doing this without being asked.
- Comfortable making a clean break from prior work rather than incrementally patching something fundamentally messy — don't assume attachment to existing code just because it exists; the user has already demonstrated willingness to discard a larger project wholesale.
