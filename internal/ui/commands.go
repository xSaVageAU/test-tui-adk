package ui

import (
	"strings"
	"time"

	"tui-testing/internal/settings"
	"tui-testing/internal/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	// Inner content width: box width minus its own left/right padding.
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
	return s.Suggestions.Width(width - 2).Render(strings.Join(rows, "\n"))
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
	default:
		a.systemMessage("Unknown command: /" + name + " — try /new, /sessions, /theme, /settings, /key, /agents, or /loader.")
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
}

// cancelMenu closes the open menu, undoing whatever it was holding open
// for: the /theme menu reverts its live preview back to the theme that
// was active before it opened, and the /key menu — if it was auto-opened
// to collect a key for a message that's waiting on one — drops that
// message rather than silently sending it later from some unrelated /key
// run.
func (a *App) cancelMenu() {
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
	a.closeMenu()
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
// reverting. Returns whether the menu should close: true for a terminal
// action (a setting toggled, a theme/session picked), false when the
// selection opened another menu instead (see agentsmenu.go's /agents
// steps) — handlePaletteKey only closes when this is true, so a
// multi-step flow can descend without being force-closed after each
// pick. The tea.Cmd, when non-nil, is a terminal action that also needs
// an async follow-up (saving an agent's provider kicks off a backend
// reload) — handlePaletteKey runs it regardless of the close decision.
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
		a.toggleSetting(id)
	case paletteSessions:
		a.switchSession(id)
	case paletteKeyProvider:
		a.openKeyInput(id)
		return false, nil
	case paletteAgents:
		a.openAgentDetailMenu(id)
		return false, nil
	case paletteAgentDetail:
		return a.confirmAgentDetailSelection(id)
	case paletteAgentProvider:
		return true, a.saveAgentProvider(id)
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
	}
	a.persistSettings()
	a.refreshTranscript()
}

// persistSettings writes the current UI toggles to settings.json.
// Best-effort, same reasoning as SaveAPIKey in internal/adk: there's
// nowhere safe to report a write failure from inside the TUI, and the
// worst case is just that a toggle doesn't survive a restart.
func (a *App) persistSettings() {
	s := settings.Load()
	s.UI = settings.UISettings{
		HighlightUser:  a.highlightUser,
		StreamReplies:  a.streamReplies,
		HITLMode:       a.hitlMode.String(),
		PermissionMode: a.permissionMode.String(),
		VerboseTools:   a.verboseTools,
		WorkingAnim:    workingAnimNames[a.workingAnim.variant],
	}
	_ = settings.Save(s)
}

func (a *App) systemMessage(text string) {
	a.messages = append(a.messages, ChatMessage{Role: RoleSystem, Content: text, At: time.Now()})
	a.followTranscript()
}
