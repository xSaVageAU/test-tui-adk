package theme

import "github.com/charmbracelet/lipgloss"

// Styles is the compiled set of widget-level styles for a given Theme.
// Build a fresh Styles whenever the active Theme changes; nothing here
// should be mutated in place.
type Styles struct {
	Theme Theme

	// App chrome
	App lipgloss.Style

	// Top bar: a plain (no background panel) meta line (just the session
	// id — see header.go's renderTopBar) plus a solid rule separating it
	// from the chat below. HeaderSession carries a background — a
	// highlighted badge — to set the session id apart from plain text.
	// HeaderContextBar colors the context-window usage bar on the top
	// bar's right side (see header.go's renderContextBar) by how full it
	// is — same three-tier severity scale as HeaderStatus (Success/
	// Warning/Error), just keyed by a 0-1 fraction instead of a
	// StatusKind.
	Header           lipgloss.Style
	HeaderRule       lipgloss.Style
	HeaderTitle      lipgloss.Style
	HeaderSession    lipgloss.Style
	HeaderStatus     func(status StatusKind) lipgloss.Style
	HeaderContextBar func(frac float64) lipgloss.Style

	// Chat viewport. MessageSystem is the plain, quiet variant (only used
	// for the empty-state placeholder); MessageEvent is the badge shown
	// for actual system events (agent switched, key set, errors, ...) —
	// those want to stand out, not blend in. ReasoningBadge/ReasoningNote
	// sit next to the "agent" label (see chat.go's renderMessage) —
	// ReasoningBadge is a filled, eye-catching treatment (same weight as
	// ToolCallName/ToolConfirmPending) shown only while the model is
	// actively sending reasoning/thinking output (see App.reasoning);
	// once it's done, ReasoningNote is what's left behind permanently
	// ("thought for Xs") — quiet (just TextFaint, no fill), since a
	// finished number doesn't need to keep grabbing attention the way an
	// in-progress one does. ReasoningText is the actual reasoning content
	// itself, shown as its own block between the label and the reply —
	// italic and quiet (same family as MessageSystem) so it reads as
	// distinctly not-the-answer at a glance.
	Viewport          lipgloss.Style
	MessageUser       lipgloss.Style
	MessageAgent      lipgloss.Style
	MessageSystem     lipgloss.Style
	MessageEvent      lipgloss.Style
	MessageContent    lipgloss.Style
	MessageUserBubble lipgloss.Style // highlighted backdrop variant for user messages
	ReasoningBadge    lipgloss.Style
	ReasoningNote     lipgloss.Style
	ReasoningText     lipgloss.Style

	// MessageFinish* style the note shown under an agent reply that ended
	// on a notable non-"stop" finish reason — Warning for a benign
	// truncation (hit the model's max output length), Blocked for the
	// model actually refusing/filtering the response (safety, recitation,
	// prohibited content, ...). Quiet colored text, same weight as
	// ToolConfirmApproved/Denied — informational, nothing to act on.
	MessageFinishWarning lipgloss.Style
	MessageFinishBlocked lipgloss.Style

	// Tool activity renders as a colored left gutter bar (like a
	// blockquote marker) with the call and its result grouped tight
	// beneath it (or, in lean mode, on the same line) — see renderTool
	// in chat.go. ToolCallName is a filled badge, same Warning-colored
	// treatment as ToolConfirmPending, so a tool call reads as a
	// distinct event at a glance rather than just bold text
	// blending into the surrounding gutter.
	ToolGutter   lipgloss.Style // the "▏" bar itself
	ToolCallName lipgloss.Style // filled badge around the tool name
	ToolCallArgs lipgloss.Style // "key=value" args, de-emphasized vs. the name
	ToolResult   lipgloss.Style // the result line/segment

	// ToolConfirm* style the status line under a tool call awaiting human
	// approval (see App.hitlMode). ToolConfirmPending is a filled badge,
	// not just colored text — direct feedback was that a plain gutter
	// line blended in too easily; a pending approval should be
	// impossible to miss. Approved/Denied are quieter since they're just
	// a resolved record at that point, same weight as the result line.
	ToolConfirmPending  lipgloss.Style
	ToolConfirmApproved lipgloss.Style
	ToolConfirmDenied   lipgloss.Style

	// StickyPrompt is the pinned "you: ..." strip overlaid on top of the
	// scrolled chat once the last prompt scrolls out of view during an
	// oversized response — see View()'s sticky-overlay compositing.
	StickyPrompt lipgloss.Style

	// Boot banner: a bordered panel printed once as the transcript's first
	// entry. Every text style here carries Background(Surface) — same
	// reason as the popup rows above, since this shares that panel look.
	// BootRule is the thin divider between the tagline and the info rows.
	BootBorder  lipgloss.Style
	BootTitle   lipgloss.Style
	BootTagline lipgloss.Style
	BootRule    lipgloss.Style
	BootLabel   lipgloss.Style
	BootValue   lipgloss.Style

	// Input bar
	InputBar        lipgloss.Style
	InputBarFocused lipgloss.Style
	InputPrompt     lipgloss.Style
	InputHint       lipgloss.Style

	// Command palette. Item/Desc/Selected/SelectedDesc all carry an
	// explicit Background so a row still fills its full width in the
	// panel's backdrop color even past the end of its text — otherwise
	// only the glyphs get colored and the row's remaining space falls
	// back to the terminal's raw (usually black) default.
	PaletteBorder       lipgloss.Style
	PaletteTitle        lipgloss.Style
	PaletteItem         lipgloss.Style
	PaletteSelected     lipgloss.Style
	PaletteDesc         lipgloss.Style
	PaletteSelectedDesc lipgloss.Style

	// Inline "/command" suggestion dropdown, anchored above the input bar
	Suggestions            lipgloss.Style
	SuggestionItem         lipgloss.Style
	SuggestionSelected     lipgloss.Style
	SuggestionDesc         lipgloss.Style
	SuggestionSelectedDesc lipgloss.Style

	// HelpBadge styles one "key desc" pill in the bottom help footer (see
	// footer.go's renderHelpFooter) — one shared background per badge
	// (dim by default, a quiet grouping cue rather than something meant
	// to compete with the chat above it; the same Warning-filled
	// treatment as ToolConfirmPending while a toggle bind's own setting —
	// AutoAccept, VerboseTools — is currently on, so the footer doubles
	// as a quiet indicator of which toggles are active — the only such
	// indicator now that the top bar no longer shows one). Key renders a
	// shade darker than Desc within that shared
	// fill — see HelpBadgeStyle's own doc comment — which is the only
	// thing distinguishing the two; a from-scratch two-tone (separate
	// background per half) was tried and looked worse, not better, per
	// direct feedback.
	HelpBadge func(active bool) HelpBadgeStyle
}

