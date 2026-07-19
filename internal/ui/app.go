// Package ui is the "shell" of the TUI: the root Bubble Tea model plus the
// components it composes (header, chat viewport, input bar, command
// palette). Layout and behavior live here; color/style vocabulary lives in
// internal/theme. Swapping a theme should never require touching this file.
//
// This file itself is deliberately just the model's spine — the App
// struct, its construction, and the three tea.Model methods (Init,
// Update, View). Everything Update dispatches into lives in its own
// file by concern: turn.go (sending/streaming/cancelling a turn),
// keyrouting.go (interpreting a keypress), transcript.go (message/
// scroll state), layout.go (sizing).
package ui

import (
	"context"
	"errors"
	"fmt"
	"time"

	"tui-testing/internal/settings"
	"tui-testing/internal/theme"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/stopwatch"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	// over — see turn.go's startReasoning/endReasoning. The reasoning
	// text itself is received but not rendered anywhere yet — this (plus
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
	// confirmQueue/confirmDecisions back a batch of confirmations that
	// arrived together (parallel tool calls all requiring approval in the
	// same turn — see gate.go's package doc comment on why those race).
	// confirmQueue holds ones that arrived but haven't been shown yet;
	// confirmDecisions holds answers for ones already shown, waiting for
	// the rest of the batch. See hitl.go's insertConfirmMessage/
	// resolveConfirmation for why the whole batch is sent back together
	// in one round trip rather than as they're each decided.
	confirmQueue     []*pendingConfirmation
	confirmDecisions []ConfirmationDecision

	// In-flight stream state, set by streamStartMsg and cleared once the
	// channel closes or errors — see turn.go's dispatchToBackend and the
	// streamChunkMsg handler below.
	streamChan        <-chan StreamChunk
	streamingMsgIndex int
	toolMsgIndex      map[string]int // tool call ID -> index into messages; see transcript.go's upsertToolMessage

	// turnCancel cancels the context backing whichever Send/Stream/
	// RespondToConfirmation call is currently in flight, if any — set at
	// the start of turn.go's dispatchToBackend and resolveConfirmation's
	// final batch send, cleared the moment that call's result lands
	// (success, error, or this same cancellation). ctrl+c calls it (see
	// keyrouting.go's handleKey and turn.go's stopTurn) to stop the agent
	// mid-turn instead of quitting the app; nil whenever nothing is
	// running, which is also how handleKey tells "the agent is working"
	// apart from "the main view."
	turnCancel context.CancelFunc

	// queuedMessages holds text sent while a turn was already running,
	// in send order — popQueuedMessage (turn.go) dispatches the next one
	// the moment the current turn genuinely ends (success, error, or a
	// stop), rather than firing a second concurrent turn. /interrupt
	// (also turn.go) replaces this outright instead of appending, since
	// redirecting means skipping whatever was already waiting, not
	// getting in line behind it.
	queuedMessages []string

	// reloadRequested is set when a reload_agents tool call completes
	// mid-turn (see backend.go's StreamChunk.ReloadRequested) and cleared
	// — with the actual reloadBackend() call batched in — at the same
	// "turn genuinely concluded" points popQueuedMessage runs at, never
	// immediately: reloadBackend() refuses outright while turnInProgress()
	// is true, which it still is the moment this flag gets set.
	reloadRequested bool

	// quitArmedAt is when ctrl+c was last pressed with nothing else for
	// it to do (no popup open, no turn running) — a second press within
	// quitConfirmWindow actually quits; any later press, or one while
	// something else intercepts ctrl+c first, is treated as a fresh
	// first press instead. See keyrouting.go's handleKey.
	quitArmedAt time.Time

	// contextWindow is the current model's max input tokens (0 if
	// unknown), set from Backend.ContextWindow on every successful
	// (re)connect — see the keySetMsg handler. contextUsed is the most
	// recently known prompt token count — i.e. turn.go's
	// accumulateUsage's turnUsage.Prompt, mirrored here the moment it
	// updates rather than only once a turn finishes, so the top bar's
	// context-usage indicator (see header.go's renderContextBar) tracks
	// live during a multi-call turn instead of jumping only at the end.
	// Persists across turns (a later turn's prompt only grows on top of
	// history, same reasoning as turnUsage.Prompt itself) until
	// resetTranscriptState zeroes it for a genuinely new/switched
	// session.
	contextWindow int
	contextUsed   int

	// turnUsage/turnFinishReason accumulate across every model call one
	// logical turn makes — including across a HITL pause/resume, which
	// closes and reopens the stream channel but is still the same turn —
	// until turn.go's attachTurnFinishReason clears them at the turn's
	// end (see dispatchToBackend, where these reset at the turn's
	// start). turnUsage.Prompt is also mirrored live into contextUsed
	// above as it updates; turnFinishReason lands on the turn's final
	// agent message.
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

	// notice/noticeExpiry/noticeTicking back the top bar's transient
	// status area (see notice.go and header.go's joinLeftCenterRight):
	// the current badge text, when it should disappear, and whether an
	// expiry tick is already in flight (so Update's wrapper schedules
	// exactly one timer per live notice rather than one per pass).
	notice        string
	noticeExpiry  time.Time
	noticeTicking bool

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

	// agentToolsAll/agentToolsCurrent/agentToolsChanged back /agents'
	// "Tools" step specifically — agentToolsAll is the full registry,
	// fetched once when that page opens; agentToolsCurrent is which of
	// those are enabled for agentMenuTarget, mutated (and persisted) as
	// the user toggles checkboxes; agentToolsChanged tracks whether the
	// backend needs reloading once the page closes, so N toggles cost
	// one reload instead of N. See agentsmenu.go's openAgentToolsMenu/
	// toggleAgentTool.
	agentToolsAll     []ToolSummary
	agentToolsCurrent map[string]bool
	agentToolsChanged bool

	// deleteSessionTarget is which session /sessions' DEL-key confirm
	// popup is about to delete, set by openDeleteSessionConfirm — "" means
	// "every session", set by openDeleteAllSessionsConfirm (ctrl+DEL)
	// instead of naming one. Safe as a sentinel since a real session ID is
	// always a non-empty UUID. See sessions.go.
	deleteSessionTarget string

	// listAgents/setAgentProvider/setAgentModel/setAgentTools/listTools
	// back /agents — nil disables the command (mirrors newBackend's
	// nil-disables-/key convention). See agentsmenu.go.
	listAgents       func() ([]AgentConfigSummary, error)
	setAgentProvider func(id, provider string) error
	setAgentModel    func(id, modelName string) error
	setAgentTools    func(id string, tools []string) error
	listTools        func() ([]ToolSummary, error)

	// targetType is the execution target the tools run against — "host"
	// or "ssh" (see settings.TargetSettings). configureTarget re-installs
	// the target from settings after targetType changes and returns a
	// description of the now-active target, or an error if (e.g.) the SSH
	// connection failed; nil disables the /settings target row, same
	// nil-disables convention as the closures above.
	targetType      string
	configureTarget func() (string, error)

	suggestMatches []commandSpec // live "/command" matches for the current input, if any
	suggestIndex   int

	// menuBack is a stack of "how to reopen the menu on screen right now"
	// closures, pushed whenever a menu opens a nested step (e.g. /agents'
	// detail page opening Provider/Model/Tools, or /settings opening a
	// numeric field) — see menustack.go's pushMenuBack/backOrClose. Esc
	// and a terminal selection both pop this before falling back to
	// actually leaving the popup, so stepping out of a nested menu goes
	// back one level instead of dropping all the way out to the chat.
	menuBack []func() tea.Cmd

	// popupWidth/popupHeight are /settings' "Popup width"/"Popup height"
	// overrides — 0 means unset (use popupWidthDefault/popupHeightDefault,
	// see layout.go's effectivePopupWidth/effectivePopupHeight). Every
	// popup modal (the command palette, /settings, /agents, and the /key
	// and /agents-model text fields) shares these two values — see
	// paletteWidth/paletteHeight/textPopupWidth.
	popupWidth  int
	popupHeight int

	// toolPreviewMaxLines is /settings' "Tool output preview lines"
	// override — 0 means unset (use toolPreviewMaxLinesDefault, see
	// commands.go's effectiveToolPreviewMaxLines). Caps how much of a
	// tool's content (read_file's result, write_file's written content)
	// verbose mode shows before truncating — see toolformat.go's
	// formatToolResult.
	toolPreviewMaxLines int
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
	// ListAgents/SetAgentProvider/SetAgentModel/SetAgentTools back
	// /agents — reading and editing config files directly, independent
	// of whether a Backend connection currently exists (unlike
	// NewBackend, these work even with Backend nil — fixing a bad
	// provider/model is exactly what you'd want /agents for in that
	// state). Pass nil for all four to disable /agents entirely, same as
	// leaving NewBackend nil disables /key.
	ListAgents       func() ([]AgentConfigSummary, error)
	SetAgentProvider func(id, provider string) error
	SetAgentModel    func(id, modelName string) error
	SetAgentTools    func(id string, tools []string) error
	// ListTools backs /agents' tools picker — every tool that could be
	// granted, independent of which agent (or none) is currently
	// selected. nil disables the "Tools" row entirely, same convention
	// as the four above.
	ListTools func() ([]ToolSummary, error)
	// ConfigureTarget re-installs the tool execution target (local host
	// or a remote SSH machine) from settings, returning a description of
	// the now-active target or an error establishing it — backs the
	// /settings "Tool execution target" row. nil disables that row.
	ConfigureTarget func() (string, error)
}

