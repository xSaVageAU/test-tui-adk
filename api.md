# Building a consumer: the REST API reference

This is the reference for writing your own consumer of a running `botson
core` — a website, a TUI (like this repo's own `frontends/chat`), a Discord
bot, anything. The core's only interface is plain HTTP; there is no NATS, no
gRPC, no Go package to import. If your language can make an HTTP request with
a bearer header, you can build a consumer.

See [AGENTS.md](../AGENTS.md) for how the core process itself is built; this
document is scoped to "what do I send, and what do I get back."

---

## 1. Connecting and authenticating

The core binds one HTTP server to `host:port` (default `127.0.0.1:4222`,
configurable — see `~/.botson/config.json`'s `host`/`port`). Every request,
to every route, needs:

```
Authorization: Bearer <api_auth_token>
```

`api_auth_token` is generated once into `~/.botson/config.json` the first
time the config is loaded, and never exposed back through
`GET /botson/settings` or anywhere else — it's the credential that gates the
API that would otherwise hand it back. A consumer on the same machine can
read it directly out of that file; a remote consumer needs it copied over
out of band. A request with a missing or wrong token gets `401` with
`{"error": "unauthorized"}`.

There is no TLS and the auth model is "one shared token, checked in constant
time" — fine for a single-user local tool, not a substitute for a real
multi-tenant auth system if you ever expose this beyond your own machine.

---

## 2. Two route groups

| Prefix | What it is | Implemented by |
|---|---|---|
| `/api/*`, `/a2a/*`, `/.well-known/agent-card.json` | The standard ADK REST/A2A surface: list agents, create/get/list/delete sessions, run a turn, artifacts, A2A JSON-RPC. Reverse-proxied byte-for-byte into a real `google.golang.org/adk/v2` REST server running internally — not reimplemented by hand, so behavior always matches upstream ADK exactly. | `internal/networking/api` (`adkbackend.go`/`adkreverseproxy.go`) |
| `/botson/*` | Everything Botson-specific that isn't part of ADK's own API: settings, custom-agent CRUD, dashboard-shaped session listing/aggregation. | `internal/networking/api` (`routes.go`), backed by `internal/management` |

Use `/api/*` for anything about actually running the agent (sessions,
turns, artifacts, A2A). Use `/botson/*` for managing the Botson instance
itself (its config, its custom agents, browsing session history for a
dashboard-style view).

---

## 3. `/api/*` — the official ADK surface

This section documents Google ADK's own REST API as Botson exposes it — not
Botson-specific behavior. `{appName}` is an agent name (from `list-apps`).
`{userId}` is **entirely yours to choose** — the core makes no assumptions
about user identity; see §6. All bodies are JSON.

### List available agents

```
GET /api/list-apps
```
→ `["Agent Botson", ...]`

### Create a session

```
POST /api/apps/{appName}/users/{userId}/sessions
POST /api/apps/{appName}/users/{userId}/sessions/{sessionId}
```
Body (optional — omit entirely for an empty session):
```json
{ "state": { }, "events": [] }
```
The first form lets the server generate a UUID; the second lets you pick
your own `sessionId`. Either way, the reply is a `Session`:
```json
{
  "id": "...",
  "appName": "Agent Botson",
  "userId": "...",
  "lastUpdateTime": 1752000000,
  "events": [],
  "state": {}
}
```

### Get / list / delete sessions

```
GET    /api/apps/{appName}/users/{userId}/sessions/{sessionId}   -> Session (full state + event history)
GET    /api/apps/{appName}/users/{userId}/sessions               -> [Session, ...] (summaries for that app+user)
DELETE /api/apps/{appName}/users/{userId}/sessions/{sessionId}
```

### Run a turn

```
POST /api/run
```
```json
{
  "appName": "Agent Botson",
  "userId": "your-consumer:some-id",
  "sessionId": "<from create-session>",
  "newMessage": { "role": "user", "parts": [{ "text": "hello" }] },
  "stateDelta": { }
}
```
- `newMessage` is a `genai.Content` — Google's Gemini content type (role +
  parts; parts can be text, function calls, or function responses).
- `stateDelta` (optional) merges directly into the session's durable state
  as part of this turn — see §7 for the specific keys Botson's own tools
  read.

**Response** (status 200): a JSON array of `Event` objects — the full
turn's events at once:
```json
[
  { "id": "...", "invocationId": "...", "author": "user", "content": {...}, "actions": {...} },
  { "id": "...", "invocationId": "...", "author": "Agent Botson", "content": {...}, "actions": {...} }
]
```
The full `Event` shape has a lot of optional fields (grounding metadata,
usage metadata, transcriptions, tool-confirmation state, ...) — the two you
need for a basic chat UI are `author` and `content.parts` (see §5 for how
to detect a pending confirmation instead of a plain reply).

`/api/run_sse` and `/api/run_live` (streaming — SSE and WebSocket
respectively) exist on the real ADK backend and are reverse-proxied the
same as everything else. **Not yet exercised by any consumer in this
repo** — `frontends/chat` only uses `/api/run` and shows a spinner while
waiting for the full turn. Go's `httputil.ReverseProxy` (what forwards
these requests) does flush `text/event-stream` responses immediately
rather than buffering, so `/api/run_sse` should work through the proxy in
principle; treat it as unverified until something in this repo actually
uses it.