// HelpBadgeStyle is one help-footer badge's pair of styles, sharing one
// Background: Key (the literal key combo, e.g. "shift+tab") a shade
// darker, Desc (what it does, e.g. "auto-accept") the badge's normal
// foreground — a light touch on purpose, just enough that the two read
// as distinct parts of the badge rather than one undifferentiated run
// of text.
type HelpBadgeStyle struct {
	Key  lipgloss.Style
	Desc lipgloss.Style
}

// StatusKind is the small set of states the header status pill can show.
type StatusKind int

const (
	StatusIdle StatusKind = iota
	StatusThinking
	StatusError
)

// New compiles a Styles set from a Theme. This is the one place widget
// appearance is defined — components should reference Styles fields, never
// build ad-hoc lipgloss.Style values with hardcoded colors.
func New(t Theme) Styles {
	s := Styles{Theme: t}

	s.App = lipgloss.NewStyle().
		Background(t.Background).
		Foreground(t.Text)

	s.Header = lipgloss.NewStyle().
		Foreground(t.TextMuted).
		Padding(0, 1)

	s.HeaderRule = lipgloss.NewStyle().
		Foreground(t.Border)

	s.HeaderTitle = lipgloss.NewStyle().
		Foreground(t.TextFaint)

	s.HeaderSession = lipgloss.NewStyle().
		Background(t.Highlight).
		Foreground(t.Accent).
		Bold(true).
		Padding(0, 1)

	s.HeaderStatus = func(status StatusKind) lipgloss.Style {
		base := lipgloss.NewStyle().Bold(true)
		switch status {
		case StatusThinking:
			return base.Foreground(t.Warning)
		case StatusError:
			return base.Foreground(t.Error)
		default:
			return base.Foreground(t.Success)
		}
	}

	s.HeaderContextBar = func(frac float64) lipgloss.Style {
		switch {
		case frac >= 0.9:
			return lipgloss.NewStyle().Foreground(t.Error)
		case frac >= 0.7:
			return lipgloss.NewStyle().Foreground(t.Warning)
		default:
			return lipgloss.NewStyle().Foreground(t.Success)
		}
	}

	s.Viewport = lipgloss.NewStyle().
		Foreground(t.Text)

	s.MessageUser = lipgloss.NewStyle().
		Foreground(t.Accent).
		Bold(true)

	s.MessageAgent = lipgloss.NewStyle().
		Foreground(t.TextMuted).
		Bold(true)

	s.MessageSystem = lipgloss.NewStyle().
		Foreground(t.TextFaint).
		Italic(true)

	s.MessageEvent = lipgloss.NewStyle().
		Background(t.Highlight).
		Foreground(t.Accent).
		Bold(true).
		Padding(0, 1)

	s.ReasoningBadge = lipgloss.NewStyle().
		Background(t.Accent).
		Foreground(t.TextOnFill).
		Bold(true).
		Padding(0, 1)

	s.ReasoningNote = lipgloss.NewStyle().
		Foreground(t.TextFaint)

	s.ReasoningText = lipgloss.NewStyle().
		Foreground(t.TextFaint).
		Italic(true)

	s.MessageContent = lipgloss.NewStyle().
		Foreground(t.Text)

	s.MessageUserBubble = lipgloss.NewStyle().
		Background(t.Highlight).
		Foreground(t.Text).
		Padding(0, 1)

	s.MessageFinishWarning = lipgloss.NewStyle().
		Foreground(t.Warning)

	s.MessageFinishBlocked = lipgloss.NewStyle().
		Foreground(t.Error).
		Bold(true)

	s.ToolGutter = lipgloss.NewStyle().
		Foreground(t.Warning)

	s.ToolCallName = lipgloss.NewStyle().
		Background(t.Warning).
		Foreground(t.TextOnFill).
		Bold(true).
		Padding(0, 1)

	s.ToolCallArgs = lipgloss.NewStyle().
		Foreground(t.TextMuted)

	s.ToolResult = lipgloss.NewStyle().
		Foreground(t.TextFaint)

	s.ToolConfirmPending = lipgloss.NewStyle().
		Background(t.Warning).
		Foreground(t.TextOnFill).
		Bold(true)

	s.ToolConfirmApproved = lipgloss.NewStyle().
		Foreground(t.Success)

	s.ToolConfirmDenied = lipgloss.NewStyle().
		Foreground(t.Error)

	s.StickyPrompt = lipgloss.NewStyle().
		Background(t.Highlight).
		Foreground(t.Text).
		Bold(true)

	s.BootBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Accent).
		Background(t.Surface).
		Padding(1, 2)

	s.BootTitle = lipgloss.NewStyle().
		Foreground(t.Accent).
		Background(t.Surface).
		Bold(true)

	s.BootTagline = lipgloss.NewStyle().
		Foreground(t.TextMuted).
		Background(t.Surface)

	s.BootRule = lipgloss.NewStyle().
		Foreground(t.Border).
		Background(t.Surface)

	s.BootLabel = lipgloss.NewStyle().
		Foreground(t.TextFaint).
		Background(t.Surface)

	s.BootValue = lipgloss.NewStyle().
		Foreground(t.Text).
		Background(t.Surface).
		Bold(true)

	s.InputBar = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Border).
		Padding(0, 1)

	s.InputBarFocused = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderFocus).
		Padding(0, 1)

	s.InputPrompt = lipgloss.NewStyle().
		Foreground(t.Accent).
		Bold(true)

	s.InputHint = lipgloss.NewStyle().
		Foreground(t.TextFaint)

	s.PaletteBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderFocus).
		Background(t.Surface).
		Padding(1, 2)

	s.PaletteTitle = lipgloss.NewStyle().
		Foreground(t.Accent).
		Background(t.Surface).
		Bold(true)

	s.PaletteItem = lipgloss.NewStyle().
		Foreground(t.Text).
		Background(t.Surface)

	s.PaletteSelected = lipgloss.NewStyle().
		Foreground(t.TextOnFill).
		Background(t.Accent).
		Bold(true)

	s.PaletteDesc = lipgloss.NewStyle().
		Foreground(t.TextFaint).
		Background(t.Surface)

	s.PaletteSelectedDesc = lipgloss.NewStyle().
		Foreground(t.TextOnFill).
		Background(t.Accent)

	s.Suggestions = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Border).
		Background(t.Surface).
		Padding(0, 1)

	s.SuggestionItem = lipgloss.NewStyle().
		Foreground(t.Accent).
		Background(t.Surface).
		Bold(true)

	s.SuggestionSelected = lipgloss.NewStyle().
		Foreground(t.TextOnFill).
		Background(t.Accent).
		Bold(true)

	s.SuggestionDesc = lipgloss.NewStyle().
		Foreground(t.TextFaint).
		Background(t.Surface)

	s.SuggestionSelectedDesc = lipgloss.NewStyle().
		Foreground(t.TextOnFill).
		Background(t.Accent)

	s.HelpBadge = func(active bool) HelpBadgeStyle {
		bg, keyFg, descFg := t.Surface, t.TextFaint, t.TextMuted
		if active {
			bg, keyFg, descFg = t.Warning, t.TextOnFill, t.TextOnFill
		}
		keyStyle := lipgloss.NewStyle().Background(bg).Foreground(keyFg).Padding(0, 0, 0, 1)
		descStyle := lipgloss.NewStyle().Background(bg).Foreground(descFg).Padding(0, 1, 0, 0)
		if active {
			descStyle = descStyle.Bold(true)
		}
		return HelpBadgeStyle{Key: keyStyle, Desc: descStyle}
	}

	return s
}
