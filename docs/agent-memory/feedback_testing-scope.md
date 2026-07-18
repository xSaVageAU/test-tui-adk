---
name: feedback-testing-scope
description: User wants verification effort scaled to actual risk, not applied as a reflexive ritual after every change — default to build+vet, escalate to a real test only when there's a specific reason to distrust the assumption.
metadata:
  type: feedback
  originSessionId: 90538001-00e7-4e08-bcb5-9837e57e0995
---

On 2026-07-14 the user asked directly why features were taking noticeably longer to implement than with other agents they'd used. Broke down what I'd actually been doing on every change (read files first, write a throwaway Go test exercising real code paths, delete it, build a real binary and launch it, check actual on-disk state, write a memory update) and split it into "calibration to genuinely surprising ADK/library behavior" versus "a habit I'd settled into applying uniformly, including to low-risk changes like a rename or config restructure."

**Rule:** don't write a throwaway verification test (or launch a real binary smoke-test) for every change by default. Reserve that for cases with a specific reason to distrust the assumption:
- Concurrency/goroutine-ordering code (this project has been burned by ADK's un-ordered parallel tool-call dispatch before — see [[tool-call-concurrency]]).
- A third-party library's runtime behavior that isn't obviously documented and matters for correctness (e.g. whether `gemini.NewModel` validates a key eagerly, whether a Go interface embedding promotes a given method).
- A bug class that's already bitten this specific project once.

For everything else — a rename, straightforward config restructuring, simple wiring changes, a new field threaded through a few call sites — `gofmt`/`go build`/`go vet` passing cleanly is enough. Don't add a temp test as a reflexive ritual on top.

**Why:** the user has direct experience with other coding agents where "self-testing," especially of TUI/interactive applications, spirals — burning far more turns/tokens on testing than the actual implementation took, sometimes going in circles trying to verify something that would take the user seconds to eyeball themselves. They explicitly said I'm "pretty ok" at this compared to other agents (verification attempts have generally worked, not spiraled) — this isn't a complaint that testing was broken, it's that the cost isn't worth paying when the risk is low. They said they've always preferred manually verifying things themselves.

**Specific pattern to drop:** never attempt to self-drive or interact with the actual running TUI to verify behavior (simulated keypresses, captured rendered frames via tmux/expect-style automation) — that's precisely the category the user wants to verify themselves, not delegate. A non-interactive check (calling a render function directly and printing its output, or a real `go test` unit test) is a different, much cheaper thing and still fine when actually warranted by the criteria above — the object being avoided is the *reflexive, uniform* ritual, not testing as a concept.

**Also worth noting:** the background-process smoke-test pattern (`run_in_background: true` on the built binary) hung on a TTY-detection issue multiple times this session, each requiring a `TaskStop` cleanup — real, if minor, wasted overhead that this new default also avoids by not reaching for it routinely.

**How to apply:** when picking up a new task in this project, don't default into the old verification ritual. Ask "is there a specific, articulable reason to distrust this particular change" before writing a test or smoke-testing a binary — if the honest answer is no, ship it after build+vet and let the user drive/verify it themselves.

**Real validation of the "escalate when there's a specific reason to distrust" carve-out (2026-07-14, same day, later).** Built a permission-mode/confirmation-gating feature on top of the existing tool wrapper stack; build+vet passed, and a pure-logic unit test of the new policy function passed too — but the user reported live that `write_file` never confirmed at all, in any mode, even after a fresh restart. Source-reading alone produced a plausible-but-wrong theory (`ctx.Agent().Name()` mismatch) that turned out to be only half right and initially caused a live panic when "fixed." The actual root cause (a `ProcessRequest`-promotion bug silently bypassing every tool wrapper's `Run` — see [[tool-call-concurrency]] for the full story) was only found by writing a real end-to-end test against the live API and watching what actually happened, twice, after each fix attempt. Confirms the rule this memory already states: build+vet and isolated unit tests are not sufficient when the thing being verified is "does ADK actually dispatch through my wrapper" — that's an integration question about a third-party framework's internal wiring, squarely in the "specific reason to distrust" bucket, not the reflexive-ritual one this memory tells me to skip. The lesson isn't "test more by default" — it's "when a live report contradicts a source-grounded theory, re-verify live immediately rather than iterating on more source-reading."
