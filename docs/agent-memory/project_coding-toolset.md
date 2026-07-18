---
name: coding-toolset
description: "The agent's toolset was refactored to self-registering specs and expanded into a real coding toolset (edit/grep/glob/run_shell) on 2026-07-18."
metadata: 
  node_type: memory
  type: project
  originSessionId: b9c62c7a-8526-4dcb-9b5a-eeacac9ffb48
---

On 2026-07-18 the `internal/adk/tools` package was refactored and expanded from 4 tools into a proper coding toolset. Advances the "Tool Gateway → just more functiontools" line in [[project_vision-hermes-competitor]].

**Refactor (Phase 1):** each tool file now self-registers via `register(spec{...})` in an `init()` (database/sql driver pattern) — adding a tool means writing one new file and editing nothing else; `tools.go` never grows. `spec` carries `destructive`, `resources`, and a `build` closure (a closure, not Name/Description/Handler fields, because `functiontool.New` is generic over arg/result types — must be constructed at the concrete call site). `Registry` applies the `gated(confirmGated(...))` wrapping in one place. `gate.go` untouched; wrapping order still load-bearing (see its doc).

**New tools:** `edit_file` (exact-string replace, unique-unless-replace_all), `grep` (pure-Go regexp walk, no ripgrep — protects single-binary), `glob` (pure-Go `**` support via glob→regexp), `run_shell` (always-confirm, resource = recursive `dirWriteRef` over working dir so it serializes vs all file ops; shell construction is split into build-tagged `run_shell_windows.go`/`run_shell_other.go`). `read_file` upgraded with offset/limit + `cat -n` line numbers (numbers are display-only, NOT part of edit_file matching). UI formatting per-tool in `internal/ui/toolformat.go`.

**Why:** pure-Go for grep/glob deliberately avoids external deps to keep the single-binary story. run_shell modeled as destructive + recursive-write is the conservative-but-safe choice (a command can touch anything).

**Windows shell gotcha (fixed 2026-07-18):** `exec.Command("cmd","/c",command)` makes Go's argv→cmdline builder backslash-escape embedded quotes (`"`→`\"`), which cmd.exe passes through literally — broke every quoted command. Fix: on Windows set `SysProcAttr.CmdLine` = `` `cmd /S /C "`+command+`"` `` (raw, no re-escaping; /S strips exactly the outer quote pair). Verified empirically vs the old path. Do NOT revert to the plain-argv form.

**Background/lifecycle (added 2026-07-18):** `run_shell` gained `background:true` → launches detached, no timeout, returns a `shell_id`; foreground default timeout kept but hard-max ceiling removed. Two new tools: `shell_output` (incremental log read since last check) and `stop_shell` (kills whole tree; both non-destructive — launch was already confirmed, stop only de-escalates). Machinery in `bgproc.go` (registry, capped ring buffer, Wait goroutine, `ShutdownBackground`). Tree-kill is platform-split: Unix `Setpgid`+kill negative PID; Windows `taskkill /T /F /PID` (chosen over Job Objects to stay dep-free). `main.go` calls `adk.ShutdownBackgroundProcesses()` after `p.Run()` (explicit, not defer — os.Exit skips defers) so nothing outlives the TUI. Verified empirically: `taskkill /T` kills parent cmd + child ping (no ghosts). Background Run returns immediately so it doesn't hold the write-gate for a long-lived server.

**How to apply:** to add a tool, copy any file's shape; the runtime allow-list is the root `agent.json` "tools" array (~/.tui-testing/agent.json) — a registered tool still isn't granted until listed there. Note wmic is gone on Win11 build 26200 — use PowerShell `Get-CimInstance Win32_Process` to enumerate processes. Not yet done: write_file create-vs-overwrite confirmation polish; multi-edit; edit_file occurrence-index; graceful (SIGTERM-then-SIGKILL) background stop.
