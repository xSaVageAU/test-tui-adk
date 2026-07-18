# Agent memory snapshot

A verbatim export of Claude Code's persistent memory for this project, taken 2026-07-18 when development moved to a new machine. These are not docs written for humans (though they're readable) — they're the distilled facts, decisions, reversals, and working-style calibration a Claude agent accumulated across the project's history, each with the *why* and a "how to apply" section.

`MEMORY.md` is the index; the other files are one memory each. `[[name]]` links refer to another file's `name:` frontmatter slug.

## Using this on a new machine

Two options, not mutually exclusive:

1. **Seed Claude Code's memory directly** (full transfer): copy every file in this directory into the new machine's per-project memory directory, which Claude Code creates on first use of the project:
   `~/.claude/projects/<project-path-slug>/memory/`
   (the slug is the project's absolute path with separators/colons replaced by dashes — check which directory appears there after opening the project once). Claude then starts with these memories loaded as if it had lived through the history itself.

2. **Just point Claude at this directory** and ask it to read through it before working. Slower per-session, but requires no setup.

## What's here that CLAUDE.md isn't

CLAUDE.md covers how the code works today. These files additionally carry: work that was built and deliberately reverted (ADK memory.Service), work that is frozen pending a design discussion (sub-agent expansion), roadmaps that were scoped but not started (theme system, remaining execution-target kinds), hard-won ADK bug forensics, and feedback the user has given about how they like to work. When a memory and the current code disagree, the code wins — verify before acting on a dated claim.