### Artifacts

```
GET    /api/apps/{appName}/users/{userId}/sessions/{sessionId}/artifacts                              -> [artifact names]
GET    /api/apps/{appName}/users/{userId}/sessions/{sessionId}/artifacts/{artifactName}                -> latest version
GET    /api/apps/{appName}/users/{userId}/sessions/{sessionId}/artifacts/{artifactName}/versions/{version}
DELETE /api/apps/{appName}/users/{userId}/sessions/{sessionId}/artifacts/{artifactName}
```
**There is no REST endpoint to upload/create an artifact.** Artifacts are
only ever created server-side, by the agent's own `saveArtifact` tool call
mid-conversation (backed by `internal/storage/artifact`). A client can
list, download, and delete them after the fact, but can't push one in
directly over this API — see §8 for whether that's worth changing.

### A2A

```
GET  /.well-known/agent-card.json
POST /a2a/v1/invoke   (A2A 1.0 JSON-RPC)
POST /a2a/invoke       (A2A 0.3 compat JSON-RPC)
```
Proxied verbatim; this repo has no A2A-specific code of its own and no
consumer here has exercised this path. Treat upstream ADK/A2A docs as the
source of truth for the JSON-RPC contract itself.

### Eval and debug routes

`/api/apps/{appName}/eval_sets`, `/api/apps/{appName}/eval_results`, and
`/api/debug/trace/*` also exist on the proxied backend. They're part of
ADK's own surface, reverse-proxied along with everything else, but nothing
in this repo calls them and they aren't covered further here — see §8.

### Example (curl)

```bash
TOKEN=$(jq -r .api_auth_token ~/.botson/config.json)
BASE=http://127.0.0.1:4222

# list agents
curl -s -H "Authorization: Bearer $TOKEN" $BASE/api/list-apps

# create a session
curl -s -X POST -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/apps/Agent%20Botson/users/myapp:123/sessions/abc-1"

# run a turn
curl -s -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"appName":"Agent Botson","userId":"myapp:123","sessionId":"abc-1",
       "newMessage":{"role":"user","parts":[{"text":"hi"}]}}' \
  "$BASE/api/run"
```

---

## 4. `/botson/*` — managing the Botson instance

Defined in [`internal/networking/api/routes.go`](../internal/networking/api/routes.go).
Every reply on failure is `{"error": "..."}` at a matching HTTP status
(400 for a bad request, 404 for not-found, 500 otherwise) — unlike the old
NATS subjects this replaced, which had no status-code channel of their own.

### Settings

| Route | Method | Body | Reply |
|---|---|---|---|
| `/botson/settings` | GET | — | `{"model_name","providerKeys":{"gemini","openrouter"},"root_agent","workspace_root","provider",...}` — `providerKeys.gemini`/`providerKeys.openrouter` are always masked (`"******"`); `api_auth_token` is never included |
| `/botson/settings` | PATCH | `{"modelName"?,"rootAgent"?,"providerKeys"?:{"gemini"?,"openrouter"?},"workspaceRoot"?,"provider"?}` — omit a field (or the whole `providerKeys` object) to leave it unchanged | same shape as GET, plus `"note"` (present only if `modelName`/`provider` changed) warning that this running core process is still using the model/provider it booted with. A `workspaceRoot` change applies immediately; `modelName`/`provider`/the API keys take effect on the next core restart |

### Agents

| Route | Method | Body | Reply |
|---|---|---|---|
| `/botson/agents` | GET | — | `[{"name","description","is_root","private","tools":[...],"instructions","read_only"}, ...]` |
| `/botson/agents/tools` | GET | — | `{"standard":["listFiles","readFile",...], "agents":["Agent Botson",...]}` — valid values for an agent's `tools` list (built-in tools, plus any other agent name for sub-agent delegation) |
| `/botson/agents` | POST | `{"name","description","tools":[...],"private","instructions"}` | `{}` on success. Creates or overwrites a custom agent under `~/.botson/agents/<name>/`. If `name` collides with a bundled default agent, saves as a user override |
| `/botson/agents/{name}` | DELETE | — | `{}` on success. Only affects custom user agents — bundled defaults can't be deleted |

