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
	{Name: "interrupt", Desc: "Stop the current response, optionally with a new prompt"},
	{Name: "reload-agents", Desc: "Reload agents/tools/MCP servers from disk"},
	{Name: "exit", Desc: "Quit the app"},
}

// interruptCommandName is /interrupt's registry name — pulled out as a
// const since runCommand also has to recognize "interrupt <prompt>"
// as its own shape, not just the bare command matchCommands/the switch
// below already handle.
const interruptCommandName = "interrupt"

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
	trimmed := strings.TrimSpace(name)
	lower := strings.ToLower(trimmed)

	// /interrupt is the one command that takes a free-text argument (the
	// redirect prompt), so it can't go through the bare-word switch
	// below like every other command — matched here first, against both
	// its bare form and "interrupt <anything>", the argument's own
	// casing left untouched since it's about to become chat text.
	switch {
	case lower == interruptCommandName:
		return a.interruptAndSend("")
	case strings.HasPrefix(lower, interruptCommandName+" "):
		return a.interruptAndSend(strings.TrimSpace(trimmed[len(interruptCommandName)+1:]))
	}

	switch lower {
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
	case "reload-agents":
		// Same reloadBackend() /agents already uses after a config edit —
		// its own turnInProgress() guard already handles "can't reload
		// mid-turn" correctly for this manual path, nothing extra needed
		// here. See turn.go's concludeTurn for the other way this fires
		// (a reload_agents tool call, deferred until the turn concludes).
		a.setNotice("Reloading agents...")
		return a.reloadBackend()
	case "exit":
		// A typed, deliberate command — unlike ctrl+c (see handleKey),
		// this never needs the "press again to confirm" safety net, so it
		// quits immediately regardless of what else is going on.
		return tea.Quit
	default:
		a.systemMessage("Unknown command: /" + name + " — try /new, /sessions, /theme, /settings, /key, /agents, /loader, /interrupt, /reload-agents, or /exit.")
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

// openSettingsMenu is /settings' entry point — a top-level category
// picker rather than one flat list of every toggle, so purely visual/TUI
// settings stay visually and structurally separate from agent/tool
// execution policy (currently just "Tool approval mode", but expected to
// grow — see settings.AgentSettings). Selecting a category pushes back
// here (see confirmMenuSelection's paletteSettings case) so Esc/Cancel
// from either sub-page returns to this picker, not straight to the chat.
func (a *App) openSettingsMenu() {
	items := []paletteItem{
		{id: "tui", title: "TUI Settings", desc: "display, popups, tool output"},
		{id: "agent", title: "Agent Settings", desc: "tool approval policy"},
	}
	a.openMenu(paletteSettings, "Settings", items)
}

// openTUISettingsMenu is /settings → "TUI Settings" — purely
// display/interaction toggles, persisted under settings.json's "ui" key.
func (a *App) openTUISettingsMenu() {
	items := []paletteItem{
		{id: "highlight", title: "Highlight user messages", desc: onOff(a.highlightUser)},
		{id: "stream", title: "Stream replies token-by-token", desc: onOff(a.streamReplies)},
		{id: "hitl", title: "Tool approval style", desc: a.hitlMode.String() + " — select to cycle"},
		{id: "verbose", title: "Verbose tool output", desc: onOff(a.verboseTools)},
		{id: "reasoning", title: "Show reasoning text", desc: onOff(a.showReasoning)},
		{id: "popupWidth", title: "Popup width", desc: fmt.Sprintf("%d — select to edit", a.effectivePopupWidth())},
		{id: "popupHeight", title: "Popup height", desc: fmt.Sprintf("%d — select to edit", a.effectivePopupHeight())},
		{id: "toolPreviewLines", title: "Tool output preview lines", desc: fmt.Sprintf("%d — select to edit", a.effectiveToolPreviewMaxLines())},
	}
	a.openMenu(paletteSettingsTUI, "TUI Settings", items)
}

// openAgentSettingsMenu is /settings → "Agent Settings" — agent/tool
// execution policy, persisted under settings.json's "agent" key. Just
// one row today (permission mode); the category exists ahead of having
// much to put in it so later additions (per-tool policy, execution
// targets, ...) have an obvious, already-separated home.
func (a *App) openAgentSettingsMenu() {
	items := []paletteItem{
		{id: "permission", title: "Tool approval mode", desc: a.permissionMode.String() + " — select to cycle"},
	}
	// The execution-target row only appears when the app was wired with a
	// ConfigureTarget closure (main.go always is; a test/embedding harness
	// may not be) — same nil-disables convention as the /agents rows.
	if a.configureTarget != nil {
		items = append(items, paletteItem{id: "target", title: "Tool execution target", desc: a.targetType + " — select to cycle"})
	}
	a.openMenu(paletteSettingsAgent, "Agent Settings", items)
}

// effectiveToolPreviewMaxLines is verbose tool output's actual line
// cap: the configured override if /settings' "Tool output preview
// lines" has been set, else toolPreviewMaxLinesDefault.
func (a *App) effectiveToolPreviewMaxLines() int {
	if a.toolPreviewMaxLines > 0 {
		return a.toolPreviewMaxLines
	}
	return toolPreviewMaxLinesDefault
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
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
		a.setNotice("Theme set to " + id + ".")
	case paletteLoader:
		// Explicit rather than relying solely on previewWorkingAnim: if
		// Enter is pressed on the very first row without ever arrowing,
		// no highlight-change event (and so no preview call) ever fired
		// for it.
		a.workingAnim.variant = parseWorkingAnimVariant(id)
		a.persistSettings()
		a.setNotice("Working animation set to " + id + ".")
	case paletteSettings:
		switch id {
		case "tui":
			a.pushMenuBack(func() tea.Cmd { a.openSettingsMenu(); return nil })
			a.openTUISettingsMenu()
			return false, nil
		case "agent":
			a.pushMenuBack(func() tea.Cmd { a.openSettingsMenu(); return nil })
			a.openAgentSettingsMenu()
			return false, nil
		}
	case paletteSettingsTUI:
		switch id {
		case "popupWidth":
			a.pushMenuBack(func() tea.Cmd { a.openTUISettingsMenu(); return nil })
			a.openPopupSizeInput(textPopupPopupWidth, "Set popup width", a.effectivePopupWidth())
			return false, nil
		case "popupHeight":
			a.pushMenuBack(func() tea.Cmd { a.openTUISettingsMenu(); return nil })
			a.openPopupSizeInput(textPopupPopupHeight, "Set popup height", a.effectivePopupHeight())
			return false, nil
		case "toolPreviewLines":
			a.pushMenuBack(func() tea.Cmd { a.openTUISettingsMenu(); return nil })
			a.openPopupSizeInput(textPopupToolPreviewLines, "Set tool output preview lines", a.effectiveToolPreviewMaxLines())
			return false, nil
		default:
			a.toggleSetting(id)
		}
	case paletteSettingsAgent:
		if id == "target" {
			a.cycleExecutionTarget()
		} else {
			a.toggleSetting(id)
		}
	case paletteSessions:
		return true, a.switchSession(id)
	case paletteConfirmDeleteSession:
		return true, a.confirmDeleteSession(id)
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

// cycleExecutionTarget flips the tool execution target between host and
// ssh, persists it, then re-installs the target from settings and reports
// the outcome. The new type is persisted *before* configureTarget runs
// because that reads the target config back from disk (see
// tools.ConfigureTarget). On an SSH failure it reverts to host so the
// persisted/displayed state matches the local fallback the tools actually
// got, rather than claiming "ssh" while running locally.
func (a *App) cycleExecutionTarget() {
	if a.configureTarget == nil {
		return
	}
	if a.targetType == settings.TargetSSH {
		a.targetType = settings.TargetHost
	} else {
		a.targetType = settings.TargetSSH
	}
	a.persistSettings()

	desc, err := a.configureTarget()
	if err != nil {
		a.status = theme.StatusError
		a.systemMessage("Execution target failed: " + err.Error() + " — staying on host.")
		if a.targetType != settings.TargetHost {
			a.targetType = settings.TargetHost
			a.persistSettings()
		}
	} else {
		a.setNotice("Tools now running on: " + desc)
	}
	a.refreshTranscript()
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

// submitNumericSetting is Enter's handler for every /settings numeric
// text field (textPopupPopupWidth/textPopupPopupHeight/
// textPopupToolPreviewLines) — parses the typed value, clamps it into
// that field's own min/max range, and persists it. A non-numeric entry
// is reported and left unchanged rather than silently falling back to
// something the user didn't type.
func (a *App) submitNumericSetting() tea.Cmd {
	raw := strings.TrimSpace(a.keyInput.Value())
	kind := a.textPopupKind

	n, err := strconv.Atoi(raw)
	if err != nil {
		a.systemMessage("Not a number — setting left unchanged.")
		return a.backOrClose(a.closeMenuCmd)
	}

	// Applied before backOrClose below reopens /settings — that reopen
	// rebuilds its rows from the current value right now, so it has to
	// already be live or the row we just edited would flash the old
	// number for one frame.
	switch kind {
	case textPopupPopupWidth:
		a.popupWidth = clampInt(n, popupWidthMin, popupWidthMax)
		a.setNotice(fmt.Sprintf("Popup width set to %d.", a.popupWidth))
	case textPopupPopupHeight:
		a.popupHeight = clampInt(n, popupHeightMin, popupHeightMax)
		a.setNotice(fmt.Sprintf("Popup height set to %d.", a.popupHeight))
	case textPopupToolPreviewLines:
		a.toolPreviewMaxLines = clampInt(n, toolPreviewMaxLinesMin, toolPreviewMaxLinesMax)
		a.setNotice(fmt.Sprintf("Tool output preview lines set to %d.", a.toolPreviewMaxLines))
		// Unlike popup width/height, this affects transcript rendering
		// directly (verbose tool-output blocks), so re-render now rather
		// than waiting for the next unrelated refresh to pick it up.
		a.refreshTranscript()
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
		HighlightUser:       a.highlightUser,
		StreamReplies:       a.streamReplies,
		HITLMode:            a.hitlMode.String(),
		VerboseTools:        a.verboseTools,
		WorkingAnim:         workingAnimNames[a.workingAnim.variant],
		HideReasoningText:   !a.showReasoning,
		PopupWidth:          a.popupWidth,
		PopupHeight:         a.popupHeight,
		ToolPreviewMaxLines: a.toolPreviewMaxLines,
	}
	// Mutate the loaded Agent section in place rather than replacing it,
	// so fields the UI doesn't own here — the whole Target block (its SSH
	// host/user/key are edited in settings.toml by hand, not in /settings)
	// — survive a settings write instead of being clobbered.
	s.Agent.PermissionMode = a.permissionMode.String()
	s.Agent.Target.Type = a.targetType
	_ = settings.Save(s)
}

func (a *App) systemMessage(text string) {
	a.messages = append(a.messages, ChatMessage{Role: RoleSystem, Content: text, At: time.Now()})
	a.followTranscript()
}
