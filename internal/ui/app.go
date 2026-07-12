// Package ui is the "shell" of the TUI: the root Bubble Tea model plus the
// components it composes (header, chat viewport, input bar, command
// palette). Layout and behavior live here; color/style vocabulary lives in
// internal/theme. Swapping a theme should never require touching this file.
package ui

import (
	"context"
	"strings"
	"time"

	"tui-testing/internal/theme"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// layout constants: the top bar (agent/status/session meta line + rule) is
// a fixed two-line panel, the input bar is a bordered box (border top/bottom
// + however many lines the input has currently wrapped to, see
// App.inputLines), and the help footer is one line. The viewport claims
// whatever height remains.
const (
	topBarHeight = 2
	footerHeight = 1
)

// App is the root tea.Model. It owns global state (theme, size, active
// agent) and delegates rendering of each region to that region's own
// render function.
type App struct {
	themeMgr   *theme.Manager
	styles     theme.Styles
	backend    Backend        // nil until a key is set; see NewApp and /key
	newBackend BackendFactory // builds a Backend from a key typed into /key; nil disables /key
	bootInfo   BootInfo       // frozen at construction; see bootart.go

	// sessionID identifies this run's conversation to the backend — a
	// fresh one generated per launch (see newSessionID), so restarting
	// starts a new conversation rather than resuming the last one now
	// that sessions persist across restarts.
	sessionID string

	// pendingMessage holds a message sent with no backend connected,
	// until /key (auto-opened for exactly this) resolves it — see
	// sendMessage and the keySetMsg handler in Update.
	pendingMessage string

	width, height int
	ready         bool

	// agentName is the backend's agent name, shown in the header — fixed
	// for the process lifetime, set once from AppConfig.AgentName. There's
	// only ever one voice in the conversation (the backend may consult
	// specialists internally via agent-as-tool, but that's invisible at
	// this layer — see ui.StreamChunk), so unlike most of what's in this
	// struct, this never changes after NewApp.
	agentName      string
	status         theme.StatusKind
	messages       []ChatMessage
	highlightUser  bool   // experimental: backdrop highlight behind user messages
	streamReplies  bool   // token-by-token replies via Backend.Stream instead of Send
	lastPromptText string // most recent user message; backs the sticky-prompt overlay in View()

	hitlMode            hitlMode // how a pending tool approval is presented — see hitl.go
	pendingConfirmation *pendingConfirmation

	// In-flight stream state, set by streamStartMsg and cleared once the
	// channel closes or errors — see dispatchToBackend and the
	// streamChunkMsg handler in Update.
	streamChan        <-chan StreamChunk
	streamingMsgIndex int
	toolMsgIndex      map[string]int // tool call ID -> index into messages; see upsertToolMessage

	// turnUsage/turnFinishReason accumulate across every model call one
	// logical turn makes — including across a HITL pause/resume, which
	// closes and reopens the stream channel but is still the same turn —
	// until attachTurnUsage lands them on the turn's final agent message.
	// See dispatchToBackend (where these reset) and attachTurnUsage.
	turnUsage        *TokenUsage
	turnFinishReason string

	viewport     viewport.Model
	userMsgLines []int // line offset of each user message's block; see renderTranscript
	input        textarea.Model
	inputLines   int // current wrapped height of the input box, [minInputLines, maxInputLines]
	help         help.Model

	paletteKind     paletteKind
	paletteTitle    string
	paletteList     list.Model
	keyInput        textinput.Model // backs paletteKeyInput; see keyinput.go
	themeMenuOrigin string          // theme active before /theme opened, for Esc to revert to

	suggestMatches []commandSpec // live "/command" matches for the current input, if any
	suggestIndex   int
}

// AppConfig bundles NewApp's startup inputs. Grew past the point where a
// positional parameter list stayed readable, especially at the main.go
// call site.
type AppConfig struct {
	// Backend may be nil if no API key was available at startup —
	// sendMessage then holds the message and opens /key instead of
	// sending anything mock/fake.
	Backend Backend
	// BackendNote is shown as the opening system message: why there's no
	// backend yet (a bad key, not simply a missing one — see main.go), or
	// "" if there's nothing worth saying.
	BackendNote string
	// NewBackend is what /key calls to build a fresh Backend from a
	// typed-in key. Pass nil to disable /key entirely.
	NewBackend BackendFactory
	// ModelName is shown in the boot banner. "" renders as "unknown".
	ModelName string
	// AgentName is the backend's agent name, shown in the boot banner and
	// the header. "" renders as "unknown".
	AgentName string
}

// NewApp constructs the app with the default (first-registered) theme
// active.
func NewApp(cfg AppConfig) *App {
	mgr := theme.NewManager(theme.Defaults()...)
	styles := mgr.Styles()

	var messages []ChatMessage
	if cfg.BackendNote != "" {
		messages = []ChatMessage{{Role: RoleSystem, Content: cfg.BackendNote, At: time.Now()}}
	}

	a := &App{
		themeMgr:   mgr,
		styles:     styles,
		backend:    cfg.Backend,
		newBackend: cfg.NewBackend,
		sessionID:  newSessionID(),
		bootInfo: BootInfo{
			Agent:     orPlaceholder(cfg.AgentName, "unknown"),
			Model:     cfg.ModelName,
			Connected: cfg.Backend != nil,
		},
		agentName:     cfg.AgentName,
		status:        theme.StatusIdle,
		highlightUser: true,
		streamReplies: true,
		inputLines:    minInputLines,
		messages:      messages,
		input:         newInput(styles),
		help:          help.New(),
	}
	a.help.ShortSeparator = "  "
	return a
}

func (a *App) Init() tea.Cmd {
	return a.input.Focus()
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		a.layout()
		a.ready = true
		return a, nil

	case tea.KeyMsg:
		return a.handleKey(msg)

	case agentReplyMsg:
		if msg.err != nil {
			a.status = theme.StatusError
			a.systemMessage("Error: " + msg.err.Error())
			return a, nil
		}
		a.messages = append(a.messages, ChatMessage{Role: RoleAgent, Content: msg.text, At: time.Now()})
		a.status = theme.StatusIdle
		a.followTranscript()
		return a, nil

	case streamStartMsg:
		// Empty placeholder that streamChunkMsg fills in as chunks arrive.
		// Tracked by index rather than "last message" so it stays correct
		// even if something else gets appended to the transcript mid-stream.
		a.messages = append(a.messages, ChatMessage{Role: RoleAgent, Content: "", At: time.Now()})
		a.streamingMsgIndex = len(a.messages) - 1
		a.streamChan = msg.ch
		a.followTranscript()
		return a, readStreamChunk(msg.ch)

	case streamChunkMsg:
		if !msg.ok {
			a.streamChan = nil
			a.status = theme.StatusIdle
			a.dropEmptyStreamingPlaceholder()
			// pendingConfirmation set means this channel close is a HITL
			// pause, not the turn actually ending — resolveConfirmation
			// reopens a fresh channel for the same turn, so the running
			// totals need to survive until then rather than land now.
			if a.pendingConfirmation == nil {
				a.attachTurnUsage()
			}
			a.followTranscript()
			return a, nil
		}
		if msg.chunk.Err != nil {
			a.streamChan = nil
			a.status = theme.StatusError
			a.systemMessage("Error: " + msg.chunk.Err.Error())
			return a, nil
		}
		switch {
		case msg.chunk.Confirmation != nil:
			// The run pauses itself right after this — nothing more will
			// arrive on the channel until resolveConfirmation answers it,
			// so the next readStreamChunk below just cleanly observes it
			// closing rather than looping forever.
			a.insertConfirmMessage(msg.chunk.Confirmation)
		case msg.chunk.ToolCall != nil:
			a.upsertToolMessage(msg.chunk.ToolCall.ID, msg.chunk.ToolCall.Name, msg.chunk.ToolCall.Args, toolStatusRunning, false)
		case msg.chunk.ToolResult != nil:
			a.upsertToolMessage(msg.chunk.ToolResult.ID, msg.chunk.ToolResult.Name, nil, summarizeResult(msg.chunk.ToolResult.Result), false)
		case msg.chunk.Usage != nil:
			a.accumulateUsage(msg.chunk.Usage)
		case msg.chunk.FinishReason != "":
			a.turnFinishReason = msg.chunk.FinishReason
		default:
			a.messages[a.streamingMsgIndex].Content += msg.chunk.Text
		}
		a.followTranscript()
		return a, readStreamChunk(a.streamChan)

	case keySetMsg:
		if msg.err != nil {
			a.status = theme.StatusError
			a.systemMessage("Could not connect with that key: " + msg.err.Error())
			return a, nil
		}
		a.backend = msg.backend
		a.systemMessage("API key set.")

		if a.pendingMessage != "" {
			text := a.pendingMessage
			a.pendingMessage = ""
			return a, a.dispatchToBackend(text)
		}
		a.status = theme.StatusIdle
		return a, nil
	}

	if a.paletteKind == paletteKeyInput {
		var cmd tea.Cmd
		a.keyInput, cmd = a.keyInput.Update(msg)
		return a, cmd
	}

	if a.paletteKind != paletteNone {
		var cmd tea.Cmd
		a.paletteList, cmd = a.paletteList.Update(msg)
		return a, cmd
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	cmds = append(cmds, cmd)
	a.viewport, cmd = a.viewport.Update(msg)
	cmds = append(cmds, cmd)
	a.layout()
	return a, tea.Batch(cmds...)
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return a, tea.Quit

	case a.paletteKind == paletteKeyInput:
		return a.handleKeyInputKey(msg)

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
func (a *App) handleSuggestKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		a.runCommand(name)
		a.layout()
		return a, nil
	}

	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	a.layout()
	return a, cmd
}

