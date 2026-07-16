// This file is the generic popup-menu machinery every menu in the app is
// built on: opening/closing the palette, and the depth-aware "step back
// one level instead of leaving the popup" stack (pushMenuBack/backOrClose)
// that /agents, /key, and /settings' nested steps all rely on. It knows
// nothing about what any particular menu contains — see commands.go for
// /theme, /settings, /loader; agentsmenu.go for /agents; keyinput.go for
// /key and the shared text-input popup; hitl.go for the Approve/Deny
// modal.
package ui

import tea "charm.land/bubbletea/v2"

func (a *App) openMenu(kind paletteKind, title string, items []paletteItem) {
	a.paletteKind = kind
	a.paletteTitle = title
	a.paletteList = newPaletteList(items, a.styles, a.paletteWidth(), a.paletteHeight())
}

func (a *App) closeMenu() {
	a.paletteKind = paletteNone
	// Whatever nested step brought us here is over — wiping the stack here
	// (rather than only where it's consumed) means every path that ends a
	// popup, including resolveConfirmation's unrelated paletteConfirm
	// close, leaves it empty for next time, defensively.
	a.menuBack = nil
}

// closeMenuCmd is closeMenu adapted to backOrClose's func() tea.Cmd shape
// — the "nothing left to go back to, actually leave the popup" case for a
// terminal selection (Esc's equivalent is cancelMenu, which also reverts
// whatever the top-level menu's own cancel semantics are).
func (a *App) closeMenuCmd() tea.Cmd {
	a.closeMenu()
	return nil
}

// pushMenuBack records how to reopen the menu currently on screen, right
// before opening one of its nested steps (e.g. /agents' agent list opening
// its Provider/Model/Tools detail page, or /settings opening a numeric
// field) — see backOrClose, which pops and replays this instead of
// leaving the whole popup when the user backs out of that step.
func (a *App) pushMenuBack(reopen func() tea.Cmd) {
	a.menuBack = append(a.menuBack, reopen)
}

// backOrClose is the shared depth-aware "leave the current step" handler
// used by both Esc (handlePaletteKey/handleTextInputKey) and a terminal
// selection: if a parent menu is on the stack — this step was reached via
// pushMenuBack — pop and reopen it instead of leaving the popup, so
// stepping out of a nested menu goes back one level rather than dropping
// all the way out to the chat. Only once the stack is empty (the actual
// top of whatever flow this is) does atTop run: cancelMenu's per-kind
// revert for Esc, or closeMenuCmd's plain close for a confirm.
func (a *App) backOrClose(atTop func() tea.Cmd) tea.Cmd {
	if len(a.menuBack) == 0 {
		return atTop()
	}
	reopen := a.menuBack[len(a.menuBack)-1]
	a.menuBack = a.menuBack[:len(a.menuBack)-1]
	return reopen()
}

// cancelMenu is Esc's handler once backOrClose finds nothing left on the
// stack to go back to — i.e. the actual top of whatever flow is open —
// undoing whatever that top-level menu was holding open for: the /theme
// menu reverts its live preview back to the theme that was active before
// it opened, the /key menu — if it was auto-opened to collect a key for a
// message that's waiting on one — drops that message rather than
// silently sending it later from some unrelated /key run. The
// agentToolsChanged check runs regardless of which menu happens to be on
// screen at this point (not just paletteAgentTools) since backOrClose
// means Esc from the Tools page itself almost always just steps back to
// the agent's detail page rather than reaching here directly — this is
// what actually fires the one reload for however many boxes got
// toggled, whenever /agents' whole flow finally ends (see
// agentsmenu.go's toggleAgentTool and reloadBackend, which clears the
// flag once it really reloads).
func (a *App) cancelMenu() tea.Cmd {
	var cmd tea.Cmd
	switch a.paletteKind {
	case paletteTheme:
		a.themeMgr.Set(a.themeMenuOrigin)
		a.applyTheme()
	case paletteLoader:
		a.workingAnim.variant = parseWorkingAnimVariant(a.loaderMenuOrigin)
	case paletteKeyProvider:
		a.dropPendingMessage()
	case paletteTextInput:
		if a.textPopupKind == textPopupAPIKey {
			a.dropPendingMessage()
		}
	}
	if a.agentToolsChanged {
		a.systemMessage("Tools updated. Reloading agents...")
		cmd = a.reloadBackend()
	}
	a.closeMenu()
	return cmd
}