// normalizeTargetType maps a settings target type to the two the UI
// toggles between — anything that isn't "ssh" (including "", an older
// settings file) is "host", matching ConfigureTarget's own fail-safe.
func normalizeTargetType(s string) string {
	if s == settings.TargetSSH {
		return settings.TargetSSH
	}
	return settings.TargetHost
}

// NewApp constructs the app with the default (first-registered) theme
// active.
func NewApp(cfg AppConfig) *App {
	mgr := theme.NewManager(theme.Load()...)
	styles := mgr.Styles()
	loadedSettings := settings.Load()
	uiSettings := loadedSettings.UI
	agentSettings := loadedSettings.Agent

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
		contextWindow:       cfg.ContextWindow,
		status:              theme.StatusIdle,
		highlightUser:       uiSettings.HighlightUser,
		streamReplies:       uiSettings.StreamReplies,
		verboseTools:        uiSettings.VerboseTools,
		showReasoning:       !uiSettings.HideReasoningText,
		popupWidth:          uiSettings.PopupWidth,
		popupHeight:         uiSettings.PopupHeight,
		toolPreviewMaxLines: uiSettings.ToolPreviewMaxLines,
		hitlMode:            parseHITLMode(uiSettings.HITLMode),
		permissionMode:      parsePermissionMode(agentSettings.PermissionMode),
		inputLines:          minInputLines,
		messages:            messages,
		input:               newInput(styles),
		listAgents:          cfg.ListAgents,
		setAgentProvider:    cfg.SetAgentProvider,
		setAgentModel:       cfg.SetAgentModel,
		setAgentTools:       cfg.SetAgentTools,
		listTools:           cfg.ListTools,
		targetType:          normalizeTargetType(agentSettings.Target.Type),
		configureTarget:     cfg.ConfigureTarget,
		workingAnim:         newWorkingAnimState(parseWorkingAnimVariant(uiSettings.WorkingAnim)),
		workingLabel:        "thinking",
	}
	return a
}