func (a *App) handlePaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Escape):
		a.cancelMenu()
		return a, nil

	case key.Matches(msg, keys.Send):
		if item, ok := a.paletteList.SelectedItem().(paletteItem); ok {
			a.confirmMenuSelection(item.id)
		}
		a.closeMenu()
		return a, nil
	}

	var cmd tea.Cmd
	a.paletteList, cmd = a.paletteList.Update(msg)

	// Live-preview: whatever's highlighted in the /theme menu is applied
	// immediately, not just on confirm, so navigating repaints the whole
	// app (this popup included) with the candidate theme.
	if a.paletteKind == paletteTheme {
		if item, ok := a.paletteList.SelectedItem().(paletteItem); ok {
			a.previewTheme(item.id)
		}
	}

	return a, cmd
}

// handleSend runs on Enter outside a popup menu: a leading "/" makes the
// input a command instead of a chat message.
func (a *App) handleSend() tea.Cmd {
	text := strings.TrimSpace(a.input.Value())
	if text == "" {
		return nil
	}

	if name, ok := strings.CutPrefix(text, "/"); ok {
		a.input.SetValue("")
		a.runCommand(name)
		a.layout()
		return nil
	}

	return a.sendMessage(text)
}

func (a *App) sendMessage(text string) tea.Cmd {
	a.messages = append(a.messages, ChatMessage{Role: RoleUser, Content: text, At: time.Now()})
	a.lastPromptText = text
	a.input.SetValue("")
	a.layout()
	// Sending is a deliberate action, unlike the steady trickle of
	// keystroke-driven refreshes elsewhere — always follow it to the
	// bottom even if you'd scrolled up to read history first.
	a.viewport.GotoBottom()

	if a.backend == nil {
		// Held rather than dropped or errored: the /key popup opens right
		// here, and the keySetMsg handler in Update sends this same text
		// the moment a key is successfully set.
		a.pendingMessage = text
		a.openKeyInput()
		return nil
	}

	return a.dispatchToBackend(text)
}

