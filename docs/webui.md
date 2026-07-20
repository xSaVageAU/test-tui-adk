# The WebUI: what `feature/webui-refactor-alpine` built

This branch added a browser frontend (`internal/webui/`) to what was previously a TUI-only
product. It did not exist on `master` at all — everything under `internal/webui/` was built
from scratch here. This document covers what was built, in what order, and — because a few
things couldn't be done without touching code outside `internal/webui/` — what changed
elsewhere in the codebase and why.

If you're reading this after the branch has merged: `CLAUDE.md` has the durable, kept-up-to-date
description of how the webui works today. This document is the history — the *why* behind the
shape, in the order it happened — which `CLAUDE.md` doesn't carry.

## Why a webui at all

The TUI's `ui.Backend` interface (`internal/ui/backend.go`) already fully decouples the terminal
frontend from the agent backend (`internal/adk`) — the one load-bearing rule in this codebase is
that `internal/ui` never imports `internal/adk`. That decoupling is what made a second frontend
cheap: the webui talks to the exact same `ui.Backend` and `ui.AppConfig` the TUI does, just over
HTTP/SSE instead of Bubble Tea messages. `internal/adk` never had to change to accommodate a
second consumer; `internal/ui` only changed where it needed to expose something the webui alone
needed (see below).

## Commit-by-commit summary

1. **`db1ea87` — Alpine.js reactive WebUI base.** The initial webui: a Go HTTP server
   (`internal/webui/server.go`) wrapping `ui.Backend` in a JSON/SSE API, and a single-page
   Alpine.js frontend (`web/index.html` + `web/app.js` + `web/style.css`) for chat, streaming
   replies over Server-Sent Events, HITL tool confirmation, session switch/new/delete, theme
   selection, settings, and agent/tool configuration — the same feature set the TUI's slash
   commands expose, reached through the same config-discovery files on disk.
2. **`60f6959` / `19806ea` — loader animation engine.** Ported the TUI's "working" animations
   (`internal/ui/workinganim_variants.go`) into JS so the browser shows the same spinner variants
   (Equalizer, Orbit, Matrix Rain, etc.) at the same frame rate (`tea.Tick(60ms)`, reproduced with
   a `requestAnimationFrame` + fixed accumulator loop).
3. **`1dd693c` / `e6556a4` — TUI parity fixes for the loaders.** Fixed a JS variable-redeclaration
   bug and a font-rendering mismatch: the Google Fonts JetBrains Mono subset is latin-only, so
   block-drawing/braille/katakana glyphs were silently falling back to mismatched-width system
   fonts and making the animations jitter. Fixed by locking animation cells to a `1ch` grid and
   widening the font fallback stack.
4. **`f990b37` — tool-output formatting parity.** Ported `internal/ui/toolformat.go`'s lean/verbose
   tool-call summaries (e.g. "read 4.2 KB" vs. the actual file content) into JS, so tool call rows
   in the webui transcript read the same way they do in the TUI.
