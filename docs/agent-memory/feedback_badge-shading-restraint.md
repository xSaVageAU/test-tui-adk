---
name: feedback-badge-shading-restraint
description: "When asked to shade/differentiate two parts of one UI badge, use one light-touch foreground change — not a two-tone background split, not stacked attributes (Faint+Bold)."
metadata: 
  node_type: memory
  type: feedback
  originSessionId: 90538001-00e7-4e08-bcb5-9837e57e0995
---

For the help-footer keybind badges ([[project_direction-pivot]] project), the user asked for the bind-text half of each badge to be "slightly darker" than the action-text half. Two escalating attempts were rejected before landing on the right one:

1. First pass: `Key` in `theme.TextFaint`, `Desc` in `theme.TextMuted`, same shared badge background — this was correct but got overridden by the next attempt before the user saw it in isolation.
2. Second pass: added `Faint(true)` to Key and `Bold(true)` to Desc on top of the same colors, reasoning "stack two signals for robustness." User feedback: made Key *harder to read*, not just quieter.
3. Third pass: split into two actual background colors (Key on `Surface`, Desc on `Highlight`/`Warning`) — a real two-tone pill. User feedback: "not looking as good as i thought," explicitly asked to go back to a single-color badge.
4. Final: single shared background, plain `TextFaint`/`TextMuted` foreground difference only, no `Faint`/`Bold` stacking. This is what shipped.

**Why:** the user's mental model was "one badge, one background, two text shades" the whole time — every attempt that changed the background or stacked extra attributes was read as over-engineering a request that was meant to be subtle ("a light touch").

**How to apply:** when a UI tweak request uses words like "slightly"/"a bit"/"subtle," resist adding a second reinforcing technique (attribute + color, or background + foreground) preemptively for "robustness" — ship the single simplest change first and let the user ask for more if it's not enough. This applies broadly to this codebase's theme/styles.go work, not just the help footer.
