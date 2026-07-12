package ui

import "github.com/charmbracelet/bubbles/key"

// keyMap centralizes every global keybinding so help text and Update's
// switch statement can't drift apart. Deliberately small: everything that
// isn't sending a message or quitting goes through the "/" command system
// instead of a dedicated hotkey.
type keyMap struct {
	Quit key.Binding
	Send key.Binding

	// Escape isn't a discoverable hotkey — it's just how an open popup
	// menu or the inline "/command" suggestion dropdown gets dismissed —
	// so it's left out of ShortHelp.
	Escape key.Binding

	// Up/Down are only live while a popup menu or the "/command"
	// suggestion dropdown is showing, repurposing the arrow keys the
	// textarea would otherwise use for cursor movement. Not global
	// hotkeys, so also left out of ShortHelp.
	Up   key.Binding
	Down key.Binding

	// Commands has no real key binding; it exists purely so the footer
	// can advertise "/ commands" next to Send and Quit.
	Commands key.Binding

	// ScrollUp/ScrollDown jump the chat viewport to the previous/next
	// user prompt rather than paging by a fixed height. Deliberately
	// bound to pgup/pgdown only — NOT viewport's own DefaultKeyMap, which
	// also aliases these to letters like f/b/u/d/j/k/h/l and even
	// spacebar. Reasonable in a read-only pager, but here those keys need
	// to reach the input untouched since the user is typing a message,
	// not paging a static document.
	ScrollUp   key.Binding
	ScrollDown key.Binding
}

var keys = keyMap{
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
	Send: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "send"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close"),
	),
	Up: key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
	),
	Commands: key.NewBinding(
		key.WithHelp("/", "commands"),
	),
	ScrollUp: key.NewBinding(
		key.WithKeys("pgup"),
		key.WithHelp("pgup/pgdn", "prev/next prompt"),
	),
	ScrollDown: key.NewBinding(
		key.WithKeys("pgdown"),
	),
}

// ShortHelp and FullHelp satisfy bubbles/help.KeyMap so the footer help
// line can be generated from the same bindings Update() switches on.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{keys.Send, keys.Commands, keys.ScrollUp, keys.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}
