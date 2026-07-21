package ui

import (
	"strings"
	"testing"

	"tui-testing/internal/theme"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

// newTestApp builds a minimal but realistically-initialized App — real
// theme manager/styles/viewport, same as NewApp uses, but with no
// backend or config wiring — enough to exercise menu-stack/state logic
// without a live connection.
func newTestApp() *App {
	mgr := theme.NewManager(theme.Load()...)
	a := &App{
		themeMgr: mgr,
		styles:   mgr.Styles(),
	}
	a.width, a.height = 80, 24
	a.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(24))
	return a
}

func TestBackOrCloseRunsAtTopWhenStackEmpty(t *testing.T) {
	a := newTestApp()
	calledTop := false
	a.backOrClose(func() tea.Cmd {
		calledTop = true
		return nil
	})
	if !calledTop {
		t.Fatal("backOrClose with an empty stack should run atTop")
	}
}

func TestBackOrClosePopsMostRecentlyPushedFirst(t *testing.T) {
	a := newTestApp()
	var order []string
	a.pushMenuBack(func() tea.Cmd { order = append(order, "first"); return nil })
	a.pushMenuBack(func() tea.Cmd { order = append(order, "second"); return nil })

	a.backOrClose(func() tea.Cmd {
		t.Fatal("atTop should not run while entries remain on the stack")
		return nil
	})
	if got := order; len(got) != 1 || got[0] != "second" {
		t.Fatalf("expected the most recently pushed entry to run first, got %v", order)
	}

	a.backOrClose(func() tea.Cmd {
		t.Fatal("atTop should not run — one entry is still on the stack")
		return nil
	})
	if got := order; len(got) != 2 || got[1] != "first" {
		t.Fatalf("expected the second-most-recently pushed entry next, got %v", order)
	}

	calledTop := false
	a.backOrClose(func() tea.Cmd { calledTop = true; return nil })
	if !calledTop {
		t.Fatal("backOrClose should run atTop once the stack is empty")
	}
}

func TestCloseMenuClearsPaletteKindAndStack(t *testing.T) {
	a := newTestApp()
	a.pushMenuBack(func() tea.Cmd { return nil })
	a.paletteKind = paletteSettings

	a.closeMenu()

	if a.paletteKind != paletteNone {
		t.Fatalf("closeMenu should reset paletteKind to paletteNone, got %v", a.paletteKind)
	}
	if len(a.menuBack) != 0 {
		t.Fatal("closeMenu should clear the back-stack")
	}
}

func TestCloseMenuCmdReturnsNilAndCloses(t *testing.T) {
	a := newTestApp()
	a.paletteKind = paletteTheme

	if cmd := a.closeMenuCmd(); cmd != nil {
		t.Fatal("closeMenuCmd should always return nil")
	}
	if a.paletteKind != paletteNone {
		t.Fatal("closeMenuCmd should close the menu")
	}
}

func TestCancelMenuRevertsLiveThemePreview(t *testing.T) {
	a := newTestApp()
	names := a.themeMgr.Names()
	if len(names) < 2 {
		t.Skip("need at least two built-in themes to test reverting a preview")
	}
	origin, other := names[0], names[1]

	a.themeMgr.Set(origin)
	a.applyTheme()
	a.themeMenuOrigin = origin
	a.paletteKind = paletteTheme

	// Simulate live-previewing a different theme while /theme is open.
	a.previewTheme(other)
	if a.themeMgr.Current().Name != other {
		t.Fatalf("previewTheme should switch the live theme immediately")
	}

	a.cancelMenu()

	if got := a.themeMgr.Current().Name; got != origin {
		t.Fatalf("cancelMenu should revert to the theme active before the menu opened, got %q want %q", got, origin)
	}
	if a.paletteKind != paletteNone {
		t.Fatal("cancelMenu should close the menu")
	}
}

func TestCancelMenuDropsPendingMessageForKeyProviderStep(t *testing.T) {
	a := newTestApp()
	a.paletteKind = paletteKeyProvider
	a.pendingMessage = "hello"

	a.cancelMenu()

	if a.pendingMessage != "" {
		t.Fatal("cancelMenu should drop a pending message when /key's provider picker is cancelled")
	}
	if !anyMessageContains(a.messages, "no API key set") {
		t.Fatal("cancelMenu should report the dropped message")
	}
}

func TestCancelMenuDropsPendingMessageForAPIKeyTextPopupOnly(t *testing.T) {
	a := newTestApp()
	a.paletteKind = paletteTextInput
	a.textPopupKind = textPopupAgentModel
	a.pendingMessage = "hello"

	a.cancelMenu()

	// Only the /key masked-field popup (textPopupAPIKey) should drop a
	// pending message — /agents' model field shares the same paletteKind
	// but has nothing to do with a message waiting on a key.
	if a.pendingMessage != "hello" {
		t.Fatal("cancelMenu should not touch pendingMessage for a non-API-key text popup")
	}
}

func TestCancelMenuReportsAgentToolsChangedRegardlessOfPaletteKind(t *testing.T) {
	a := newTestApp()
	// agentToolsChanged is checked unconditionally, not just when
	// paletteKind == paletteAgentTools — see cancelMenu's doc comment on
	// why (Esc from the Tools page itself almost always steps back to
	// the detail page instead of reaching cancelMenu directly).
	a.paletteKind = paletteAgents
	a.agentToolsChanged = true

	a.cancelMenu()

	// The report is a transient top-bar notice, not a transcript message
	// — see notice.go for the notice/systemMessage split.
	if !strings.Contains(a.notice, "Tools updated") {
		t.Fatal("cancelMenu should report a pending tools reload when agentToolsChanged is true")
	}
	if a.paletteKind != paletteNone {
		t.Fatal("cancelMenu should still close the menu")
	}
}

func anyMessageContains(messages []ChatMessage, substr string) bool {
	for _, m := range messages {
		if strings.Contains(m.Content, substr) {
			return true
		}
	}
	return false
}
