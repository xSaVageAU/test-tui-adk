---
name: theme-system-roadmap
description: "Near-term plan is to keep expanding the semantic color palette; deep config-driven customization (widget attributes, custom boot art, movable layout zones) is a deliberate later-roadmap item, not scope creep to avoid."
metadata: 
  node_type: memory
  type: project
  originSessionId: 90538001-00e7-4e08-bcb5-9837e57e0995
---

Discussed 2026-07-15: how far to take `internal/theme`'s customization system (currently a fixed 14-color-token `Theme` struct per JSON file, feeding one hardcoded `Styles.New()` compiler — see `internal/theme/{theme,load,manager,styles}.go`).

**Decision: expand the palette (more semantic color tokens / less hardcoded-in-Go styling) for now** — a full attribute-level theme system is explicitly deferred, not rejected.

**The larger vision, for later** (user's own words): people should be able to get "really deep" with customization purely through config files —
- Widget-level style attributes overridable per theme, not just the 14 base colors (bold/italic/padding/border-style per component, not hardcoded in `styles.go`'s `New()`).
- Custom boot-art: let a theme/config supply its own ASCII art for the boot banner instead of the built-in one.
- A "zones" layout system — users designating where components (chat, input, header, etc.) sit, effectively a movable/dockable layout, not just a fixed vertical stack.

**Why:** the user's own framing — "a full custom theme system might be a bit much but it'd be good for the roadmap." This is a genuine future milestone for [[agent-platform-vision]], not an idea that was rejected — don't downgrade or forget it when the current palette-expansion work feels "done."

**How to apply:** when doing near-term theme work, keep it inside the current architecture (adding tokens, not adding override machinery). When the user is ready to revisit the bigger system, this memory has the three concrete pillars (widget attributes, custom boot art, layout zones) already scoped from this conversation — don't re-derive them from scratch.
