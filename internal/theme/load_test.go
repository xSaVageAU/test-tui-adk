package theme

import (
	"strings"
	"testing"
)

// validThemeJSON has every field validate() requires — tests that need a
// baseline to mutate/corrupt start from this.
const validThemeJSON = `{
	"name": "Test Theme",
	"background": "#000000",
	"surface": "#111111",
	"highlight": "#222222",
	"border": "#333333",
	"borderFocus": "#444444",
	"text": "#555555",
	"textMuted": "#666666",
	"textFaint": "#777777",
	"textOnFill": "#888888",
	"accent": "#999999",
	"accentMuted": "#aaaaaa",
	"reasoning": "#bbbbbb",
	"success": "#cccccc",
	"warning": "#dddddd",
	"error": "#eeeeee",
	"attention": "#ffffff"
}`

func TestParseThemeValid(t *testing.T) {
	th, err := parseTheme([]byte(validThemeJSON))
	if err != nil {
		t.Fatalf("parseTheme on a fully-populated theme returned an error: %v", err)
	}
	if th.Name != "Test Theme" {
		t.Errorf("Name = %q, want %q", th.Name, "Test Theme")
	}
	if th.Background != "#000000" {
		t.Errorf("Background = %q, want %q", th.Background, "#000000")
	}
}

func TestParseThemeMalformedJSON(t *testing.T) {
	_, err := parseTheme([]byte("{ not valid json"))
	if err == nil {
		t.Fatal("parseTheme on malformed JSON should return an error, got nil")
	}
}

func TestParseThemeMissingName(t *testing.T) {
	_, err := parseTheme([]byte(`{"background": "#000000"}`))
	if err == nil {
		t.Fatal("parseTheme with no \"name\" should return an error, got nil")
	}
}

func TestParseThemeMissingColorField(t *testing.T) {
	// A theme file that has a name and most colors, but is missing one
	// (accent) — this is the exact shape of mistake loadCustomThemes has
	// to tolerate from a hand-edited file without taking down the app.
	_, err := parseTheme([]byte(`{
		"name": "Almost",
		"background": "#000000",
		"surface": "#111111",
		"highlight": "#222222",
		"border": "#333333",
		"borderFocus": "#444444",
		"text": "#555555",
		"textMuted": "#666666",
		"textFaint": "#777777",
		"textOnFill": "#888888",
		"accentMuted": "#aaaaaa",
		"reasoning": "#bbbbbb",
		"success": "#cccccc",
		"warning": "#dddddd",
		"error": "#eeeeee",
		"attention": "#ffffff"
	}`))
	if err == nil {
		t.Fatal("parseTheme with a missing color field should return an error, got nil")
	}
}

func TestValidateReportsFirstMissingFieldInDeclarationOrder(t *testing.T) {
	// Name set, every color empty — validate() should name "background"
	// specifically (the first field in its own declaration-order list),
	// not just fail generically, since that's the deterministic-error
	// behavior its doc comment promises.
	th := Theme{Name: "Empty"}
	err := th.validate()
	if err == nil {
		t.Fatal("validate on a theme with no colors should return an error, got nil")
	}
	if !strings.Contains(err.Error(), "background") {
		t.Errorf("validate's error = %q, want it to name \"background\" as the first missing field", err.Error())
	}
}

func TestValidateAcceptsFullyPopulatedTheme(t *testing.T) {
	th, err := parseTheme([]byte(validThemeJSON))
	if err != nil {
		t.Fatalf("parseTheme failed unexpectedly: %v", err)
	}
	if err := th.validate(); err != nil {
		t.Errorf("validate on a fully-populated theme returned an error: %v", err)
	}
}

// TestLoadReturnsBuiltinsWithMonoFirst pins the invariant Manager.NewManager
// depends on (it always activates index 0) and that Load's own doc
// comment promises (defaultThemeOrder's order, Mono first).
func TestLoadReturnsBuiltinsWithMonoFirst(t *testing.T) {
	themes := Load()
	if len(themes) < len(defaultThemeOrder) {
		t.Fatalf("Load returned %d themes, want at least the %d built-ins", len(themes), len(defaultThemeOrder))
	}
	if themes[0].Name != "Mono" {
		t.Errorf("themes[0].Name = %q, want %q (the default active theme)", themes[0].Name, "Mono")
	}
	// Every built-in should itself validate — a broken embedded default
	// panics at Load() time (see mustLoadDefault), so simply reaching
	// this point already proves it, but check explicitly for a clearer
	// failure message if that ever changes.
	for i, name := range defaultThemeOrder {
		if err := themes[i].validate(); err != nil {
			t.Errorf("built-in theme %q failed validate(): %v", name, err)
		}
	}
}
