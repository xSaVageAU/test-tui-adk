// Package ui is the "shell" of the TUI: the root Bubble Tea model plus the
// components it composes (header, chat viewport, input bar, command
// palette). Layout and behavior live here; color/style vocabulary lives in
// internal/theme. Swapping a theme should never require touching this file.
package ui

import (
	"context"
	"strings"
	"time"

	"tui-testing/internal/settings"
	"tui-testing/internal/theme"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/stopwatch"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	bootInfo   BootInfo       // seeded at construction, kept live afterward; see bootart.go

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

	status        theme.StatusKind
	messages      []ChatMessage
	highlightUser bool // experimental: backdrop highlight behind user messages
	streamReplies bool // token-by-token replies via Backend.Stream instead of Send
	verboseTools  bool // false shows a one-line lean summary per tool call/result; see chat.go's formatToolArgs/formatToolResult
	showReasoning bool // false hides ChatMessage.ReasoningText but leaves the "thinking/thought for Xs" badge alone; see settings.UISettings.HideReasoningText for why this field's polarity is flipped at the persistence boundary
	// reasoning is true whenever the model is actively sending reasoning/
	// thinking chunks (StreamChunk.Reasoning) rather than its real reply.
	// Cleared the moment anything else arrives (real text, a tool call,
	// the stream ending) since those all mean the reasoning phase is
	// over — see startReasoning/endReasoning. The reasoning text itself
	// is received but not rendered anywhere yet — this (plus
	// ChatMessage.ReasoningActive/ReasoningDuration and reasoningStart/
	// stopwatch below) is deliberately just the detection/timing half of
	// a "show reasoning" feature, not the display-the-actual-text half.
	//
	// reasoningStart is the actual wall-clock source of truth for how
	// long a burst took (time.Since(reasoningStart) both live and at the
	// end) — real reasoning bursts turned out to often finish in well
	// under a second, so a value quantized to stopwatch's own tick
	// interval was never precise enough to show anything but "0s".
	// stopwatch is kept purely as a periodic wake-up source driving live
	// re-renders while active (its own Elapsed() isn't read) — a fresh
	// Model per burst, since its ticks carry a per-instance ID, so a
	// stray tick from a previous burst is safely ignored if it arrives
	// after a new one has already started. Ticks flow through Update's
	// stopwatch.TickMsg/StartStopMsg cases.
	reasoning      bool
	reasoningStart time.Time
	stopwatch      stopwatch.Model
	lastPromptText string // most recent user message; backs the sticky-prompt overlay in View()

	hitlMode            hitlMode       // how a pending tool approval is presented — see hitl.go
	permissionMode      permissionMode // whether a confirmation-gated tool call asks at all — see permissions.go
	pendingConfirmation *pendingConfirmation

	// In-flight stream state, set by streamStartMsg and cleared once the
	// channel closes or errors — see dispatchToBackend and the
	// streamChunkMsg handler in Update.
	streamChan        <-chan StreamChunk
	streamingMsgIndex int
	toolMsgIndex      map[string]int // tool call ID -> index into messages; see upsertToolMessage

	// contextWindow is the current model's max input tokens (0 if
	// unknown), set from Backend.ContextWindow on every successful
	// (re)connect — see the keySetMsg handler. contextUsed is the most
	// recently known prompt token count — i.e. accumulateUsage's
	// turnUsage.Prompt, mirrored here the moment it updates rather than
	// only once a turn finishes, so the top bar's context-usage
	// indicator (see header.go's renderContextBar) tracks live during a
	// multi-call turn instead of jumping only at the end. Persists
	// across turns (a later turn's prompt only grows on top of history,
	// same reasoning as turnUsage.Prompt itself) until resetTranscriptState
	// zeroes it for a genuinely new/switched session.
	contextWindow int
	contextUsed   int

	// turnUsage/turnFinishReason accumulate across every model call one
	// logical turn makes — including across a HITL pause/resume, which
	// closes and reopens the stream channel but is still the same turn —
	// until attachTurnFinishReason clears them at the turn's end (see
	// dispatchToBackend, where these reset at the turn's start).
	// turnUsage.Prompt is also mirrored live into contextUsed above as it
	// updates; turnFinishReason lands on the turn's final agent message.
	turnUsage        *TokenUsage
	turnFinishReason string

	viewport     viewport.Model
	userMsgLines []int // line offset of each user message's block; see renderTranscript
	input        textarea.Model
	inputLines   int // current wrapped height of the input box, [minInputLines, maxInputLines]

	paletteKind      paletteKind
	paletteTitle     string
	paletteList      list.Model
	keyInput         textinput.Model // backs paletteTextInput; see keyinput.go
	themeMenuOrigin  string          // theme active before /theme opened, for Esc to revert to
	loaderMenuOrigin string          // working-anim variant active before /loader opened, for Esc to revert to

	// workingAnim is the "agent is working" animation shown above the
	// input box — see workinganim.go. workingAnimActive tracks whether
	// its tick loop is currently running, so dispatchToBackend and
	// resolveConfirmation (the two moments a turn starts) can both call
	// startWorkingAnim unconditionally without risking two overlapping
	// tick chains.
	workingAnim       workingAnimState
	workingAnimActive bool
	// workingLabel is Orbit's center text (other variants ignore it) —
	// "thinking" for most of a turn, "using <tool>" while a tool call is
	// actually in flight, so it reflects the turn's real phase instead
	// of a fixed word. Set at turn start (dispatchToBackend,
	// resolveConfirmation) and on every ToolCall/ToolResult chunk (see
	// Update).
	workingLabel string

	// textPopupKind/textPopupLabel/textPopupProvider back paletteTextInput
	// (see keyinput.go) — one popup shared by /key's masked API-key field
	// and /agents' unmasked model field, distinguished by kind so Enter
	// knows which submit* to call.
	textPopupKind     textPopupKind
	textPopupLabel    string // this popup's title, e.g. "Set Gemini API key" or "Set model for research"
	textPopupProvider string // textPopupAPIKey: which provider the key is for

	// agentMenuTarget/agentMenuSummaries back /agents' multi-step flow:
	// agentMenuSummaries is snapshotted once when the top-level list
	// opens (so a later step can show an agent's current provider/model
	// without a redundant listAgents call), and agentMenuTarget is which
	// entry's menu id ("root", or a sub-agent's ID) the provider-list and
	// model-input steps are currently editing. See agentsmenu.go.
	agentMenuTarget    string
	agentMenuSummaries []AgentConfigSummary

	// listAgents/setAgentProvider/setAgentModel back /agents — nil
	// disables the command (mirrors newBackend's nil-disables-/key
	// convention). See agentsmenu.go.
	listAgents       func() ([]AgentConfigSummary, error)
	setAgentProvider func(id, provider string) error
	setAgentModel    func(id, modelName string) error

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
	// Specialists is the name of every sub-agent the backend loaded at
	// startup (empty if none), shown in the boot banner. Meaningless
	// without a Backend — leave nil when Backend is nil.
	Specialists []string
	// ContextWindow is the backend's model's max input tokens (0 if
	// unknown), backing the top bar's context-usage indicator from the
	// very first frame. Meaningless without a Backend — leave 0 when
	// Backend is nil.
	ContextWindow int
	// ListAgents/SetAgentProvider/SetAgentModel back /agents — reading
	// and editing config files directly, independent of whether a
	// Backend connection currently exists (unlike NewBackend, these work
	// even with Backend nil — fixing a bad provider/model is exactly
	// what you'd want /agents for in that state). Pass nil for all three
	// to disable /agents entirely, same as leaving NewBackend nil
	// disables /key.
	ListAgents       func() ([]AgentConfigSummary, error)
	SetAgentProvider func(id, provider string) error
	SetAgentModel    func(id, modelName string) error
}