// dispatchToBackend sends text to the current backend, streamed or in one
// shot depending on streamReplies, and returns the tea.Cmd that kicks
// that off. Shared by sendMessage's normal path and by the keySetMsg
// handler resuming a held message.
func (a *App) dispatchToBackend(text string) tea.Cmd {
	a.status = theme.StatusThinking
	a.turnUsage, a.turnFinishReason = nil, ""
	backend := a.backend

	if !a.streamReplies {
		return func() tea.Msg {
			reply, err := backend.Send(context.Background(), a.sessionID, text)
			if err != nil {
				return agentReplyMsg{err: err}
			}
			return agentReplyMsg{text: reply}
		}
	}

	return func() tea.Msg {
		ch, err := backend.Stream(context.Background(), a.sessionID, text)
		if err != nil {
			return agentReplyMsg{err: err}
		}
		return streamStartMsg{ch: ch}
	}
}

type agentReplyMsg struct {
	text string
	err  error
}

// streamStartMsg carries the channel a fresh Backend.Stream call is
// delivering chunks on.
type streamStartMsg struct {
	ch <-chan StreamChunk
}

// streamChunkMsg wraps one receive from that channel; ok is false once
// the channel's closed (the stream is over).
type streamChunkMsg struct {
	chunk StreamChunk
	ok    bool
}

