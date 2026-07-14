package ui

import (
	"fmt"
	"io"

	"tui-testing/internal/theme"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// paletteKind identifies which slash command opened the popup, so
// App.handlePaletteKey knows what selecting a row should do.
type paletteKind int

const (
	paletteNone paletteKind = iota
	paletteTheme
	paletteSettings
	paletteTextInput     // not list-backed — shared by /key and /agents' model field, see keyinput.go
	paletteConfirm       // list-backed (Approve/Deny) but has its own key handler — see hitl.go
	paletteSessions      // see sessions.go
	paletteKeyProvider   // /key's first step: which provider is this key for — see keyinput.go
	paletteAgents        // /agents' top-level list: root + every sub-agent — see agentsmenu.go
	paletteAgentDetail   // /agents' per-agent list: Provider / Model — see agentsmenu.go
	paletteAgentProvider // /agents' provider picker for whichever agent is selected — see agentsmenu.go
)

// paletteItem is a single row in any popup menu (themes, settings). id is
// what selection logic acts on; title/desc are display only. One item
// type serves every menu kind so the list plumbing (delegate, list.Model)
// doesn't need to be duplicated per menu.
type paletteItem struct {
	id    string
	title string
	desc  string
}

func (p paletteItem) FilterValue() string { return p.title }

// paletteDelegate renders each row of a popup menu. Re-created whenever
// the theme changes since it closes over a Styles value.
type paletteDelegate struct {
	styles theme.Styles
}

// One row per item, no gap between rows — was 2 rows + 1 blank spacer
// (3 rows/item), which meant a 12-row-capped popup only fit ~3 items
// before needing to scroll. Title and desc share the row (same pattern
// as the inline "/command" suggestion dropdown in commands.go) instead
// of stacking on separate lines.
func (d paletteDelegate) Height() int                             { return 1 }
func (d paletteDelegate) Spacing() int                            { return 0 }
func (d paletteDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d paletteDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(paletteItem)
	if !ok {
		return
	}

	// .Width() forces the row to fill its full width, not just the
	// glyphs — otherwise the row's backdrop color only shows behind the
	// text and the rest of the line falls back to the terminal's raw
	// default.
	width := m.Width()

	titleStyle, descStyle := d.styles.PaletteItem, d.styles.PaletteDesc
	if index == m.Index() {
		titleStyle, descStyle = d.styles.PaletteSelected, d.styles.PaletteSelectedDesc
	}

	left := titleStyle.Render(" " + item.title)
	fmt.Fprint(w, left+descStyle.Width(max(width-lipgloss.Width(left), 0)).Render("  "+item.desc))
}

// paletteTitleHeight is how many rows renderPaletteTitle's output takes
// up (the title line plus one blank spacer row) — callers must size the
// underlying list.Model shorter by this much, since that space is spent
// outside of it.
const paletteTitleHeight = 2

// newPaletteList builds the list.Model backing a popup menu. The title is
// deliberately NOT set via l.Title/l.Styles.Title — bubbles/list's own
// title rendering always appends an unstyled "  "+status suffix after the
// styled title text before truncating the row to width, and those
// trailing cells can never be made to carry our background color no
// matter how the title style itself is tuned. renderPaletteTitle renders
// it ourselves instead, the same way every other row in this app is
// rendered, so there's nothing bubbles/list didn't paint for us.
func newPaletteList(items []paletteItem, s theme.Styles, width, height int) list.Model {
	listItems := make([]list.Item, len(items))
	for i, it := range items {
		listItems[i] = it
	}

	l := list.New(listItems, paletteDelegate{styles: s}, width, max(height-paletteTitleHeight, 0))
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)

	// list.Model.View() reserves a row for its title/filter area whenever
	// filtering is enabled, regardless of SetShowTitle — so with filtering
	// left on, our height-minus-paletteTitleHeight math was still short
	// by one, and on a 3-item menu that was enough to force pagination.
	// These menus max out at 3 items, so filtering (typing to narrow the
	// list) was never buying much anyway; disabling it removes both the
	// phantom row and the pagination dots at once.
	l.SetFilteringEnabled(false)
	l.SetShowPagination(false)

	restylePalette(&l, s)
	return l
}

// restylePalette re-themes an already-built popup list in place. Needed
// because the list's delegate is baked in at construction time rather
// than read live from Styles on every render — used both on a normal
// theme swap and, more interestingly, while the /theme menu itself is
// open: live-previewing a highlighted theme should repaint the popup
// along with everything behind it, not just the background.
func restylePalette(l *list.Model, s theme.Styles) {
	l.SetDelegate(paletteDelegate{styles: s})
}

// renderPaletteTitle draws the popup's title as a single fully filled
// bar, rendered entirely by us so its background covers the whole width
// with no unstyled tail. The caller adds the blank spacer row below it.
func renderPaletteTitle(s theme.Styles, title string, width int) string {
	return s.PaletteTitle.Width(width).Render(" " + title)
}

// renderPaletteOverlay draws a popup menu as a centered box floating over
// bg (the already-rendered app frame) instead of taking over the whole
// screen, so the chat stays visible around and (for the /theme menu)
// previewable behind it.
func renderPaletteOverlay(bg string, s theme.Styles, title string, l list.Model, width, height int) string {
	content := renderPaletteTitle(s, title, l.Width()) + "\n\n" + l.View()
	box := s.PaletteBorder.Render(content)
	x := max((width-lipgloss.Width(box))/2, 0)
	y := max((height-lipgloss.Height(box))/2, 0)
	return overlay(bg, box, x, y, width)
}
