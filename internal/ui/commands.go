package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"tui-testing/internal/settings"
	"tui-testing/internal/theme"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// commandSpec is a slash command's entry in the registry that both the
// dispatcher (runCommand) and the inline suggestion dropdown draw from, so
// the two can't drift out of sync.
type commandSpec struct {
	Name string
	Desc string
}

var commandSpecs = []commandSpec{
	{Name: "new", Desc: "Start a new session"},
	{Name: "sessions", Desc: "Switch to a past session"},
	{Name: "theme", Desc: "Change the color theme"},
	{Name: "settings", Desc: "Adjust settings"},
	{Name: "key", Desc: "Set a provider API key"},
	{Name: "agents", Desc: "Configure agent provider/model"},
	{Name: "loader", Desc: "Choose the \"working\" animation"},
	{Name: "exit", Desc: "Quit the app"},
}

// commandQuery returns the text after a leading "/" in the input box, and
// whether the input is currently in "typing a command" mode at all.
func (a *App) commandQuery() (string, bool) {
	return strings.CutPrefix(a.input.Value(), "/")
}

// matchCommands returns the commands whose name starts with query, in
// registry order. An empty query matches everything.
func matchCommands(query string) []commandSpec {
	query = strings.ToLower(query)
	var matches []commandSpec
	for _, c := range commandSpecs {
		if strings.HasPrefix(c.Name, query) {
			matches = append(matches, c)
		}
	}
	return matches
}

// renderSuggestions draws the inline "/command" dropdown anchored above
// the input bar — a compact, single-line-per-command list, distinct from
// the full-screen popup a selected command may itself go on to open.
func renderSuggestions(s theme.Styles, matches []commandSpec, selected, width int) string {
	// Inner content width: box width (the full terminal width — matching
	// the viewport below it, not width-2, which left a right-edge gap)
	// minus its own border (2) and left/right padding (2).
	rowWidth := max(width-2-2, 0)

	rows := make([]string, len(matches))
	for i, c := range matches {
		name := "/" + c.Name
		if i == selected {
			left := s.SuggestionSelected.Render(" " + name + " ")
			rows[i] = left + s.SuggestionSelectedDesc.Width(max(rowWidth-lipgloss.Width(left), 0)).Render(" "+c.Desc)
			continue
		}
		left := s.SuggestionItem.Render(" " + name)
		rows[i] = left + s.SuggestionDesc.Width(max(rowWidth-lipgloss.Width(left), 0)).Render("  "+c.Desc)
	}
	return s.Suggestions.Width(width).Render(strings.Join(rows, "\n"))
}

// runCommand dispatches a slash command typed into the input (the leading
// "/" already stripped). Unknown commands surface as a system message
// rather than being silently sent as chat. Returns a tea.Cmd for the one
// command (/sessions) that needs an async backend round-trip before it
// has anything to show; every other command acts synchronously and
// returns nil.
func (a *App) runCommand(name string) tea.Cmd {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "new":
		a.startNewSession()
	case "sessions":
		return a.openSessionsMenu()
	case "theme":
		a.openThemeMenu()
	case "settings":
		a.openSettingsMenu()
	case "key":
		a.openKeyProviderMenu()
	case "agents":
		a.openAgentsMenu()
	case "loader":
		return a.openLoaderMenu()
	case "exit":
		// A typed, deliberate command — unlike ctrl+c (see handleKey),
		// this never needs the "press again to confirm" safety net, so it
		// quits immediately regardless of what else is going on.
		return tea.Quit
	default:
		a.systemMessage("Unknown command: /" + name + " — try /new, /sessions, /theme, /settings, /key, /agents, /loader, or /exit.")
	}
	return nil
}

func (a *App) openThemeMenu() {
	// Remembered so Esc can revert a live preview instead of leaving
	// whatever theme the cursor happened to be resting on when dismissed.
	a.themeMenuOrigin = a.themeMgr.Current().Name

	names := a.themeMgr.Names()
	items := make([]paletteItem, len(names))
	for i, name := range names {
		desc := ""
		if name == a.themeMenuOrigin {
			desc = "current"
		}
		items[i] = paletteItem{id: name, title: name, desc: desc}
	}
	a.openMenu(paletteTheme, "Choose theme", items)
}