func (a *App) Init() tea.Cmd {
	return a.input.Focus()
}

// Update wraps update (the real message handler, below) with the one
// piece of bookkeeping every code path shares: the top-bar notice's
// expiry timer. setNotice can't schedule that itself — it returns no
// tea.Cmd so plain void helpers can call it (see notice.go) — so the
// timer is started here whenever a pass ends with a live notice and no
// tick in flight. A notice replaced mid-flight just extends noticeExpiry;
// the stale tick then lands early, sees the deadline hasn't passed, and
// this wrapper reschedules for the remainder.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var model tea.Model = a
	if _, ok := msg.(noticeExpireMsg); ok {
		a.noticeTicking = false
		if !time.Now().Before(a.noticeExpiry) {
			a.notice = ""
		}
	} else {
		model, cmd = a.update(msg)
	}
	if a.notice != "" && !a.noticeTicking {
		a.noticeTicking = true
		cmd = tea.Batch(cmd, noticeExpireTick(time.Until(a.noticeExpiry)))
	}
	return model, cmd
}

func (a *App) update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		a.turnCancel = nil
		if msg.err != nil {
			a.status = theme.StatusIdle
			// concludeTurn before systemMessage: a queued message or a
			// pending reload both need the turn treated as genuinely over
			// before they act, and systemMessage's own followTranscript
			// call is what actually renders whatever that produces — the
			// other way around would leave it visually stale for a frame.
			cmd := a.concludeTurn()
			if errors.Is(msg.err, context.Canceled) {
				a.systemMessage("Stopped.")
			} else {
				a.status = theme.StatusError
				a.systemMessage("Error: " + msg.err.Error())
			}
			return a, cmd
		}
		a.messages = append(a.messages, ChatMessage{Role: RoleAgent, Content: msg.text, At: time.Now()})
		a.status = theme.StatusIdle
		cmd := a.concludeTurn()
		a.followTranscript()
		return a, cmd

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
			a.turnCancel = nil
			a.status = theme.StatusIdle
			endCmd := a.endReasoning()
			a.dropEmptyStreamingPlaceholder()
			// pendingConfirmation set means this channel close is a HITL
			// pause, not the turn actually ending — resolveConfirmation
			// reopens a fresh channel for the same turn, so the running
			// totals need to survive until then rather than land now, and
			// nothing queued/reload-pending should fire yet either
			// (turnInProgress is still true — see concludeTurn).
			if a.pendingConfirmation == nil {
				a.attachTurnFinishReason()
				endCmd = tea.Batch(endCmd, a.concludeTurn())
			}
			a.followTranscript()
			return a, endCmd
		}
		if msg.chunk.Err != nil {
			a.streamChan = nil
			a.turnCancel = nil
			endCmd := a.endReasoning()
			// See agentReplyMsg's error branch for why this runs before
			// systemMessage below.
			queueCmd := a.concludeTurn()
			if errors.Is(msg.chunk.Err, context.Canceled) {
				a.status = theme.StatusIdle
				a.dropEmptyStreamingPlaceholder()
				a.systemMessage("Stopped.")
			} else {
				a.status = theme.StatusError
				a.systemMessage("Error: " + msg.chunk.Err.Error())
			}
			return a, tea.Batch(endCmd, queueCmd)
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
		case msg.chunk.ReloadRequested:
			// Just latch the flag — concludeTurn (called once this turn
			// actually ends) is what turns it into a real reloadBackend()
			// call; acting on it here would hit reloadBackend()'s own
			// turnInProgress() guard and silently no-op.
			a.reloadRequested = true
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
			a.setNotice(msg.successMsg)
		} else {
			a.setNotice("Connected.")
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
			a.setNotice("No past sessions yet.")
			return a, nil
		}
		items := make([]paletteItem, len(msg.sessions))
		for i, s := range msg.sessions {
			items[i] = paletteItem{id: s.ID, title: shortSessionID(s.ID), desc: relativeTime(s.UpdatedAt)}
		}
		a.openMenu(paletteSessions, "Switch session", items)
		return a, nil

	case transcriptLoadedMsg:
		if msg.sessionID != a.sessionID {
			return a, nil // stale — a later switch already landed first
		}
		if msg.err != nil {
			a.systemMessage("Could not load history: " + msg.err.Error())
			return a, nil
		}
		a.replayTranscript(msg.entries)
		if len(msg.entries) == 0 {
			a.setNotice("Switched to " + shortSessionID(a.sessionID) + ".")
		} else {
			a.setNotice("Switched to " + shortSessionID(a.sessionID) + " — history loaded.")
		}
		a.followTranscript()
		return a, nil

	case sessionsDeletedMsg:
		n := len(msg.deletedIDs)
		if n == 0 {
			a.systemMessage("Could not delete session(s): " + msg.err.Error())
			return a, nil
		}
		label := "Deleted 1 session."
		if n != 1 {
			label = fmt.Sprintf("Deleted %d sessions.", n)
		}
		// Partial failure stays a transcript message (the error detail
		// must outlive a 4-second badge); clean success is just a notice.
		if msg.err != nil {
			a.systemMessage(label + " Some failed: " + msg.err.Error())
		} else {
			a.setNotice(label)
		}
		for _, id := range msg.deletedIDs {
			if id == a.sessionID {
				a.startNewSession()
				break
			}
		}
		if msg.remaining > 0 {
			return a, a.openSessionsMenu()
		}
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

