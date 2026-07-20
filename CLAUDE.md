# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A terminal AI agent platform — a single-binary Go competitor to things like Hermes/OpenClaw — built on Google's Agent Development Kit for Go (`google.golang.org/adk/v2`) with a Bubble Tea v2 TUI. The repo name ("tui-testing") is a historical placeholder from when this was a UI mockup; it is now the real product. `internal/appdir/appdir.go` has the one constant to change when it gets a real name.

## Commands

```
go build ./...          # build
go vet ./...            # vet — run alongside build; this pair is the default verification
go test ./...           # all tests
go test ./internal/ui -run TestName   # single test
go run .                # launch the TUI (needs an interactive terminal; a Gemini key via GOOGLE_API_KEY or /key for a live backend)
```

Verification norms for this repo: `go build` + `go vet` is the default bar. Add tests or deeper verification only when there is a specific reason to distrust a change (concurrency, unconfirmed library behavior) — no reflexive throwaway tests, and don't try to drive the TUI programmatically to "check" a change.

## Architecture

### The one load-bearing boundary

`internal/ui` never imports `internal/adk`. The TUI talks only to the `ui.Backend` interface (`internal/ui/backend.go`), which `*adk.Client` satisfies structurally; `adk` imports `ui` for the contract types (`StreamChunk`, etc.) — the implementer depends on the consumer, never the reverse. `main.go` is the only bridge: it wires `adk.New` in as a `ui.BackendFactory` and adapts the agent-config read/write functions into the plain closures `ui.AppConfig` expects. Keep new cross-package needs flowing through this same shape.

### internal/adk — the agent side

- `client.go` — public API (Client, New, Send, Stream, RespondToConfirmation). The package doc comment here is the map of the whole package.
- `agents.go` / `rootagent.go` / `subagents.go` — builds the agent tree: one root agent, with sub-agents exposed as **agent-as-tool** calls (never transfer targets), so there is exactly one voice in the conversation. Sub-agents cannot do human-in-the-loop confirmation — that's an ADK limitation, not a choice; don't try to add it.
- `eventstream.go` — translates ADK's event model into `ui.StreamChunk`. One field set per chunk; Usage/FinishReason can arrive multiple times per turn.
- `store.go` — sqlite-backed session persistence (pure-Go driver, `glebarez/sqlite`, deliberately CGO-free for the single-binary goal).
- `openrouter/` — second provider besides Gemini, speaking OpenRouter's API and aggregating its stream into the same shapes. Model names are used as-is (no `vendor/` prefix needed).
- `mcpservers.go` — MCP client support: root-agent-only, discovered from `mcpservers/*.toml` config files. MCP toolsets are wrapped in a resilience layer because ADK re-resolves toolsets every turn — a dead server would otherwise kill every subsequent turn.

### internal/adk/tools — the toolset

