package ui

import (
	"strings"
	"time"

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
	{Name: "key", Desc: "Set your GOOGLE_API_KEY"},
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
		a.openKeyInput()
	default:
		a.systemMessage("Unknown command: /" + name + " — try /new, /sessions, /theme, /settings, or /key.")
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

func (a *App) openSettingsMenu() {
	items := []paletteItem{
		{id: "highlight", title: "Highlight user messages", desc: onOff(a.highlightUser)},
		{id: "stream", title: "Stream replies token-by-token", desc: onOff(a.streamReplies)},
		{id: "hitl", title: "Tool approval style", desc: a.hitlMode.String() + " — select to cycle"},
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
	case paletteKeyInput:
		if a.pendingMessage != "" {
			a.pendingMessage = ""
			a.status = theme.StatusError
			a.systemMessage("Message not sent — no API key set.")
		}
	}
	a.closeMenu()
}

// confirmMenuSelection runs the effect of picking id from whichever menu
// is currently open. For the /theme menu the theme is already live (see
// previewTheme) — confirming just makes it permanent instead of reverting.
func (a *App) confirmMenuSelection(id string) {
	switch a.paletteKind {
	case paletteTheme:
		a.systemMessage("Theme set to " + id + ".")
	case paletteSettings:
		a.toggleSetting(id)
	case paletteSessions:
		a.switchSession(id)
	}
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
	}
	a.refreshTranscript()
}

func (a *App) systemMessage(text string) {
	a.messages = append(a.messages, ChatMessage{Role: RoleSystem, Content: text, At: time.Now()})
	a.followTranscript()
}
