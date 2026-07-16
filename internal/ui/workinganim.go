// This file is the "agent is working" animation's shared machinery: the
// variant enum/registry, live state (workingAnimState) and its tick loop,
// the render dispatcher, background-painting helpers every variant draws
// through (bgStyle et al.), and the color-math helpers (lerpColor,
// hexToRGB, randomRainRune) the variants share. The 10 renderX functions
// themselves — the actual animations — live in workinganim_variants.go.
package ui

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"tui-testing/internal/theme"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// workingAnimHeight is the fixed number of rows reserved above the input
// box for the "agent is working" animation — always reserved, whether or
// not anything is currently rendered into it, so the layout never jumps
// when a turn starts/ends. Individual variants can use fewer rows (see
// render's padding below) but never more. See App.layout/View.
const workingAnimHeight = 2

// workingAnimVariant selects which of the ported animations is active —
// these started as standalone experiments in a separate scratch repo
// (the user built ~10 candidate "thinking" loaders and wanted to compare
// them live in the real app rather than in isolation) and were ported
// here, then iterated on through several rounds of live feedback: ones
// that didn't land (Neural Sparks, DNA Weave, Thought Stream, Breathing
// Dot) were dropped, new ones (Braille Wave, Radar Sweep, Slash Trail)
// added, and several survivors got tweaked — see each renderX's doc
// comment for what changed and why.
type workingAnimVariant int

const (
	// animEqualizer is first — the default (see parseWorkingAnimVariant's
	// fallback) and /loader's top row, both because it's index 0 here.
	animEqualizer workingAnimVariant = iota
	animPulseWave
	animOrbit
	animGlitchScan
	animCylonScanner
	animBouncingDots
	animMatrixRain
	animBrailleWave
	animRadarSweep
	animSlashTrail
	workingAnimCount
)

// workingAnimNames doubles as both display title and the id persisted to
// settings.json's workingAnim field — same convention /theme already
// uses for theme names.
var workingAnimNames = [workingAnimCount]string{
	"Equalizer",
	"Pulse Wave",
	"Orbit",
	"Glitch Scan",
	"Cylon Scanner",
	"Bouncing Dots",
	"Matrix Rain",
	"Braille Wave",
	"Radar Sweep",
	"Slash Trail",
}

func parseWorkingAnimVariant(name string) workingAnimVariant {
	for i, n := range workingAnimNames {
		if n == name {
			return workingAnimVariant(i)
		}
	}
	return animEqualizer
}

// workingAnimState is the animation's whole live state: which variant
// and the running frame counter every variant reads. One instance lives
// on App for its whole lifetime — see App.workingAnim. Used to hold
// Neural Sparks' persistent per-cell field too; nothing left needs
// state beyond the frame counter and an RNG now that variant's gone.
type workingAnimState struct {
	variant workingAnimVariant
	frame   int
	rng     *rand.Rand
}