One file per tool. Each tool self-registers by calling `register(spec{...})` from its own `init()` — adding a tool means writing one new file and touching nothing else. `tools.go` explains the spec shape; which tools an agent actually gets is config (each agent's `tools` list in its JSON), not code.

`gate.go` wraps every tool with confirmation gating (destructive tools require human approval) and resource-conflict serialization (overlapping filesystem paths serialize instead of racing — ADK runs a turn's parallel tool calls in unordered goroutines). The wrapping order is load-bearing; read gate.go's doc comments before changing it. Two ADK gotchas encoded there:

- Any `tool.Tool` wrapper **must override `ProcessRequest` as well as `Run`** — wrapping `Run` alone silently no-ops because ADK invokes tools through `ProcessRequest`.
- Use `ctx.AgentName()`, never `ctx.Agent()`, inside tool context.

`target.go` defines the execution-target abstraction: tools run against a `Target` (local host by default, or a remote machine over SSH/SFTP per `settings.toml`), so the whole toolset relocates wholesale rather than per-tool. `run_shell` supports a background-process lifecycle (`shell_output`/`stop_shell`) on both targets; `main.go` cleans these up on exit.

### internal/ui — the TUI

Bubble Tea **v2** (`charm.land/bubbletea/v2`) — the API differs from v1 in ways that matter (e.g. AltScreen/MouseMode are declarative fields on the `tea.View` returned from `View()`, not Program options). `app.go` is the model root; menus stack via `menustack.go`; HITL tool confirmations are in `hitl.go`/`permissions.go`. Turn state is tracked by whether the stream channel is live — a turn is "in progress" from send until the chunk channel closes, and messages typed mid-turn are queued invisibly (not rendered as pending) until sent.

### internal/webui — the browser frontend

Serves `--web` mode: a JSON/SSE API over the same `ui.Backend` the TUI uses, plus static assets go:embed'ed from `web/` (JS/CSS changes need a `go build`; the server sends `Cache-Control: no-store`, so no `?v` cache-busting). Go handlers are split by resource — `stream.go`, `sessions.go`, `config.go` — routed with Go 1.22+ ServeMux method patterns; API routes sit on their own mux so a wrong-method request gets a 405 instead of falling through to the file server.

`web/js/` is native ES modules with **no build step** — keep it that way. `main.js` is the entry point; its `Object.assign(window, …)` block is the complete contract with `index.html`'s Alpine templates (module top-level declarations are not globals, so anything a template calls must be listed there). `api.js` holds every `/api/*` fetch except the two streaming endpoints, which live in `stream.js` with the chunk handling. `anim.js` and `toolformat.js` are lockstep ports of `internal/ui` files — change them only together with their Go counterparts. The store↔theme/anim/commands import cycles are deliberate and safe (function declarations only, all calls at runtime); don't add top-level cross-module *calls*. A future page = its own `<page>.html` + entry module importing the shared modules, not additions to the chat page's files.

### Configuration — everything is config-discovered

All persistent state lives under `~/.tui-testing/` (via `internal/appdir`, the only package that decides paths): root `agent.json`, sub-agent configs, `settings.toml` (normalized to the full schema on load — see `internal/settings`), themes (JSON, with built-in defaults embedded from `internal/theme/defaults/`), MCP server configs, credentials. Agents, tools, providers, models, and themes are all editable at runtime through slash commands (`/agents`, `/key`, `/sessions`, `/new`) that write these files and rebuild the backend — new features should follow the same config-discovery pattern rather than hardcoding.

## Decision history — read before proposing features

`docs/agent-memory/` is a snapshot of a prior Claude agent's full project memory (see its README). Consult it before starting anything nontrivial; the headlines, so you don't re-propose settled or reverted things:

- **Long-term memory was built and reverted.** ADK's `memory.Service` + `preloadmemorytool` is keyword search over raw transcripts, not a fact store — the user rejected the whole approach, not just the tuning. A real attempt needs an extraction/judgment layer designed *with* the user first. Don't reach for the ADK primitive as shipped.
- **Sub-agent work is frozen (2026-07-17).** Agent-as-tool stays as-is: don't expand it (no sub-agent HITL fix, no write-capable sub-agents, no transfer mode) until the user explicitly re-opens it with a dedicated design brainstorm. The context gap (disposable session, only final text returned) is structural, confirmed in ADK source.
- **Session management scope is closed** — /new, /sessions with replay and delete are all built. Interrupt-and-redirect exists as `/interrupt`.
- **Execution targets:** local + SSH (incl. remote background processes) are done. MCP-server-as-target, remote-agent, and container targets were brainstormed but deliberately deferred; the option space is captured in the memory file.
- **Theme roadmap:** near-term work stays inside the current 14-token palette architecture. Widget-attribute overrides, custom boot art, and movable layout zones are scoped *future* milestones — deferred, not rejected.

## Conventions

- Minimal, idiomatic Go over feature breadth. Small diffs that fit the existing shape beat clever abstractions.
- This codebase uses long doc comments that explain *why* (constraints, ADK gotchas, ordering requirements) — match that density when touching code, and read the doc comment of anything you're about to change; the sharp edges are documented in place.
- Dependencies are chosen to keep the binary pure-Go and self-contained (no CGO).