func (a *App) View() tea.View {
	if !a.ready {
		return tea.NewView("")
	}

	width := a.renderWidth()
	topBar := renderTopBar(a.styles, width, a.sessionID, a.notice, a.contextUsed, a.contextWindow)
	body := a.viewport.View()
	if sticky := a.stickyPromptOverlay(); sticky != "" {
		body = overlay(body, sticky, 0, 0, a.viewport.Width())
	}
	if queued := a.queuedPromptOverlay(); queued != "" {
		y := lipgloss.Height(body) - lipgloss.Height(queued)
		body = overlay(body, queued, 0, y, a.viewport.Width())
	}
	inputBar := renderInputBar(a.styles, a.input, width, a.inputLines, true)
	footer := renderHelpFooter(a.styles, a.permissionMode == permissionFullAuto, a.verboseTools, width)

	// The "/" suggestions dropdown and the workingAnim both live in the
	// same slot directly above the input bar — suggestions take priority
	// over (rather than stacking above) the anim, since both showing at
	// once would otherwise mean the palette floats above the anim instead
	// of being attached to the input it's actually completing.
	parts := []string{topBar, body}
	if len(a.suggestMatches) > 0 {
		parts = append(parts, renderSuggestions(a.styles, a.suggestMatches, a.suggestIndex, width))
	} else {
		workingAnimBlock := blankWorkingAnim(a.styles.Theme, width)
		if a.workingAnimShouldRun() {
			workingAnimBlock = a.workingAnim.render(a.styles.Theme, width, a.workingLabel)
		}
		parts = append(parts, workingAnimBlock)
	}
	parts = append(parts, inputBar, footer)

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