// NewApp constructs the app with the default (first-registered) theme
// active.
func NewApp(cfg AppConfig) *App {
	mgr := theme.NewManager(theme.Load()...)
	styles := mgr.Styles()
	uiSettings := settings.Load().UI

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
			Model:       cfg.ModelName,
			Theme:       mgr.Current().Name,
			Specialists: cfg.Specialists,
		},
		contextWindow:    cfg.ContextWindow,
		status:           theme.StatusIdle,
		highlightUser:    uiSettings.HighlightUser,
		streamReplies:    uiSettings.StreamReplies,
		verboseTools:     uiSettings.VerboseTools,
		showReasoning:    !uiSettings.HideReasoningText,
		hitlMode:         parseHITLMode(uiSettings.HITLMode),
		permissionMode:   parsePermissionMode(uiSettings.PermissionMode),
		inputLines:       minInputLines,
		messages:         messages,
		input:            newInput(styles),
		listAgents:       cfg.ListAgents,
		setAgentProvider: cfg.SetAgentProvider,
		setAgentModel:    cfg.SetAgentModel,
		workingAnim:      newWorkingAnimState(parseWorkingAnimVariant(uiSettings.WorkingAnim)),
		workingLabel:     "thinking",
	}
	return a
}

func (a *App) Init() tea.Cmd {
	return a.input.Focus()
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		widthChanged := a.ready && msg.Width != a.width
		a.width, a.height = msg.Width, msg.Height
		if !a.ready {
			// Nothing to preserve yet — this is the very first size the
			// program ever receives.
			a.layout()
			a.ready = true
			return a, nil
		}
		if widthChanged {
			a.resizeAndPreserveScroll()
		} else {
			// Height-only change: the transcript doesn't reflow (wrapping
			// only depends on width), so the existing YOffset already
			// points at exactly the same content it did before — anchoring
			// it to a message boundary here would be actively wrong,
			// snapping away from the user's real mid-message scroll
			// position even though nothing needed to move.
			a.layout()
		}
		return a, nil

	case tea.KeyPressMsg:
		return a.handleKey(msg)

	case workingAnimTickMsg:
		a.workingAnim.advance()
		if !a.workingAnimShouldRun() {
			a.workingAnimActive = false
			return a, nil
		}
		return a, workingAnimTick()

	case stopwatch.TickMsg:
		var cmd tea.Cmd
		a.stopwatch, cmd = a.stopwatch.Update(msg)
		// Reject a stray tick from a burst that's already ended (its own
		// ID won't match a.stopwatch's current one, but a.reasoning being
		// false is the simpler check — either way nothing should update).
		// The duration itself comes from reasoningStart, not the
		// stopwatch's own Elapsed() — see App.reasoning's doc comment.
		if a.reasoning && a.streamingMsgIndex < len(a.messages) {
			a.messages[a.streamingMsgIndex].ReasoningDuration = time.Since(a.reasoningStart)
			a.refreshTranscript()
		}
		return a, cmd

	case stopwatch.StartStopMsg:
		var cmd tea.Cmd
		a.stopwatch, cmd = a.stopwatch.Update(msg)
		return a, cmd

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
			endCmd := a.endReasoning()
			a.dropEmptyStreamingPlaceholder()
			// pendingConfirmation set means this channel close is a HITL
			// pause, not the turn actually ending — resolveConfirmation
			// reopens a fresh channel for the same turn, so the running
			// totals need to survive until then rather than land now.
			if a.pendingConfirmation == nil {
				a.attachTurnFinishReason()
			}
			a.followTranscript()
			return a, endCmd
		}
		if msg.chunk.Err != nil {
			a.streamChan = nil
			a.status = theme.StatusError
			endCmd := a.endReasoning()
			a.systemMessage("Error: " + msg.chunk.Err.Error())
			return a, endCmd
		}
		var cmd tea.Cmd
		switch {
		case msg.chunk.Confirmation != nil:
			// The run pauses itself right after this — nothing more will
			// arrive on the channel until resolveConfirmation answers it,
			// so the next readStreamChunk below just cleanly observes it
			// closing rather than looping forever.
			a.insertConfirmMessage(msg.chunk.Confirmation)
		case msg.chunk.ToolCall != nil:
			cmd = a.endReasoning()
			a.upsertToolMessage(msg.chunk.ToolCall.ID, msg.chunk.ToolCall.Name, msg.chunk.ToolCall.Args, toolStatusRunning, false)
			a.workingLabel = "using " + msg.chunk.ToolCall.Name
		case msg.chunk.ToolResult != nil:
			a.completeToolMessage(msg.chunk.ToolResult.ID, msg.chunk.ToolResult.Name, msg.chunk.ToolResult.Result)
			a.workingLabel = "thinking"
		case msg.chunk.Usage != nil:
			a.accumulateUsage(msg.chunk.Usage)
		case msg.chunk.FinishReason != "":
			a.turnFinishReason = msg.chunk.FinishReason
		case msg.chunk.Reasoning != "":
			cmd = a.startReasoning()
			if a.streamingMsgIndex < len(a.messages) {
				a.messages[a.streamingMsgIndex].ReasoningText += msg.chunk.Reasoning
			}
		default:
			cmd = a.endReasoning()
			a.messages[a.streamingMsgIndex].Content += msg.chunk.Text
		}
		a.followTranscript()
		return a, tea.Batch(cmd, readStreamChunk(a.streamChan))

	case keySetMsg:
		if msg.err != nil {
			a.status = theme.StatusError
			prefix := msg.failPrefix
			if prefix == "" {
				prefix = "Could not connect"
			}
			a.systemMessage(prefix + ": " + msg.err.Error())
			return a, nil
		}
		a.backend = msg.backend
		a.bootInfo.Model = msg.backend.ModelName()
		a.bootInfo.Specialists = msg.backend.Specialists()
		a.contextWindow = msg.backend.ContextWindow()
		if msg.successMsg != "" {
			a.systemMessage(msg.successMsg)
		} else {
			a.systemMessage("Connected.")
		}

		if a.pendingMessage != "" {
			text := a.pendingMessage
			a.pendingMessage = ""
			return a, a.dispatchToBackend(text)
		}
		a.status = theme.StatusIdle
		return a, nil

	case sessionsLoadedMsg:
		if msg.err != nil {
			a.systemMessage("Could not list sessions: " + msg.err.Error())
			return a, nil
		}
		if len(msg.sessions) == 0 {
			a.systemMessage("No past sessions yet.")
			return a, nil
		}
		items := make([]paletteItem, len(msg.sessions))
		for i, s := range msg.sessions {
			items[i] = paletteItem{id: s.ID, title: shortSessionID(s.ID), desc: relativeTime(s.UpdatedAt)}
		}
		a.openMenu(paletteSessions, "Switch session", items)
		return a, nil
	}

	if a.paletteKind == paletteTextInput {
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

func (a *App) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return a, tea.Quit

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
		a.cancelMenu()
		return a, nil

	case key.Matches(msg, keys.Send):
		item, ok := a.paletteList.SelectedItem().(paletteItem)
		if !ok {
			a.closeMenu()
			return a, nil
		}
		closeAfter, cmd := a.confirmMenuSelection(item.id)
		if closeAfter {
			a.closeMenu()
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

// handleSend runs on Enter outside a popup menu: a leading "/" makes the
// input a command instead of a chat message.
func (a *App) handleSend() tea.Cmd {
	text := strings.TrimSpace(a.input.Value())
	if text == "" {
		return nil
	}

	if name, ok := strings.CutPrefix(text, "/"); ok {
		a.input.SetValue("")
		cmd := a.runCommand(name)
		a.layout()
		return cmd
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
		a.openKeyProviderMenu()
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
	a.workingLabel = "thinking"
	a.reasoning = false
	a.turnUsage, a.turnFinishReason = nil, ""
	backend := a.backend
	animCmd := a.startWorkingAnim()

	if !a.streamReplies {
		return tea.Batch(animCmd, func() tea.Msg {
			reply, err := backend.Send(context.Background(), a.sessionID, text)
			if err != nil {
				return agentReplyMsg{err: err}
			}
			return agentReplyMsg{text: reply}
		})
	}

	return tea.Batch(animCmd, func() tea.Msg {
		ch, err := backend.Stream(context.Background(), a.sessionID, text)
		if err != nil {
			return agentReplyMsg{err: err}
		}
		return streamStartMsg{ch: ch}
	})
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
	a.contextUsed = a.turnUsage.Prompt
}

// reasoningTickInterval drives both the live "thinking Xms/Xs" re-render
// cadence and, indirectly, the finest resolution a burst that ends
// between ticks could show live (the final frozen duration is always
// precise regardless — see endReasoning — this only affects what's
// visible while still counting up). 100ms rather than stopwatch's own
// 1s default: real reasoning bursts turned out to often finish well
// under a second, and at a 1s interval the very first tick usually
// never even fired before reasoning ended, leaving the badge stuck at
// "0s" the whole time and the final duration landing on exactly zero.
const reasoningTickInterval = 100 * time.Millisecond

// startReasoning marks the current streaming message as actively
// reasoning, records when it started, and starts a stopwatch purely as
// a periodic wake-up source for live re-renders (see App.reasoning's
// doc comment for why the displayed duration itself comes from
// reasoningStart, not the stopwatch's own Elapsed()). Idempotent (a
// no-op if already reasoning), since every reasoning chunk in a burst
// hits this same case, not just the first.
func (a *App) startReasoning() tea.Cmd {
	if a.reasoning {
		return nil
	}
	a.reasoning = true
	a.reasoningStart = time.Now()
	a.stopwatch = stopwatch.New(stopwatch.WithInterval(reasoningTickInterval))
	if a.streamingMsgIndex < len(a.messages) {
		a.messages[a.streamingMsgIndex].ReasoningActive = true
		a.messages[a.streamingMsgIndex].ReasoningDuration = 0
	}
	return a.stopwatch.Start()
}

// endReasoning freezes the current streaming message's reasoning
// duration at the precise wall-clock time elapsed since startReasoning
// and stops the stopwatch — called from every place a turn's reasoning
// phase can end: real text arriving, a tool call, the stream closing,
// or an error. Idempotent (a no-op, returning nil, if reasoning wasn't
// active) so every one of those call sites can call it unconditionally.
func (a *App) endReasoning() tea.Cmd {
	if !a.reasoning {
		return nil
	}
	a.reasoning = false
	if a.streamingMsgIndex < len(a.messages) {
		a.messages[a.streamingMsgIndex].ReasoningActive = false
		a.messages[a.streamingMsgIndex].ReasoningDuration = time.Since(a.reasoningStart)
	}
	return a.stopwatch.Stop()
}

// attachTurnFinishReason lands the turn's finish reason (if any) on the
// last RoleAgent message it produced, then clears both accumulators.
// Called once the turn is genuinely over (see the streamChunkMsg !ok
// case) — never mid-turn, since a tool call in progress would otherwise
// get an intermediate model call's reason attached to the wrong bubble.
//
// turnUsage itself isn't attached anywhere here — it's a running
// accumulator only, already reflected live in a.contextUsed as it
// updates (see accumulateUsage); nothing renders it per-message.
//
// If the model's very last call in the turn produced no closing prose,
// its placeholder was already dropped by dropEmptyStreamingPlaceholder by
// the time this runs, leaving no RoleAgent message left to attach to —
// rare, and not worth inventing a bubble just to hold a finish reason,
// so it's silently skipped rather than attached somewhere misleading.
func (a *App) attachTurnFinishReason() {
	reason := a.turnFinishReason
	a.turnUsage, a.turnFinishReason = nil, ""
	if reason == "" {
		return
	}
	for i := len(a.messages) - 1; i >= 0; i-- {
		if a.messages[i].Role == RoleAgent {
			a.messages[i].FinishReason = reason
			return
		}
	}
}

// upsertToolMessage tracks a tool call's opening state — running, then
// (if applicable) awaiting approval or approved/denied — as a single
// transcript entry found and updated by call ID rather than appended
// fresh each event; see completeToolMessage for how the final result
// lands, in a distinct field so it can be reformatted live if
// verboseTools changes later instead of baking in a summary once at
// event time. Without upserting-by-ID, a call's confirmation request and
// its eventual result each used to print as their own separate message,
// so the same invocation showed up two or three times in a row as it
// progressed.
//
// On every later sighting of the same ID, only status/pending are
// touched — args aren't overwritten, since only the initial call event
// carries them; a confirmation event reuses them via nil.
func (a *App) upsertToolMessage(id, name string, args map[string]any, status string, pending bool) {
	if idx, ok := a.toolMsgIndex[id]; ok && idx < len(a.messages) {
		a.messages[idx].ToolStatus = status
		a.messages[idx].ToolPending = pending
		return
	}
	a.newToolMessage(id, ChatMessage{ToolName: name, ToolArgs: args, ToolStatus: status, ToolPending: pending, At: time.Now()})
}

// completeToolMessage records a finished call's raw result. Kept
// separate from upsertToolMessage (rather than another status string)
// specifically so renderTool can reformat it live if verboseTools is
// toggled after the fact — see ChatMessage's doc comment. A nil result
// is normalized to an empty map so a genuinely-empty result stays
// distinguishable from "no result yet" (ChatMessage.ToolResult == nil),
// which is what renderTool actually checks.
func (a *App) completeToolMessage(id, name string, result map[string]any) {
	if result == nil {
		result = map[string]any{}
	}
	if idx, ok := a.toolMsgIndex[id]; ok && idx < len(a.messages) {
		a.messages[idx].ToolResult = result
		a.messages[idx].ToolStatus = ""
		a.messages[idx].ToolPending = false
		return
	}
	// Shouldn't happen — a result always follows its own call, whose
	// ToolCall event already created this entry — but mirrors
	// upsertToolMessage's fallback rather than silently dropping a
	// result with nowhere to attach to.
	a.newToolMessage(id, ChatMessage{ToolName: name, ToolResult: result, At: time.Now()})
}

// newToolMessage appends a fresh RoleTool entry plus the trailing empty
// agent placeholder every tool entry gets — same as the old
// insertToolMessage — a fresh empty placeholder is opened after it for
// whatever prose the agent sends next, dropping the previous placeholder
// first if it never received any text (a tool call visually interrupts
// the streaming reply in progress, so that reply's text shouldn't keep
// appending into the same bubble as if nothing happened, but an
// untouched placeholder isn't worth preserving as a stray empty bubble).
// Records id -> index in toolMsgIndex so later events for the same call
// find their way back here instead of appending a duplicate entry.
func (a *App) newToolMessage(id string, msg ChatMessage) {
	msg.Role = RoleTool
	a.dropEmptyStreamingPlaceholder()
	a.messages = append(a.messages, msg)
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
	// Keeps the boot banner's "theme" row in sync too — every caller
	// (a real /theme confirm, a live preview while arrowing through the
	// menu, cancelMenu reverting a preview) goes through here, so this
	// is the one place that needs to know about BootInfo.Theme at all.
	a.bootInfo.Theme = a.themeMgr.Current().Name
	a.viewport.Style = a.styles.Viewport
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
	content, userMsgLines := renderTranscript(a.styles, a.bootInfo, a.messages, a.viewport.Width(), a.highlightUser, a.verboseTools, a.showReasoning)
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
	if a.viewport.YOffset() <= a.userMsgLines[n-1] {
		return ""
	}
	return renderStickyPrompt(a.styles, a.lastPromptText, a.viewport.Width())
}

// jumpToPrevPrompt / jumpToNextPrompt back PgUp/PgDn: rather than
// scrolling a fixed page height, they jump straight to the start of the
// nearest earlier/later user message. Falls back to a plain page
// scroll when there's no prompt in that direction — the top of the
// conversation still has the boot banner above the first one, and the
// bottom has nothing past the last one.
func (a *App) jumpToPrevPrompt() {
	for i := len(a.userMsgLines) - 1; i >= 0; i-- {
		if a.userMsgLines[i] < a.viewport.YOffset() {
			a.viewport.SetYOffset(a.userMsgLines[i])
			return
		}
	}
	a.viewport.PageUp()
}

func (a *App) jumpToNextPrompt() {
	for _, line := range a.userMsgLines {
		if line > a.viewport.YOffset() {
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
	suggestHeight := 0
	if len(a.suggestMatches) > 0 {
		suggestHeight = len(a.suggestMatches) + 2 // border top/bottom
	}
	vpHeight := max(a.height-topBarHeight-workingAnimHeight-suggestHeight-inputBoxHeight-footerHeight, 0)

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

func (a *App) paletteWidth() int  { return min(a.width-8, 50) }
func (a *App) paletteHeight() int { return min(a.height-8, 12) }

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

func (a *App) View() tea.View {
	if !a.ready {
		return tea.NewView("")
	}

	width := a.renderWidth()
	topBar := renderTopBar(a.styles, width, a.sessionID, a.contextUsed, a.contextWindow)
	body := a.viewport.View()
	if sticky := a.stickyPromptOverlay(); sticky != "" {
		body = overlay(body, sticky, 0, 0, a.viewport.Width())
	}
	workingAnimBlock := blankWorkingAnim(a.styles.Theme, width)
	if a.workingAnimShouldRun() {
		workingAnimBlock = a.workingAnim.render(a.styles.Theme, width, a.workingLabel)
	}
	inputBar := renderInputBar(a.styles, a.input, width, a.inputLines, true)
	footer := renderHelpFooter(a.styles, a.permissionMode == permissionFullAuto, a.verboseTools, width)

	parts := []string{topBar, body}
	if len(a.suggestMatches) > 0 {
		parts = append(parts, renderSuggestions(a.styles, a.suggestMatches, a.suggestIndex, width))
	}
	parts = append(parts, workingAnimBlock, inputBar, footer)

	frame := lipgloss.JoinVertical(lipgloss.Left, parts...)

	// Popup menus float over the chat instead of replacing it, both so
	// context isn't lost and so /theme can preview its highlighted theme
	// against the real screen behind it.
	switch a.paletteKind {
	case paletteTextInput:
		frame = renderTextInputOverlay(frame, a.styles, a.textPopupLabel, a.keyInput, width, a.height)
	case paletteNone:
		// frame already holds the whole screen; nothing more to composite.
	default:
		frame = renderPaletteOverlay(frame, a.styles, a.paletteTitle, a.paletteList, width, a.height)
	}

	v := tea.NewView(frame)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	// The actual fix for this whole app's background-color problem: a
	// one-time OSC background-color escape the terminal itself then
	// treats as its default, so anything that resolves to "no explicit
	// color" (JoinVertical's own unstyled line-padding, textarea's
	// private internal viewport, ...) shows the theme's background
	// instead of the terminal's raw one. See the migration plan for the
	// full trace of why this needed a framework change rather than more
	// manual Background() patching.
	v.BackgroundColor = lipgloss.Color(a.styles.Theme.Background)
	return v
}