// readStreamChunk blocks on one channel receive — the standard Bubble
// Tea pattern for draining a channel: each chunk re-arms this same Cmd
// for the next one (see the streamChunkMsg case in Update), so the
// program only ever has one outstanding read at a time.
func readStreamChunk(ch <-chan StreamChunk) tea.Cmd {
	return func() tea.Msg {
		chunk, ok := <-ch
		return streamChunkMsg{chunk: chunk, ok: ok}
	}
}

// accumulateUsage folds one model call's usage into the running total for
// the turn in progress — a turn can invoke the model more than once (e.g.
// once to decide on a tool call, again after the result comes back).
//
// Prompt is NOT summed across calls: each call's PromptTokenCount is
// already a cumulative snapshot of the entire conversation sent to the
// model up to that point (the whole point of a "prompt" is it includes
// everything before it), so a later call's prompt count already contains
// an earlier call's in full. Summing them double-counted that shared
// history — a turn with one intermediate tool call was reporting roughly
// (first call's full context) + (second call's full context, which
// already includes the first) instead of just the latter. Since context
// only grows within a turn, the last call's Prompt is the correct total;
// max is used rather than "just take the latest value" so this stays
// correct even if a chunk somehow arrived out of order.
//
// Output, in contrast, genuinely is new content each call — the first
// call's function-call tokens and the second call's final prose are both
// real generation cost — so that part is correctly summed. Total is
// recomputed from the corrected Prompt/Output rather than summed from
// each call's own Total field, which would carry the same double-count.
func (a *App) accumulateUsage(u *TokenUsage) {
	if a.turnUsage == nil {
		a.turnUsage = &TokenUsage{}
	}
	if u.Prompt > a.turnUsage.Prompt {
		a.turnUsage.Prompt = u.Prompt
	}
	a.turnUsage.Output += u.Output
	a.turnUsage.Total = a.turnUsage.Prompt + a.turnUsage.Output
}

// attachTurnUsage lands the turn's accumulated usage/finish-reason on the
// last RoleAgent message it produced, then clears the accumulator. Called
// once the turn is genuinely over (see the streamChunkMsg !ok case) —
// never mid-turn, since a tool call in progress would otherwise get an
// intermediate model call's numbers attached to the wrong bubble.
//
// If the model's very last call in the turn produced no closing prose,
// its placeholder was already dropped by dropEmptyStreamingPlaceholder by
// the time this runs, leaving no RoleAgent message left to attach to —
// rare, and not worth inventing a bubble just to hold a token count, so
// it's silently skipped rather than attached somewhere misleading.
func (a *App) attachTurnUsage() {
	usage, reason := a.turnUsage, a.turnFinishReason
	a.turnUsage, a.turnFinishReason = nil, ""
	if usage == nil && reason == "" {
		return
	}
	for i := len(a.messages) - 1; i >= 0; i-- {
		if a.messages[i].Role == RoleAgent {
			a.messages[i].Usage = usage
			a.messages[i].FinishReason = reason
			return
		}
	}
}