### Sessions (dashboard-shaped view)

Distinct from `/api/apps/.../sessions/...`'s raw session objects — these
are shaped for display (extracted display name, human-readable event
summaries) rather than for driving a conversation. A session's real
identity is always the composite key `(agent, user, sessionId)`.

| Route | Method | Body | Reply |
|---|---|---|---|
| `/botson/sessions?agent=&user=` | GET | — (both query params optional filters) | `[{"id","agentName","userId","displayName","lastUpdateTime","eventCount"}, ...]`, most-recently-updated first |
| `/botson/sessions/{agent}/{user}/{sessionId}` | GET | — | `{...same fields as above, "state":{...}, "events":[{"author","timestamp","text"}, ...]}` |
| `/botson/sessions/{agent}/{user}/{sessionId}` | DELETE | — | `{}` on success |
| `/botson/sessions/{agent}/{user}/{sessionId}/autoMode` | PATCH | `{"enabled": true\|false}` | `{}` on success — see §7 for what auto mode does |

### Dashboard aggregation

| Route | Method | Reply |
|---|---|---|
| `/botson/dashboard/stats` | GET | `{"totalAgents","totalSessions","totalEvents","dbPath","agents":[{"name","description","isRoot","sessionCount"}, ...],"recentSessions":[...up to 10 session summaries...]}` |
| `/botson/dashboard/users` | GET | `["user-id-1","user-id-2",...]` — every distinct user ID actually present in session data, sorted. Empty if no sessions exist yet. **No default/seed value** — see §6 |

---

## 5. Human-in-the-loop (HITL) confirmations

If a tool call requires approval (`RequireConfirmation: true` in Botson's
tool registry — currently `saveArtifact`, `updateSettings`, `writeFile`,
`editFile`, `runCommand`), a run's events do **not** simply pause and
resume the original tool call. The actual sequence for one gated call:

1. The model's real `functionCall` (e.g. `writeFile`, some call id `X`).
2. An **immediate** `functionResponse` for that same call id `X`, before
   any human has done anything: `{"response": {"error": "error tool
   \"writeFile\" requires confirmation, please approve or reject"}}`. This
   is ADK's own internal bookkeeping for "this call is now blocked pending
   confirmation" — it is not a real result, even though it has exactly the
   shape of one.
3. A synthetic wrapper `functionCall` named `adk_request_confirmation` (a
   **new**, different call id), whose `args.originalFunctionCall` embeds
   the real call (name, id `X`, args) and whose
   `args.toolConfirmation.hint` is the prompt to show the user. This is
   what a consumer should actually render as "pending approval".
4. The human's decision arrives as a `functionResponse` on the
   `adk_request_confirmation` call id: `{"response": {"confirmed":
   true|false}}`, sent as `newMessage.parts` on a following `/api/run`
   call for the same session.
5. Only *then*, if approved, does the real tool handler run — producing a
   **second** `functionResponse` reusing the *original* call id `X`, this
   time with the tool's actual result.

**The trap**: call id `X` gets two different `functionResponse`s over its
lifetime (the fake "requires confirmation" placeholder, then the real
result). Naively keying a "call id → its result" lookup off the last-seen
`functionResponse` works once everything's settled, but shows the tool
call as falsely "Completed" with the placeholder error in the window
between steps 2 and 5 — exactly while the real `adk_request_confirmation`
card should be showing "pending". Track which call ids appear as some
`adk_request_confirmation`'s `args.originalFunctionCall.id`, and never
render those ids' raw `functionCall`/`functionResponse` as their own trace
at all — their whole story (pending → approved/denied → result) belongs
to the confirmation card alone. `frontends/chat/render.go`'s
`renderEvents` does exactly this and is a working reference
implementation.

**Ordering-only (deferred) confirmations.** ADK dispatches a turn's
parallel `functionCall`s concurrently, so a *non*-gated call (e.g.
`readFile`) the model emitted *after* a gated one could really execute
before the gated call's approved effect exists. `internal/engine/toolorder`
closes that hole by pausing such a call through the exact same steps 2-5,
with one difference: the wrapper's `args.toolConfirmation.payload` is
`{"botsonToolOrderDeferred": true}`. That marker means the confirmation
exists purely so the call rides the resume pass in its emitted position —
there is no human decision in it. **Every consumer must check for it**:
answer marked confirmations `{"confirmed": true}` immediately and
silently, batched into the same `/api/run` call as the user's answers to
that turn's real confirmations. Prompting a human for one is noise; never
answering it stalls the run until the request times out. Relatedly, a
consumer must send *all* of a turn's confirmation answers in **one**
`/api/run` call: the resume executes strictly in emitted order, so
answering only a later call's confirmation while an earlier one is still
unanswered leaves the resumed call waiting on a sibling that cannot resume
yet.

