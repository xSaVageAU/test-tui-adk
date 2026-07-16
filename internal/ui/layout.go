// This file computes size: how much height each region (top bar, working
// anim/suggestions slot, input box, footer, and whatever's left for the
// viewport) claims out of the terminal's current dimensions, how a resize
// preserves scroll position, and how big a popup renders at. Nothing here
// renders content — see chat.go/header.go/footer.go/palette.go for that —
// this file only decides dimensions those renderers are then called with.
package ui

import "charm.land/bubbles/v2/viewport"

// layout constants: the top bar (agent/status/session meta line + rule) is
// a fixed two-line panel, the input bar is a bordered box (border top/bottom
// + however many lines the input has currently wrapped to, see
// App.inputLines), and the help footer is one line. The viewport claims
// whatever height remains.
const (
	topBarHeight = 2
	footerHeight = 1
)

// popupWidthDefault/popupHeightDefault are every popup's outer size until
// /settings' "Popup width"/"Popup height" is explicitly set (see
// effectivePopupWidth/effectivePopupHeight) — the exact values this app
// shipped with before that setting existed, so an untouched install looks
// unchanged. popup{Width,Height}{Min,Max} bound what a typed value in
// that setting's text field is clamped to (see submitNumericSetting in
// commands.go) — floors keep a popup legible, ceilings keep a typo like
// an extra zero from being taken literally.
const (
	popupWidthDefault  = 50
	popupHeightDefault = 12
	popupWidthMin      = 24
	popupWidthMax      = 200
	popupHeightMin     = 6
	popupHeightMax     = 80
)

// layout recomputes every child component's size from the current terminal
// dimensions and the input box's current wrap height. Called on resize and
// after every input edit, since typing can grow or shrink the input box.
func (a *App) layout() {
	if a.width == 0 {
		return
	}
	width := a.renderWidth()

	a.input.SetWidth(width - 4) // border + padding on each side
	a.inputLines = wrappedLines(a.input)

	if query, active := a.commandQuery(); active {
		a.suggestMatches = matchCommands(query)
	} else {
		a.suggestMatches = nil
	}
	if a.suggestIndex >= len(a.suggestMatches) {
		a.suggestIndex = max(len(a.suggestMatches)-1, 0)
	}

	inputBoxHeight := a.inputLines + 2 // border top/bottom
	// The "/" suggestions dropdown takes over the workingAnim's reserved
	// slot right above the input bar rather than stacking above it (see
	// View()) — so only one of the two is ever reserved here, not both.
	reservedHeight := workingAnimHeight
	if len(a.suggestMatches) > 0 {
		reservedHeight = len(a.suggestMatches) + 2 // border top/bottom
	}
	vpHeight := max(a.height-topBarHeight-reservedHeight-inputBoxHeight-footerHeight, 0)

	if a.viewport.Width() == 0 {
		a.viewport = viewport.New(viewport.WithWidth(width), viewport.WithHeight(vpHeight))
		a.viewport.MouseWheelDelta = 2 // a bit faster than 1, still finer than the 3-line default
		a.viewport.Style = a.styles.Viewport
	} else {
		a.viewport.SetWidth(width)
		a.viewport.SetHeight(vpHeight)
	}

	switch a.paletteKind {
	case paletteTextInput:
		a.keyInput.SetWidth(a.textPopupWidth() - 4)
	case paletteNone:
		// nothing to resize
	default:
		a.paletteList.SetSize(a.paletteWidth(), max(a.paletteHeight()-paletteTitleHeight, 0))
		restylePalette(&a.paletteList, a.styles) // re-sync the title's baked-in width to the new size
	}
	a.refreshTranscript()
}

// resizeAndPreserveScroll is WindowSizeMsg's handler specifically when
// the width changed (see the widthChanged branch in Update — a
// height-only resize skips this entirely and just calls layout()
// directly, since nothing below applies without an actual rewrap).
// layout()'s own refreshTranscript rewraps the whole transcript to the
// new width, which changes how many lines the same content actually
// takes up — a plain numeric viewport.YOffset (a raw line index)
// doesn't survive that. viewport.SetWidth/SetContent never touch it
// themselves (confirmed from bubbles' own source), so left alone it
// keeps pointing at whatever line index it was, which after a rewrap is
// usually a *different* — and on a shrink specifically, visibly earlier
// — chunk of the conversation than what was on screen a moment ago.
// Widening happens to mostly self-correct (fewer wrapped lines means a
// stale offset is more likely to just clamp near the bottom, which is
// often where you want to be anyway); narrowing has no such luck, which
// is exactly the asymmetry reported live: smooth growing the terminal,
// jumpy shrinking it.
//
// Fixed by anchoring to content instead of a line number: capture
// whether the viewport was pinned to the bottom, or which user message
// was at/above the top edge, *before* the rewrap, then restore the
// equivalent position afterward using userMsgLines' new (post-rewrap)
// line numbers for that same message. Bottom-pinning is its own case
// (same convention as followTranscript) rather than folded into the
// message anchor, since anchoring to "the last message" wouldn't
// necessarily still read as "the bottom" once a long reply under it
// changes where the true bottom actually lands.
func (a *App) resizeAndPreserveScroll() {
	wasAtBottom := a.viewport.AtBottom()
	anchor := a.scrollAnchor()

	a.layout()

	switch {
	case wasAtBottom:
		a.viewport.GotoBottom()
	case anchor >= 0 && anchor < len(a.userMsgLines):
		a.viewport.SetYOffset(a.userMsgLines[anchor])
	}
}

// scrollAnchor returns the index into a.userMsgLines of the last user
// message at or before the viewport's current scroll position, or -1 if
// the viewport is scrolled above the first message (e.g. still showing
// the boot banner).
func (a *App) scrollAnchor() int {
	anchor := -1
	for i, line := range a.userMsgLines {
		if line <= a.viewport.YOffset() {
			anchor = i
		}
	}
	return anchor
}

func (a *App) effectivePopupWidth() int {
	if a.popupWidth > 0 {
		return a.popupWidth
	}
	return popupWidthDefault
}

func (a *App) effectivePopupHeight() int {
	if a.popupHeight > 0 {
		return a.popupHeight
	}
	return popupHeightDefault
}

// paletteWidth/paletteHeight are every list-backed popup's actual
// on-screen size: the configured (or default) size, still clamped to the
// terminal's own dimensions so a large configured popup never overflows
// a small window.
func (a *App) paletteWidth() int  { return min(a.width-8, a.effectivePopupWidth()) }
func (a *App) paletteHeight() int { return min(a.height-8, a.effectivePopupHeight()) }

// renderWidth is the actual column count every full-width component
// renders at — one column narrower than the terminal's own reported
// width. Purely a safety margin: during a fast interactive resize, a
// WindowSizeMsg can lag the terminal's true current size by a frame or
// two, and content rendered at exactly the terminal's full width wraps
// onto an unwanted extra line the instant that happens — visible live
// as the input box's own border wrapping and everything below it
// snapping down, then back once the next WindowSizeMsg catches state
// up. One column of slack on the right absorbs that lag before it ever
// reaches the terminal's real edge. Not applied to popups (paletteWidth/
// textPopupWidth) — those are already narrower and centered, nowhere
// near the edge to begin with.
func (a *App) renderWidth() int {
	return max(a.width-1, 0)
}
