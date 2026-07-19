// This file is where a raw tea.KeyPressMsg gets interpreted, depending on
// what's currently showing: the inline "/command" suggestion dropdown, an
// open popup menu, or the plain chat view — handleKey is Update's single
// entry point for every keypress (see app.go), and dispatches to
// handleSuggestKey/handlePaletteKey (or one of hitl.go's/keyinput.go's
// own handlers) from there.
package ui

import (
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// quitConfirmWindow is how long a "ctrl+c again to exit" arming (see
// App.quitArmedAt) stays live — a second press after this long is
// treated as a fresh first press instead of the confirmation.
const quitConfirmWindow = 2 * time.Second

func (a *App) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		switch {
		case a.paletteKind == paletteConfirm, a.pendingConfirmation != nil:
			// A HITL approval is blocking the turn — ctrl+c reads the same
			// as Esc/n here (deny) rather than abandoning the whole app
			// mid-decision.
			return a, a.resolveConfirmation(false)
		case a.paletteKind != paletteNone:
			return a, a.backOrClose(a.cancelMenu)
		case a.turnCancel != nil:
			a.stopTurn()
			return a, nil
		case !a.quitArmedAt.IsZero() && time.Since(a.quitArmedAt) < quitConfirmWindow:
			return a, tea.Quit
		default:
			a.quitArmedAt = time.Now()
			a.setNotice("Press ctrl+c again to exit.")
			return a, nil
		}

	case key.Matches(msg, keys.AutoAccept):
		a.toggleAutoAccept()
		return a, nil

	case key.Matches(msg, keys.VerboseTools):
		a.toggleSetting("verbose")
		return a, nil

	case a.paletteKind == paletteTextInput:
		return a.handleTextInputKey(msg)

	case a.paletteKind == paletteConfirm:
		return a.handleConfirmModalKey(msg)

	case a.paletteKind != paletteNone:
		return a.handlePaletteKey(msg)

	case a.pendingConfirmation != nil:
		return a.handlePendingConfirmKey(msg)

	case len(a.suggestMatches) > 0:
		return a.handleSuggestKey(msg)

	case key.Matches(msg, keys.ScrollUp):
		a.jumpToPrevPrompt()
		return a, nil

	case key.Matches(msg, keys.ScrollDown):
		a.jumpToNextPrompt()
		return a, nil

	case key.Matches(msg, keys.Send):
		return a, a.handleSend()
	}

	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	a.layout()
	return a, cmd
}

// handleSuggestKey runs while the inline "/command" dropdown is showing:
// arrow keys move the highlight, Enter runs the highlighted command, Esc
// clears the input to dismiss it, and anything else (more typing,
// backspace, ...) still reaches the textarea so the query keeps narrowing.
func (a *App) handleSuggestKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Up):
		a.suggestIndex = max(a.suggestIndex-1, 0)
		return a, nil

	case key.Matches(msg, keys.Down):
		a.suggestIndex = min(a.suggestIndex+1, len(a.suggestMatches)-1)
		return a, nil

	case key.Matches(msg, keys.Escape):
		a.input.SetValue("")
		a.layout()
		return a, nil

	case key.Matches(msg, keys.Send):
		name := a.suggestMatches[a.suggestIndex].Name
		a.input.SetValue("")
		cmd := a.runCommand(name)
		a.layout()
		return a, cmd
	}

	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	a.layout()
	return a, cmd
}

func (a *App) handlePaletteKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Escape):
		return a, a.backOrClose(a.cancelMenu)

	// DEL/ctrl+DEL only mean anything on the /sessions list itself — see
	// keys.go's DeleteSession/DeleteAllSessions doc comment.
	case a.paletteKind == paletteSessions && key.Matches(msg, keys.DeleteAllSessions):
		if n := len(a.paletteList.Items()); n > 0 {
			a.openDeleteAllSessionsConfirm(n)
		}
		return a, nil

	case a.paletteKind == paletteSessions && key.Matches(msg, keys.DeleteSession):
		if item, ok := a.paletteList.SelectedItem().(paletteItem); ok {
			a.openDeleteSessionConfirm(item.id)
		}
		return a, nil

	case key.Matches(msg, keys.Send):
		item, ok := a.paletteList.SelectedItem().(paletteItem)
		if !ok {
			return a, a.backOrClose(a.closeMenuCmd)
		}
		closeAfter, cmd := a.confirmMenuSelection(item.id)
		if closeAfter {
			return a, tea.Batch(cmd, a.backOrClose(a.closeMenuCmd))
		}
		return a, cmd
	}

	var cmd tea.Cmd
	a.paletteList, cmd = a.paletteList.Update(msg)

	// Live-preview: whatever's highlighted in the /theme or /loader menu
	// is applied immediately, not just on confirm, so navigating repaints
	// the whole app (this popup included) with the candidate theme, or
	// swaps which animation is actively ticking above the input box.
	switch a.paletteKind {
	case paletteTheme:
		if item, ok := a.paletteList.SelectedItem().(paletteItem); ok {
			a.previewTheme(item.id)
		}
	case paletteLoader:
		if item, ok := a.paletteList.SelectedItem().(paletteItem); ok {
			a.previewWorkingAnim(item.id)
		}
	}

	return a, cmd
}