5. **`eb44914` — transient top-bar status notices.** This is the one change that reached back into
   the TUI, not just the webui — see [Changes outside `internal/webui`](#changes-outside-internalwebui)
   below. Confirmations and progress notes ("Theme set to X.", "Started a new session.") moved out
   of the permanent transcript and into a 4-second top-bar badge, in **both** UIs, so the two stay
   behaviorally identical rather than the webui inventing its own notification model.
6. **`84c3783` — lazy-loading file-tree sidebar.** A VS Code-style collapsible file tree, backed by
   a new endpoint that lists one directory at a time rather than walking a whole tree — see below,
   this also required a small new capability in `internal/adk/tools` and `internal/ui`.
7. **`371dc01` — maintainability refactor.** No new features. Split the by-then ~500-line
   `server.go` and ~1800-line `app.js` into resource-focused Go files and dependency-free ES
   modules, in preparation for the webui growing beyond a single chat page. Covered in detail
   below since it's the most recent and most structurally significant change.

## Features implemented

- **Full chat parity with the TUI**: streamed replies (SSE), reasoning/thinking display,
  markdown rendering, tool-call rows with running/done/approved/denied states, HITL tool
  confirmation (approve/deny), session create/switch/delete with transcript replay, a slash
  command palette (`/new`, `/sessions`, `/theme`, `/settings`, `/key`, `/agents`, `/loader`,
  `/interrupt`, `/reload-agents`, `/exit`), and prompt history navigation (PgUp/PgDn).
- **Theme system**: reads the same theme JSON files the TUI uses; the 14-token palette maps onto
  CSS custom properties, so all page styling keys off `var(--…)`.
- **Settings**: TUI settings (verbose tool output, reasoning visibility, streaming, user-message
  highlighting) and agent settings (tool approval mode, execution target) are read/written through
  the same `settings.toml`-backed API the TUI uses, so a change in one UI is visible in the other.
- **Agent/tool configuration**: the `/agents` modal lists agents, lets you set provider/model per
  agent, and toggle which tools an agent has — the same `SetAgentProvider`/`SetAgentModel`/
  `SetAgentTools` config-discovery functions the TUI's `/agents` menu calls.
- **10 working-animation variants**, pixel-for-pixel ported from the TUI, theme-aware and
  frame-rate-matched.
- **A file-tree sidebar** (☰ toggle) showing the active execution target's working tree — local
  host or SSH remote, whichever the agent's tools are currently pointed at — lazily fetched one
  directory at a time, polled every 3s while open plus refreshed immediately whenever a tool call
  finishes (a tool may have just changed the filesystem).
- **Transient top-bar status notices**, shared with the TUI: short-lived center-of-header badges
  for confirmations/progress, distinct from permanent transcript system messages (which are
  reserved for things a user shouldn't lose — errors, refusals).

No new external dependencies were introduced. The Go side uses only the standard library
(`net/http`, `embed`) plus `github.com/google/uuid`, which was already a project dependency. The
frontend uses Alpine.js (from a CDN, as before) and otherwise ships zero npm packages and no build
step.

## Changes outside `internal/webui`

Most of the branch's work is self-contained inside `internal/webui/`. Three things couldn't be:

### `internal/ui` — new capability the webui needed from `AppConfig`, plus a TUI-visible behavior change

- **`notice.go` (new file) + `header.go` + `app.go`**: the transient top-bar notice system
  (`setNotice`, `noticeExpireMsg`, `joinLeftCenterRight`) described above. This is a **TUI**
  change made *for* webui parity — the notice/systemMessage split (expiring badge vs. permanent
  transcript message) now exists identically in both UIs. `commands.go`, `agentsmenu.go`,
  `keyrouting.go`, `menustack.go`, `sessions.go`, and `turn.go` each had a handful of
  `a.systemMessage(...)` calls changed to `a.setNotice(...)` at the specific call sites that are
  confirmations/progress rather than failures — the split is manual/curated, not a bulk rename,
  since e.g. "Some sessions failed to delete" deliberately stayed a permanent transcript message
  while "Started a new session." became a transient notice. `menustack_test.go` was updated for
  the same reason (asserting against `a.notice` instead of the transcript).
- **`app.go` — `AppConfig.ListTargetDir` + `TargetDirListing`/`TargetDirEntry`**: a new field and
  two new exported types on `ui.AppConfig`, following the same pattern as the existing
  `ConfigureTarget` field (a plain closure the `main.go` bridge wires up, nil-checked so it's
  optional). This is the contract the file-tree sidebar's `/api/files` endpoint is built on. The
  TUI itself doesn't consume it — it exists on the `ui` side only because that's where
  `Backend`/`AppConfig` contract types are conventionally defined (mirroring how `StreamChunk`
  etc. are defined in `ui` even though only `adk` produces them).

### `internal/adk` — a thin re-export, and `internal/adk/tools` — the actual directory listing

- **`agents.go` — `ListTargetDir`**: a one-function re-export mapping `tools.ListDir`'s result
  into `ui.TargetDirListing`/`ui.TargetDirEntry`, the same "implementer depends on consumer" seam
  `ConfigureExecutionTarget` already used.
- **`tools/target.go` + `target_local.go` + `target_ssh.go` — `Getwd()`**: one new method added to
  the `Target` interface (local: `os.Getwd()`; SSH: the SFTP session's `Getwd()`, i.e. the login
  home). This is what the file-tree sidebar roots itself at — and why switching the execution
  target (local ↔ SSH) makes the sidebar immediately re-root at the new cwd.
- **`tools/filetree.go` (new file) — `ListDir`**: lists **one** directory per call rather than
  walking a whole tree. An earlier design walked recursively with a global entry budget; that was
  rejected because a budget both truncates large trees arbitrarily and spends SFTP round-trips on
  directories nobody had actually opened. The settled design is VS Code-style lazy loading — one
  `ReadDir` per expanded directory, complete at any depth, `O(visible)` instead of `O(tree)` — with
  only a per-directory cap (`dirListMaxEntries = 2000`) as a sanity bound, not a tree budget.

### `main.go` — the `--web`/`--port` flags

Added `flag.BoolVar(&runWeb, "web", ...)` and `flag.IntVar(&port, "port", 8080, ...)`. The same
`ui.AppConfig` that used to go straight into `ui.NewApp(...)` is now built once and handed to
either `webui.StartServer(ctx, appConfig, port)` (if `--web`) or `ui.NewApp(appConfig)` — the two
frontends are switched on at the entry point, nothing upstream of `main.go` knows which one is
running.

## The maintainability refactor (`371dc01`)

By the time the file-tree sidebar landed, `internal/webui/server.go` was ~536 lines (every HTTP
handler inline) and `web/app.js` was ~1789 lines (every frontend subsystem — Alpine store, working
animations, file tree, SSE streaming, HITL confirm, slash commands, modal logic, the toolformat.go
port, markdown rendering — in one file). Neither was broken, but the user's stated intent is to
expand the webui into a multi-page product where chat is only one feature among several, so this
was refactored for maintainability with **no functional changes**.

### Server: 4 files instead of 1

`server.go` now only holds the `Server` struct, route table, static file serving, and a
`writeJSON` helper. Handlers moved into `stream.go` (the streaming trio: `/api/stream`,
`/api/confirm`, `/api/interrupt`, sharing a new `beginTurn` helper that replaces what used to be
duplicated cancel-and-swap boilerplate), `sessions.go` (session list/create/delete, transcript),
and `config.go` (status, key, agents, themes, settings, files). Routing switched to Go 1.22+
`ServeMux` method/wildcard patterns (`"DELETE /api/sessions/{id}"`, `r.PathValue("id")`), which
deleted every manual `if r.Method != ...` check and `strings.TrimPrefix` call. API routes were put
on their own sub-mux mounted at `/api/` so a wrong-method request to a real route now gets a
proper `405` instead of falling through to the static file server and 404ing.

### Frontend: 10 ES modules instead of 1 file, still zero build step

`web/app.js` became `web/js/{main,store,api,stream,commands,filetree,anim,toolformat,theme,format}.js`
— native ES modules, loaded via `<script type="module" src="js/main.js">`, no bundler/npm/TypeScript
introduced. The split follows the subsystem boundaries that were already marked out as banner
comments in the old file. Two things had to be solved to make the split behave identically to the
monolith:

- **The window-global contract.** Alpine's inline expressions in `index.html` (`@click="..."`,
  `x-text="..."`) can only call `window` properties, and — unlike top-level functions in a
  classic script — nothing inside an ES module is implicitly global. `main.js` now ends with one
  explicit `Object.assign(window, { ... })` block listing every function a template calls
  (`modalClose`, `doConfirm`, `ftToggle`, `summarizeResult`, etc.). This turns what used to be an
  implicit, easy-to-break contract into a single visible list — and was verified by grepping
  `index.html` for every `@click`/`@keydown`/`x-text`/`x-html` call and confirming each name
  appears in that block.
- **Load order.** `main.js`'s module tag has to run before Alpine's CDN `<script defer>` tag,
  because `store.js` registers the Alpine store via an `alpine:init` listener that must exist
  before Alpine boots. Module scripts and deferred scripts both execute after parsing, in document
  order, so keeping `main.js`'s tag first in `<head>` preserves that.

`api.js` collects every `/api/*` fetch call in one place (except the two SSE/streaming endpoints,
which live in `stream.js` because their transport *is* their logic) — this is meant to be the
shared contract any future page imports. `anim.js` and `toolformat.js` are kept as 1:1 ports of
`internal/ui/workinganim_variants.go` and `internal/ui/toolformat.go` respectively, matching the
existing "keep in lockstep, cross-reference in comments" convention this codebase already uses for
TUI↔webui parity code.

Also dropped in this pass: the `?v=N` cache-busting query params on `style.css`/`app.js` in
`index.html`. They were redundant — the server already sends `Cache-Control: no-store` on every
static asset — and bumping them by hand on every change was pure ceremony.

One incidental bug fix surfaced during this refactor: Go's `mime` package can resolve `.js`'s MIME
type from the Windows registry, which on some machines maps it to `text/plain` — silently breaking
ES module loading (browsers require a JavaScript MIME type for `<script type="module">`).
`server.go` now calls `mime.AddExtensionType(".js", "text/javascript; charset=utf-8")` (and the
same for `.css`) before serving, so the embedded assets' types are pinned regardless of host
registry state.

### Verification

`go build ./...` and `go vet ./...` catch the Go side, per this repo's normal bar. The JS side
isn't covered by either, so it was checked by: syntax-checking every module with
`node --input-type=module --check`; evaluating the *whole* import graph under Node with browser
globals (`document`, `window`, `Alpine`, `localStorage`) stubbed out, to catch bad imports and
temporal-dead-zone issues in the deliberate `store.js` ↔ `theme.js`/`anim.js`/`commands.js` import
cycles (safe because those cycles only exchange function declarations, never top-level calls); and
live `curl` probes of the running server confirming route dispatch, embedded-asset MIME types, and
method-based 404/405/409 behavior all matched the pre-refactor server.

## Current file map

```
internal/webui/
  server.go     — Server struct, route table (stdlib net/http, Go 1.22+ patterns), static serving
  stream.go     — /api/stream, /api/confirm, /api/interrupt, writeSSEStream, beginTurn helper
  sessions.go   — /api/sessions (list/create/delete), /api/transcript/{id}
  config.go     — /api/status, /api/key, /api/agents, /api/themes, /api/settings, /api/files
  web/
    index.html
    style.css
    js/
      main.js       — entry point; wiring; the window-export contract with index.html
      store.js      — Alpine store: all reactive state + message/palette/modal helpers
      api.js        — every /api/* fetch (except the two streaming endpoints)
      stream.js     — SSE send/receive, chunk handling, HITL confirm/resume, interrupt
      commands.js   — slash-command registry + implementations + modal confirm logic
      filetree.js   — lazy-loading file-tree sidebar state and polling
      anim.js       — the 10 working-animation variants + spin-timer lifecycle
      theme.js      — theme load/apply, CSS custom property mapping
      toolformat.js — tool-call arg/result summarization (port of toolformat.go)
      format.js     — boot art, markdown rendering, small formatters, scroll helper
```

## Related capabilities elsewhere (not changed on this branch, referenced above)

- `ui.Backend`/`ui.AppConfig` (`internal/ui/backend.go`) — the interface boundary that made a
  second frontend possible without touching `internal/adk`.
- `internal/adk/tools/target.go`'s `Target` interface — the local/SSH execution-target
  abstraction the file-tree sidebar (and every other tool) runs against.
- `internal/settings`, `internal/theme` — the config-discovery packages both UIs read/write
  through; nothing about their schemas changed for the webui.
