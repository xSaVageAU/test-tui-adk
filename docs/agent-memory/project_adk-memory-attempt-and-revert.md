---
name: adk-memory-attempt-and-revert
description: "Long-term memory was implemented via ADK's memory.Service, found to be raw-transcript keyword search rather than real memory, and reverted at the user's request."
metadata: 
  node_type: memory
  type: project
  originSessionId: 90538001-00e7-4e08-bcb5-9837e57e0995
---

On 2026-07-13, persistent long-term memory was built into tui-testing's `internal/adk` package (a custom sqlite-backed `memory.Service` implementation, wired to the root agent via ADK's `preloadmemorytool`), then **reverted the same day** after the user tried it and it didn't match what they wanted. See [[project-direction-pivot]] for the broader context this sits inside, and [[agent-platform-vision]] for how this ties to the Hermes-parity goal.

**What ADK's `memory.Service` actually is, confirmed by reading the source (`memory/inmemory.go`, `tool/preloadmemorytool/tool.go`):** not a fact store. `AddSessionToMemory` performs no summarization or judgment — it stores every text-bearing message verbatim. `SearchMemory` is plain keyword-overlap matching against that raw text (no embeddings, no semantic similarity), and ADK's own `preloadmemorytool` has **no cap or ranking of its own** — every match gets dumped into the system prompt. This is much closer to full-text search over chat history than to durable "remembered facts."

**What actually broke:** the user reported a fresh session (new UUID, no shared history) behaving as if it could see "exactly what I said last session." Root cause: the initial matching algorithm counted *any* shared word — including stopwords like "the"/"is"/"my" — as a match, so an ordinary new sentence pulled in nearly the entire memory table from prior sessions. Fixed with a stopword list + relevance ranking + a result cap (`maxMemoryResults`), verified with a targeted test — but even after that fix, the user decided the underlying approach (raw transcript search) wasn't what they wanted at all, regardless of tuning.

**The user's actual mental model, stated directly:** "a simple file or sqlite where it would just store text to remember later" — i.e. durable, distilled facts, retrievable reliably regardless of exact wording overlap. Not what ADK's shipped primitive does.

**Comparison the user asked for and got an answer on:** how Claude's own memory system works, as a second concrete reference point beyond Hermes (whose actual mechanism is unconfirmed — only marketing-level claims were found via web search, see [[agent-platform-vision]]). Claude's memory has an explicit **extraction/judgment step** before anything is written (an active decision that something is worth persisting, with a stated *why*) and stores **distilled statements**, not raw transcript quotes — the opposite of what `memory.Service`/`AddSessionToMemory` does by default.

**Current state:** memory is **fully removed** — `internal/adk/memstore.go` deleted, `MemoryService`/`preloadmemorytool` wiring pulled out of `client.go`, `Client` no longer carries a `mem`/`sessions` field. Persistent *sessions* (separate feature, sqlite-backed via ADK's own `session/database`) were kept — only memory was reverted, since it was the only piece that didn't match what was wanted.

**How to apply, when revisiting this later (user's words: "we will circle back to Hermes/Claude style memory in the future"):**
- Don't reach for ADK's `memory.Service` + `preloadmemorytool` as shipped — it's a search primitive, not a memory primitive. If used at all, it would need to sit *underneath* a from-scratch extraction layer, not be the whole feature.
- The real design work is the extraction step: something (an LLM call, likely) that decides *what's worth remembering* from a conversation and writes a short, distilled statement — not the storage/retrieval mechanics, which are comparatively easy (a sqlite table, as already built once).
- The user is explicitly unsure how deep they want to go with this ("it's a complicated feature to get right and make it so it's used appropriately — not overused or underused") — treat this as a real open design question requiring discussion before implementation, not a feature to unilaterally scope and build.
- Hermes' actual mechanism is still unconfirmed beyond marketing copy — worth a real research pass (their GitHub source, not just their site) if/when this gets revisited, rather than continuing to guess.