// upsertToolMessage tracks one tool invocation's entire lifecycle — call,
// optional approval, eventual result — as a single transcript entry found
// and updated by call ID rather than appended fresh each event. Without
// this, a call's confirmation request and its eventual result each used
// to print as their own separate message, so the same invocation showed
// up two or three times in a row as it progressed.
//
// On first sight of an ID a fresh entry is created, and — same as the old
// insertToolMessage — a fresh empty placeholder is opened after it for
// whatever prose the agent sends next, dropping the previous placeholder
// first if it never received any text (a tool call visually interrupts
// the streaming reply in progress, so that reply's text shouldn't keep
// appending into the same bubble as if nothing happened, but an
// untouched placeholder isn't worth preserving as a stray empty bubble).
// On every later sighting of the same ID, only that entry's status is
// touched — args aren't overwritten, since only the initial call event
// carries them; a confirmation or result event passes nil/reuses them.
func (a *App) upsertToolMessage(id, name string, args map[string]any, status string, pending bool) {
	if idx, ok := a.toolMsgIndex[id]; ok && idx < len(a.messages) {
		a.messages[idx].ToolStatus = status
		a.messages[idx].ToolPending = pending
		return
	}

	a.dropEmptyStreamingPlaceholder()
	a.messages = append(a.messages, ChatMessage{
		Role:        RoleTool,
		ToolName:    name,
		ToolArgs:    args,
		ToolStatus:  status,
		ToolPending: pending,
		At:          time.Now(),
	})
	if a.toolMsgIndex == nil {
		a.toolMsgIndex = map[string]int{}
	}
	a.toolMsgIndex[id] = len(a.messages) - 1

	a.messages = append(a.messages, ChatMessage{Role: RoleAgent, Content: "", At: time.Now()})
	a.streamingMsgIndex = len(a.messages) - 1
}

// dropEmptyStreamingPlaceholder removes the in-progress agent placeholder
// if it never received any text — called both when a tool event is about
// to interrupt it and when the stream ends outright (e.g. it closed right
// after a tool result with no closing remarks).
//
// The Role check is not optional: RoleTool messages legitimately have an
// empty Content too (they carry their data in other fields), so checking
// Content alone would delete the very message just inserted in front of
// this one — which is exactly what was happening to every confirmation
// entry, since a confirmation pauses the stream immediately after, and
// that pause's cleanup ran this same check without ever re-pointing
// streamingMsgIndex away from it first.
func (a *App) dropEmptyStreamingPlaceholder() {
	if a.streamingMsgIndex < len(a.messages) &&
		a.messages[a.streamingMsgIndex].Role == RoleAgent &&
		a.messages[a.streamingMsgIndex].Content == "" {
		a.messages = append(a.messages[:a.streamingMsgIndex], a.messages[a.streamingMsgIndex+1:]...)
	}
}

func (a *App) applyTheme() {
	a.styles = a.themeMgr.Styles()
	applyInputStyles(&a.input, a.styles)
	if a.paletteKind != paletteNone {
		restylePalette(&a.paletteList, a.styles)
	}
	a.refreshTranscript()
}

// refreshTranscript re-renders the transcript into the viewport at
// whatever scroll position the viewport is already at — it never moves
// YOffset itself. This is the one layout() calls on nearly every
// keystroke, resize, and cursor blink, so it has to leave scrolling
// alone; see followTranscript for the variant that's allowed to move it.
func (a *App) refreshTranscript() {
	content, userMsgLines := renderTranscript(a.styles, a.bootInfo, a.messages, a.viewport.Width, a.highlightUser)
	a.viewport.SetContent(content)
	a.userMsgLines = userMsgLines
}

// followTranscript is refreshTranscript's counterpart for the call sites
// that just appended genuinely new content (a reply, a streamed chunk, a
// system event). Only here — not in the generic keystroke/resize refresh
// above — do we ask "was the user following the conversation?" and, if
// so, keep following, all the way to the true bottom, same as always.
//
// Keeping the last prompt visible during an oversized response is handled
// separately, in View() — as a sticky overlay pinned over whatever's
// scrolled beneath it, not by capping where auto-follow can scroll to.
func (a *App) followTranscript() {
	wasAtBottom := a.viewport.AtBottom()
	a.refreshTranscript()
	if wasAtBottom {
		a.viewport.GotoBottom()
	}
}

