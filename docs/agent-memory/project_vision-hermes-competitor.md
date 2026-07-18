---
name: agent-platform-vision
description: "Product vision — a single-binary, portable, fast Go/ADK alternative to popular Python/Node agents like Hermes (Nous Research) and OpenClaw."
metadata: 
  node_type: memory
  type: project
  originSessionId: 90538001-00e7-4e08-bcb5-9837e57e0995
---

The user's goal for the real agent platform (now being built in this repo — see [[project-direction-pivot]]) is to compete with popular agent products, especially **Hermes** by Nous Research (very popular as of mid-2026) and **OpenClaw** (not yet tried by the user).

**Why Hermes is the benchmark:** the user tried it and found it "really cool," but its main downside is heavy setup — a script that downloads/sets up Python, Node, etc. The user wants something more portable (a single compiled binary) and probably faster, using Go + ADK instead of Hermes' Python stack.

**Important caveat the user raised themselves:** their understanding of Hermes is based on using it, not reading its source, and they explicitly said Python-vs-Go architectural assumptions may not translate directly — treat feature-parity ideas sourced from "how I assume Hermes works" as unconfirmed until checked, not a settled spec.

**Confirmed via web research on 2026-07-13** (Hermes' own GitHub/marketing — nousresearch/hermes-agent, hermes-agent.org): actual Hermes feature set —
- Persistent cross-session memory + a self-improving "skill" learning loop (creates/improves skills from experience, searches its own past conversations, builds a model of the user over time) — this is Hermes' real headline/differentiating feature.
- Bring-your-own-model: Nous Portal, OpenRouter, OpenAI, custom endpoints, no lock-in.
- One gateway process serving many surfaces: Telegram, Discord, Slack, WhatsApp, Signal, CLI.
- TUI with multiline editing, slash-command autocomplete, conversation history, streaming tool output, and **interrupt-and-redirect mid-response**.
- A "Tool Gateway": web search, AI image generation, TTS, browser automation.
- A desktop app (macOS/Windows/Linux, June 2026) with one-click install/self-update — not a CLI/TUI-comparable target.

**How to apply — map each to its nearest ADK-native equivalent rather than assuming a from-scratch build:**
- Persistent memory/learning loop → **tried and reverted 2026-07-13.** ADK's `memory.Service` (`SearchMemory`/`AddSessionToMemory`) turned out to be raw-transcript keyword search, not durable remembered facts — see [[adk-memory-attempt-and-revert]] for the full story and what a real attempt would need to look like (a deliberate extraction/judgment step, closer to how Claude's own memory system works). Memory is intentionally **not implemented** right now; the user wants to circle back and build it properly later rather than keep the mismatched version.
- Single gateway, many surfaces → already the user's own planned "core + thin clients" REST architecture (see [[project-direction-pivot]]); agent-as-tool (the delegation pattern just decided on, not transfer) is the right fit for keeping one consistent voice across all of them.
- Bring-your-own-model → `model.LLM` is a plain interface in ADK Go (`gemini.NewModel` is just one implementation) — provider-swapping is architecturally already available, not a heavy lift.
- Tool Gateway (web search/image/TTS/browser) → just more `functiontool.New(...)`-style tools, same pattern as the existing `list_files` tool — no new architecture, only per-tool implementation effort.
- Interrupt-and-redirect mid-response → **not implemented** in this project's TUI. Real gap if feature parity matters — flagged, not yet scheduled.
- Single-binary portability (no Python/Node setup) is a genuine, real differentiator this Go+ADK approach already has over Hermes. Nothing in Hermes' actual feature list requires giving it up — protect it; don't accidentally erode it chasing parity (e.g. by pulling in a heavy runtime dependency for one feature).