**Auto mode.** A session can carry its own `botson:autoMode` flag in
durable state, toggled via `PATCH /botson/sessions/{agent}/{user}/{sessionId}/autoMode`
(never via `stateDelta`, since it needs to change mid-conversation, not
just on a fresh session's first turn). When set, every confirmation on
that session is meant to be answered `{"confirmed": true}` without a human
decision, marked with an extra key alongside `confirmed` in the response —
`{"confirmed": true, "botsonAutoMode": true}` — so history (and any other
consumer) can tell it apart from both a real human `y` and a `toolorder`
ordering-only deferral. Two independent things answer these, on purpose: a
connected chat client can answer immediately itself, and
`internal/automode`'s background worker polls every auto-mode session for
anything still unanswered and answers it the same way — a safety net that
guarantees the turn keeps advancing even after every consumer disconnects.
`internal/automode` also caps consecutive auto-approvals per session (see
its `maxConsecutiveApprovals`) and turns the flag back off, with the
reason recorded in-conversation, if a run looks like it's looping without
ever seeing a fresh enable — unattended does not mean unbounded.

---

## 6. There is no default "user"

The core makes no assumptions about what a user ID looks like or what
value it should default to. Every route that's scoped to a session
requires the caller to supply `userId` explicitly — there's no hardcoded
fallback.

This means **you choose your own user-ID scheme.** A few reasonable
patterns:
- One fixed ID per consumer app (`"my-web-console"`) if you don't need
  per-person session isolation.
- One ID per real end-user (`"discord:123456789"`, `"chat-alice"`) if you
  do.
- Something else entirely — the core just stores and filters on whatever
  string you send.

Whatever you pick, **avoid characters that aren't safe in a filesystem
path** (`/ \ < > : " | ? *`, control characters, or a bare `.`/`..`) —
`appName`/`userId`/`sessionId` end up as path segments under
`~/.botson/artifacts/` (`internal/storage/artifact`), which rejects them
outright rather than silently mangling a path. `frontends/chat` uses
`"chat-" + <local username>`, not `"chat:" + ...`, for exactly this
reason. Session lookups otherwise require the *exact*
`(appName, userId, sessionId)` triple a session was created under.

---

## 7. Session state conventions

A session's `state` map (visible via `GET /api/.../sessions/{sessionId}`
or `GET /botson/sessions/{agent}/{user}/{sessionId}`) carries a few
flat, Botson-specific keys — flat rather than nested, since session state
is JSON round-tripped on reload and a nested map value comes back as
`map[string]interface{}` on a later turn where a scalar round-trips
losslessly either way:

| Key | Type | Meaning |
|---|---|---|
| `botson:cwd` | string | Overrides the workspace directory the file/command tools (`listFiles`, `readFile`, `writeFile`, `editFile`, `runCommand`) operate in for this session, instead of the core's configured `workspace_root`. Set via `stateDelta` on a `POST /api/run` call, typically on the session's first turn — **not sandboxed**, can point anywhere the core process can read/write. |
| `botson:autoMode` | bool | See §5's "Auto mode". Toggled via the dedicated `/botson/sessions/.../autoMode` route, not `stateDelta`. |
| `botson:tools:read:<absolute path>` | bool | Read-before-write tracking (`internal/engine/tools/read_tracking.go`) — internal bookkeeping, not meant to be set by a consumer. |
| `__session_metadata__.displayName` | string | If present, used in place of the raw session ID in list views (`/botson/sessions`, dashboard). `frontends/chat` and any similar consumer typically sets this via `stateDelta` on the first turn, from the user's first message. |

---

## 8. Known gaps

- **No streaming consumer yet.** `/api/run_sse`/`/api/run_live` are
  proxied through but nothing in this repo drives them — `frontends/chat`
  waits for the full `/api/run` reply. Building a streaming consumer is
  unexplored territory, not a known-broken one.
- **No artifact upload endpoint.** Artifacts can only be created by the
  agent's own `saveArtifact` tool call, never pushed in directly by a
  client over REST. If a future consumer needs to hand the agent a file
  (rather than asking it to write one), that's a gap to close, likely by
  proxying ADK's own artifact-upload behavior if it has one, or adding a
  Botson-specific route.
- **Eval and debug routes are unexplored.** `/api/apps/{appName}/eval_sets`,
  `/eval_results`, and `/api/debug/trace/*` are proxied like everything
  else, but no consumer here has ever called them and their request/reply
  shapes aren't documented in this file — treat upstream ADK docs as the
  source of truth if you need them.
- **A2A is proxied but unused.** Same situation as eval/debug: the routes
  exist and forward correctly, but this repo has no A2A-specific code and
  no worked example.