// stickyPromptOverlay returns the pinned "you: ..." strip to composite
// over the top of the viewport in View(), or "" if there's nothing to
// pin — either no prompt's been sent yet, or the last one is already
// visible at (or below) the current scroll position, in which case
// overlaying it too would just draw a duplicate on top of itself.
func (a *App) stickyPromptOverlay() string {
	n := len(a.userMsgLines)
	if n == 0 || a.lastPromptText == "" {
		return ""
	}
	if a.viewport.YOffset <= a.userMsgLines[n-1] {
		return ""
	}
	return renderStickyPrompt(a.styles, a.lastPromptText, a.viewport.Width)
}

// jumpToPrevPrompt / jumpToNextPrompt back PgUp/PgDn: rather than
// scrolling a fixed page height, they jump straight to the start of the
// nearest earlier/later user message. Falls back to a plain page
// scroll when there's no prompt in that direction — the top of the
// conversation still has the boot banner above the first one, and the
// bottom has nothing past the last one.
func (a *App) jumpToPrevPrompt() {
	for i := len(a.userMsgLines) - 1; i >= 0; i-- {
		if a.userMsgLines[i] < a.viewport.YOffset {
			a.viewport.SetYOffset(a.userMsgLines[i])
			return
		}
	}
	a.viewport.PageUp()
}

func (a *App) jumpToNextPrompt() {
	for _, line := range a.userMsgLines {
		if line > a.viewport.YOffset {
			a.viewport.SetYOffset(line)
			return
		}
	}
	a.viewport.PageDown()
}

// layout recomputes every child component's size from the current terminal
// dimensions and the input box's current wrap height. Called on resize and
// after every input edit, since typing can grow or shrink the input box.
func (a *App) layout() {
	if a.width == 0 {
		return
	}

	a.input.SetWidth(a.width - 4) // border + padding on each side
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
	suggestHeight := 0
	if len(a.suggestMatches) > 0 {
		suggestHeight = len(a.suggestMatches) + 2 // border top/bottom
	}
	vpHeight := max(a.height-topBarHeight-suggestHeight-inputBoxHeight-footerHeight, 0)

	if a.viewport.Width == 0 {
		a.viewport = viewport.New(a.width, vpHeight)
		a.viewport.MouseWheelDelta = 2 // a bit faster than 1, still finer than the 3-line default
	} else {
		a.viewport.Width = a.width
		a.viewport.Height = vpHeight
	}

	switch a.paletteKind {
	case paletteKeyInput:
		a.keyInput.Width = a.keyInputWidth() - 4
	case paletteNone:
		// nothing to resize
	default:
		a.paletteList.SetSize(a.paletteWidth(), max(a.paletteHeight()-paletteTitleHeight, 0))
		restylePalette(&a.paletteList, a.styles) // re-sync the title's baked-in width to the new size
	}
	a.refreshTranscript()
}

func (a *App) paletteWidth() int  { return min(a.width-8, 50) }
func (a *App) paletteHeight() int { return min(a.height-8, 12) }

func (a *App) View() string {
	if !a.ready {
		return ""
	}

	topBar := renderTopBar(a.styles, a.width, a.agentName, a.status, a.sessionID)
	body := a.viewport.View()
	if sticky := a.stickyPromptOverlay(); sticky != "" {
		body = overlay(body, sticky, 0, 0, a.viewport.Width)
	}
	inputBar := renderInputBar(a.styles, a.input, a.width, a.inputLines, true)
	footer := a.styles.Help.Render(a.help.View(keys))

	parts := []string{topBar, body}
	if len(a.suggestMatches) > 0 {
		parts = append(parts, renderSuggestions(a.styles, a.suggestMatches, a.suggestIndex, a.width))
	}
	parts = append(parts, inputBar, footer)

	frame := lipgloss.JoinVertical(lipgloss.Left, parts...)

	// Popup menus float over the chat instead of replacing it, both so
	// context isn't lost and so /theme can preview its highlighted theme
	// against the real screen behind it.
	switch a.paletteKind {
	case paletteKeyInput:
		return renderKeyInputOverlay(frame, a.styles, a.keyInput, a.width, a.height)
	case paletteNone:
		return frame
	default:
		return renderPaletteOverlay(frame, a.styles, a.paletteTitle, a.paletteList, a.width, a.height)
	}
}
