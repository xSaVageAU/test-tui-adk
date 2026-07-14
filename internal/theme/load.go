package theme

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"tui-testing/internal/appdir"

	"github.com/charmbracelet/lipgloss"
)

// defaultsFS embeds the built-in theme configs at build time — see
// defaults/*.json. Nothing about them is special at runtime; they're
// parsed through the exact same path as a user's own theme files.
//
//go:embed defaults/*.json
var defaultsFS embed.FS

// defaultThemeOrder fixes both load and cycle order for the built-ins.
// embed.FS's ReadDir sorts lexically ("nord" before "vibrant"), which
// isn't the order this app wants — Mono first, since it's the theme
// active on a fresh launch (see Manager.NewManager, which always starts
// on index 0).
var defaultThemeOrder = []string{"mono", "vibrant", "nord", "dracula", "gruvbox", "tokyonight", "greenphosphor"}

// Load returns every available theme: the built-ins embedded in the
// binary, in defaultThemeOrder, followed by any custom themes found in
// appdir's "themes" directory (created if missing). A custom theme whose
// Name matches a built-in's replaces it in place — same position, so
// cycle order and Manager.Set("Mono") stay predictable — rather than
// appearing as a confusing duplicate entry.
//
// A malformed embedded default is a build-time bug (the file shipped
// broken) and panics rather than degrading — see mustLoadDefault. A
// malformed *custom* file is a runtime condition a user can hit by
// hand-editing, so it's skipped rather than blocking the whole app from
// starting; same for the custom directory being unreadable for some
// filesystem reason.
func Load() []Theme {
	themes := make([]Theme, len(defaultThemeOrder))
	for i, name := range defaultThemeOrder {
		themes[i] = mustLoadDefault(name)
	}

	for _, custom := range loadCustomThemes() {
		if i := indexByName(themes, custom.Name); i >= 0 {
			themes[i] = custom
		} else {
			themes = append(themes, custom)
		}
	}
	return themes
}

func mustLoadDefault(name string) Theme {
	data, err := defaultsFS.ReadFile("defaults/" + name + ".json")
	if err != nil {
		panic(fmt.Sprintf("theme: embedded default %q missing: %v", name, err))
	}
	t, err := parseTheme(data)
	if err != nil {
		panic(fmt.Sprintf("theme: embedded default %q invalid: %v", name, err))
	}
	return t
}

// loadCustomThemes reads every *.json file in appdir's "themes"
// directory. Best-effort: any single file (or the directory itself)
// that can't be read/parsed/validated is silently skipped rather than
// failing the whole load — a typo in one custom theme shouldn't take
// down the app's ability to start with its built-ins.
func loadCustomThemes() []Theme {
	dir, err := appdir.Path("themes")
	if err != nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var themes []Theme
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		t, err := parseTheme(data)
		if err != nil {
			continue
		}
		themes = append(themes, t)
	}
	return themes
}

// parseTheme decodes and validates a single theme file's contents —
// shared by both the embedded defaults and custom on-disk themes so
// they're held to the identical standard.
func parseTheme(data []byte) (Theme, error) {
	var t Theme
	if err := json.Unmarshal(data, &t); err != nil {
		return Theme{}, err
	}
	if err := t.validate(); err != nil {
		return Theme{}, err
	}
	return t, nil
}

// validate reports the first missing required field, in field-declaration
// order, so the error is at least deterministic even though nothing
// downstream can render a theme with holes in it — every field feeds a
// lipgloss.Style somewhere in New.
func (t Theme) validate() error {
	if t.Name == "" {
		return fmt.Errorf("missing required \"name\"")
	}
	fields := []struct {
		key   string
		value lipgloss.Color
	}{
		{"background", t.Background},
		{"surface", t.Surface},
		{"highlight", t.Highlight},
		{"border", t.Border},
		{"borderFocus", t.BorderFocus},
		{"text", t.Text},
		{"textMuted", t.TextMuted},
		{"textFaint", t.TextFaint},
		{"textOnFill", t.TextOnFill},
		{"accent", t.Accent},
		{"accentMuted", t.AccentMuted},
		{"reasoning", t.Reasoning},
		{"success", t.Success},
		{"warning", t.Warning},
		{"error", t.Error},
		{"attention", t.Attention},
	}
	for _, f := range fields {
		if f.value == "" {
			return fmt.Errorf("%s: missing required color %q", t.Name, f.key)
		}
	}
	return nil
}

func indexByName(themes []Theme, name string) int {
	for i, t := range themes {
		if t.Name == name {
			return i
		}
	}
	return -1
}