// openLoaderMenu is /loader's entry point — same live-preview shape as
// /theme (see previewWorkingAnim/cancelMenu's paletteLoader case), but
// also has to actually kick off the tick loop itself: unlike a real
// turn, nothing else would ever call startWorkingAnim while just
// browsing the list. Returns a tea.Cmd (openThemeMenu doesn't need to)
// for exactly that reason.
func (a *App) openLoaderMenu() tea.Cmd {
	a.loaderMenuOrigin = workingAnimNames[a.workingAnim.variant]

	items := make([]paletteItem, workingAnimCount)
	for i, name := range workingAnimNames {
		desc := ""
		if name == a.loaderMenuOrigin {
			desc = "current"
		}
		items[i] = paletteItem{id: name, title: name, desc: desc}
	}
	a.openMenu(paletteLoader, "Choose \"working\" animation", items)
	return a.startWorkingAnim()
}

// previewWorkingAnim swaps the active variant immediately, without
// waiting for the menu selection to be confirmed — same reasoning as
// previewTheme, called on every highlight change while /loader has
// focus (see handlePaletteKey).
func (a *App) previewWorkingAnim(name string) {
	a.workingAnim.variant = parseWorkingAnimVariant(name)
}

func (a *App) openSettingsMenu() {
	items := []paletteItem{
		{id: "highlight", title: "Highlight user messages", desc: onOff(a.highlightUser)},
		{id: "stream", title: "Stream replies token-by-token", desc: onOff(a.streamReplies)},
		{id: "hitl", title: "Tool approval style", desc: a.hitlMode.String() + " — select to cycle"},
		{id: "permission", title: "Tool approval mode", desc: a.permissionMode.String() + " — select to cycle"},
		{id: "verbose", title: "Verbose tool output", desc: onOff(a.verboseTools)},
		{id: "reasoning", title: "Show reasoning text", desc: onOff(a.showReasoning)},
		{id: "popupWidth", title: "Popup width", desc: fmt.Sprintf("%d — select to edit", a.effectivePopupWidth())},
		{id: "popupHeight", title: "Popup height", desc: fmt.Sprintf("%d — select to edit", a.effectivePopupHeight())},
	}
	a.openMenu(paletteSettings, "Settings", items)
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

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
// toggled, whenever /agents' whole flow finally ends (see toggleAgentTool
// and reloadBackend, which clears the flag once it really reloads).
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

// dropPendingMessage reports (and clears) a message held by sendMessage
// while /key was collecting a key it never actually got — called from
// wherever the /key flow can be cancelled (either its provider-picker
// first step or the key-entry second step), so a cancelled /key never
// leaves a message silently stranded to be surprise-sent by some later,
// unrelated successful connection.
func (a *App) dropPendingMessage() {
	if a.pendingMessage == "" {
		return
	}
	a.pendingMessage = ""
	a.status = theme.StatusError
	a.systemMessage("Message not sent — no API key set.")
}

// confirmMenuSelection runs the effect of picking id from whichever menu
// is currently open. For the /theme menu the theme is already live (see
// previewTheme) — confirming just makes it permanent instead of
// reverting. Returns whether this was a terminal action: true for a
// setting toggled, a theme/session picked, or a provider saved — false
// when the selection opened another menu instead (see agentsmenu.go's
// /agents steps, and the popupWidth/popupHeight cases below), meaning
// handlePaletteKey should leave whatever new step this call just opened
// alone. When true, handlePaletteKey runs backOrClose rather than an
// unconditional close — a step reached via pushMenuBack goes back to its
// parent instead of leaving the popup; only a true top-level pick (theme,
// loader, session, a plain settings toggle) actually closes it. The
// tea.Cmd, when non-nil, is a terminal action's own async follow-up
// (saving an agent's provider kicks off a backend reload) — batched
// alongside backOrClose's own result regardless of which one fires.
func (a *App) confirmMenuSelection(id string) (bool, tea.Cmd) {
	switch a.paletteKind {
	case paletteTheme:
		a.systemMessage("Theme set to " + id + ".")
	case paletteLoader:
		// Explicit rather than relying solely on previewWorkingAnim: if
		// Enter is pressed on the very first row without ever arrowing,
		// no highlight-change event (and so no preview call) ever fired
		// for it.
		a.workingAnim.variant = parseWorkingAnimVariant(id)
		a.persistSettings()
		a.systemMessage("Working animation set to " + id + ".")
	case paletteSettings:
		switch id {
		case "popupWidth":
			a.pushMenuBack(func() tea.Cmd { a.openSettingsMenu(); return nil })
			a.openPopupSizeInput(textPopupPopupWidth, "Set popup width", a.effectivePopupWidth())
			return false, nil
		case "popupHeight":
			a.pushMenuBack(func() tea.Cmd { a.openSettingsMenu(); return nil })
			a.openPopupSizeInput(textPopupPopupHeight, "Set popup height", a.effectivePopupHeight())
			return false, nil
		default:
			a.toggleSetting(id)
		}
	case paletteSessions:
		a.switchSession(id)
	case paletteKeyProvider:
		a.pushMenuBack(func() tea.Cmd { a.openKeyProviderMenu(); return nil })
		a.openKeyInput(id)
		return false, nil
	case paletteAgents:
		a.pushMenuBack(func() tea.Cmd { a.openAgentsMenu(); return nil })
		a.openAgentDetailMenu(id)
		return false, nil
	case paletteAgentDetail:
		return a.confirmAgentDetailSelection(id)
	case paletteAgentProvider:
		return true, a.saveAgentProvider(id)
	case paletteAgentTools:
		// Stays open, same reasoning as the whole page: a checklist is
		// for toggling several things in one visit, not one pick and
		// close like every other menu here.
		a.toggleAgentTool(id)
		return false, nil
	}
	return true, nil
}

// previewTheme applies a theme immediately, without waiting for the menu
// selection to be confirmed, so arrowing through /theme repaints the whole
// app (including the popup itself) live. Called on every highlight change
// while the /theme menu has focus.
func (a *App) previewTheme(name string) {
	a.themeMgr.Set(name)
	a.applyTheme()
}

func (a *App) toggleSetting(id string) {
	switch id {
	case "highlight":
		a.highlightUser = !a.highlightUser
	case "stream":
		a.streamReplies = !a.streamReplies
	case "hitl":
		a.hitlMode = a.hitlMode.next()
	case "permission":
		a.permissionMode = a.permissionMode.next()
	case "verbose":
		a.verboseTools = !a.verboseTools
	case "reasoning":
		a.showReasoning = !a.showReasoning
	}
	a.persistSettings()
	a.refreshTranscript()
}

// submitPopupSize is Enter's handler for /settings' "Popup width"/"Popup
// height" fields (textPopupPopupWidth/textPopupPopupHeight) — parses the
// typed value, clamps it into the popup{Width,Height}{Min,Max} range, and
// persists it. A non-numeric entry is reported and left unchanged rather
// than silently falling back to something the user didn't type.
func (a *App) submitPopupSize() tea.Cmd {
	raw := strings.TrimSpace(a.keyInput.Value())
	kind := a.textPopupKind

	n, err := strconv.Atoi(raw)
	if err != nil {
		a.systemMessage("Not a number — popup size left unchanged.")
		return a.backOrClose(a.closeMenuCmd)
	}

	// Applied before backOrClose below reopens /settings — that reopen
	// rebuilds its rows from a.effectivePopupWidth/Height right now, so
	// the value has to already be live or the row we just edited would
	// flash the old number for one frame.
	switch kind {
	case textPopupPopupWidth:
		a.popupWidth = clampInt(n, popupWidthMin, popupWidthMax)
		a.systemMessage(fmt.Sprintf("Popup width set to %d.", a.popupWidth))
	case textPopupPopupHeight:
		a.popupHeight = clampInt(n, popupHeightMin, popupHeightMax)
		a.systemMessage(fmt.Sprintf("Popup height set to %d.", a.popupHeight))
	}
	a.persistSettings()
	return a.backOrClose(a.closeMenuCmd)
}

func clampInt(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// persistSettings writes the current UI toggles to settings.json.
// Best-effort, same reasoning as SaveAPIKey in internal/adk: there's
// nowhere safe to report a write failure from inside the TUI, and the
// worst case is just that a toggle doesn't survive a restart.
func (a *App) persistSettings() {
	s := settings.Load()
	s.UI = settings.UISettings{
		HighlightUser:     a.highlightUser,
		StreamReplies:     a.streamReplies,
		HITLMode:          a.hitlMode.String(),
		PermissionMode:    a.permissionMode.String(),
		VerboseTools:      a.verboseTools,
		WorkingAnim:       workingAnimNames[a.workingAnim.variant],
		HideReasoningText: !a.showReasoning,
		PopupWidth:        a.popupWidth,
		PopupHeight:       a.popupHeight,
	}
	_ = settings.Save(s)
}

func (a *App) systemMessage(text string) {
	a.messages = append(a.messages, ChatMessage{Role: RoleSystem, Content: text, At: time.Now()})
	a.followTranscript()
}