func newWorkingAnimState(variant workingAnimVariant) workingAnimState {
	return workingAnimState{variant: variant, rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
}

// advance moves state forward one tick. Every current variant derives
// its whole look from the frame counter alone (unlike Neural Sparks
// before it, which needed a persistent field stepped explicitly here) —
// kept as its own method regardless, since "what happens once per tick"
// is a distinct concern from "what a render call computes," and a
// future variant may need real per-tick state again.
func (w *workingAnimState) advance() {
	w.frame++
}

// render dispatches to the active variant. Not every variant fills all
// workingAnimHeight rows any more (Pulse Wave, Cylon Scanner, Equalizer,
// and Radar Sweep are single-line — see each's doc comment for why
// shorter reads better than stretched) — short output
// is padded with blank rows at the *top*, so a one-line variant still
// anchors to the bottom, right against the input box, rather than
// floating with a gap underneath it.
func (w *workingAnimState) render(t theme.Theme, width int, label string) string {
	var out string
	switch w.variant {
	case animPulseWave:
		out = w.renderPulseWave(t, width)
	case animOrbit:
		out = w.renderOrbit(t, width, label)
	case animGlitchScan:
		out = w.renderGlitchScan(t, width)
	case animCylonScanner:
		out = w.renderCylonScanner(t, width)
	case animBouncingDots:
		out = w.renderBouncingDots(t, width)
	case animEqualizer:
		out = w.renderEqualizer(t, width)
	case animMatrixRain:
		out = w.renderMatrixRain(t, width)
	case animBrailleWave:
		out = w.renderBrailleWave(t, width)
	case animRadarSweep:
		out = w.renderRadarSweep(t, width)
	case animSlashTrail:
		out = w.renderSlashTrail(t, width)
	}

	lines := strings.Split(out, "\n")
	blank := blankAnimLine(t, width)
	for len(lines) < workingAnimHeight {
		lines = append([]string{blank}, lines...)
	}
	if len(lines) > workingAnimHeight {
		lines = lines[len(lines)-workingAnimHeight:]
	}
	return strings.Join(lines, "\n")
}

// bgStyle is every glyph style in this file's shared starting point.
// Most of these animations draw with partial block/braille characters
// (▁▂▃▄, ⠋⠙⠹, ▬─, ...) which only fill *part* of their cell with the
// foreground color — without an explicit background, the rest of that
// cell showed the terminal's raw default instead of the theme's,
// visible as the "reserved area" looking black while an animation
// played even though the glyphs themselves were the right color.
func bgStyle(t theme.Theme) lipgloss.Style {
	return lipgloss.NewStyle().Background(lipgloss.Color(t.Background))
}

// blankAnimLine is one width-wide, background-painted but otherwise
// empty row — used both by render (padding a short variant up to
// workingAnimHeight) and blankWorkingAnim (the whole reserved block
// while idle). A bare "" here would leave the terminal's raw default
// background showing through instead of the theme's.
func blankAnimLine(t theme.Theme, width int) string {
	return lipgloss.NewStyle().Background(lipgloss.Color(t.Background)).Width(width).Render("")
}

// blankWorkingAnim is what occupies the reserved rows while nothing is
// actively running — workingAnimHeight blank (but background-painted)
// lines, so the layout never shifts when a turn starts or ends.
func blankWorkingAnim(t theme.Theme, width int) string {
	line := blankAnimLine(t, width)
	lines := make([]string, workingAnimHeight)
	for i := range lines {
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// ─────────────────────────────────────────────
// tick loop
// ─────────────────────────────────────────────

type workingAnimTickMsg struct{}

func workingAnimTick() tea.Cmd {
	return tea.Tick(60*time.Millisecond, func(time.Time) tea.Msg {
		return workingAnimTickMsg{}
	})
}

// startWorkingAnim kicks off the tick loop if it isn't already running —
// idempotent, so both dispatchToBackend and resolveConfirmation (the two
// moments a turn starts "thinking") can call it unconditionally without
// risking two overlapping tick chains. Also used by /loader to drive its
// own live preview while the picker is open, independent of whether the
// agent is actually thinking (see workingAnimShouldRun).
func (a *App) startWorkingAnim() tea.Cmd {
	if a.workingAnimActive {
		return nil
	}
	a.workingAnimActive = true
	return workingAnimTick()
}

// workingAnimShouldRun is checked both when a tick lands (to decide
// whether to keep going) and when rendering (to decide whether to show
// the animation or the blank reserved rows): true while the agent is
// actually thinking, or while /loader is open for its live preview.
func (a *App) workingAnimShouldRun() bool {
	return a.status == theme.StatusThinking || a.paletteKind == paletteLoader
}

// ─────────────────────────────────────────────
// shared color helpers
// ─────────────────────────────────────────────

// lerpColor and hexToRGB operate on plain hex strings, not lipgloss.Color
// — matching theme.Theme's own fields (plain strings; see theme.go's doc
// comment on why lipgloss v2 can no longer be used as a struct field
// type). Callers wrap the result with lipgloss.Color(...) at the point
// they hand it to a Style's Foreground/Background.
func lerpColor(a, b string, t float64) string {
	ar, ag, ab := hexToRGB(a)
	br, bg, bb := hexToRGB(b)
	r := int(float64(ar) + t*(float64(br)-float64(ar)))
	g := int(float64(ag) + t*(float64(bg)-float64(ag)))
	b2 := int(float64(ab) + t*(float64(bb)-float64(ab)))
	return fmt.Sprintf("#%02X%02X%02X", r, g, b2)
}

func hexToRGB(h string) (int, int, int) {
	h = strings.TrimPrefix(h, "#")
	if len(h) != 6 {
		return 0, 0, 0
	}
	var r, g, b int
	fmt.Sscanf(h, "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}

func randomRainRune(rng *rand.Rand) rune {
	runes := []rune("ｦｧｨｩｪｫｬｭｮｯｰｱｲｳｴｵｶｷｸｹｺｻｼｽｾｿﾀﾁﾂﾃﾄﾅﾆﾇﾈﾉﾊﾋﾌﾍﾎﾏﾐﾑﾒﾓﾔﾕﾖﾗﾘﾙﾚﾛﾜﾝ0123456789<>[]{}+-*")
	return runes[rng.Intn(len(runes))]
}
