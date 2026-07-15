package ui

import (
	"fmt"
	"math"
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

// ─────────────────────────────────────────────
// 1. Pulse Wave — was two stacked sine-wave lines; the second line read
// as redundant next to the first (same shape, different phase), so it's
// gone and this is a one-liner now.
// ─────────────────────────────────────────────

func (w *workingAnimState) renderPulseWave(t theme.Theme, width int) string {
	frameVal := float64(w.frame) * 0.05
	chars := []rune("▁▂▃▄▅▆▇█▇▆▅▄▃▂▁")
	var line strings.Builder

	for i := 0; i < width; i++ {
		phase := float64(i)/float64(width)*math.Pi*6 - frameVal*2
		v := (math.Sin(phase) + 1) / 2
		idx := int(v * float64(len(chars)-1))

		col := lerpColor(t.Accent, t.Warning, v)
		if v > 0.85 {
			col = lerpColor(t.Warning, t.Text, (v-0.85)/0.15)
		}
		line.WriteString(bgStyle(t).Foreground(lipgloss.Color(col)).Render(string(chars[idx])))
	}

	return line.String()
}

// ─────────────────────────────────────────────
// 2. Orbit — the center label used to be a hardcoded " thinking ";
// label is now whatever the caller passes (App.workingLabel), which
// tracks the turn's real phase — "thinking" normally, "using <tool>"
// while a tool call is actually in flight — so this reflects genuine
// state instead of a fixed word.
// ─────────────────────────────────────────────

func (w *workingAnimState) renderOrbit(t theme.Theme, width int, label string) string {
	frameVal := float64(w.frame) * 0.07

	cx := float64(width) / 2.0
	radius := float64(width)/2.0 - 4
	if radius < 5 {
		radius = 5
	}

	cells := make([]struct {
		ch  rune
		col string
	}, width)
	for i := range cells {
		cells[i].ch = ' '
	}

	type orbitDot struct {
		angle      float64
		colA, colB string
	}
	dots := []orbitDot{
		{frameVal, t.Accent, t.AccentMuted},
		{frameVal + math.Pi, t.Warning, t.Error},
	}

	trailLen := 12
	for _, dot := range dots {
		for ti := 0; ti < trailLen; ti++ {
			a := dot.angle - float64(ti)*0.18
			x := int(cx + math.Cos(a)*radius)
			if x < 0 || x >= width {
				continue
			}
			fade := 1.0 - float64(ti)/float64(trailLen)
			col := lerpColor(dot.colB, dot.colA, fade)
			if ti > 0 {
				rr, g, b := hexToRGB(col)
				col = fmt.Sprintf("#%02X%02X%02X", int(float64(rr)*fade), int(float64(g)*fade), int(float64(b)*fade))
			}
			ch := '●'
			if ti > 0 {
				ch = '·'
			}
			cells[x].ch = ch
			cells[x].col = col
		}
	}

	labelStr := " " + label + " "
	lr := []rune(labelStr)
	lStart := width/2 - len(lr)/2
	for i, r := range lr {
		pos := lStart + i
		if pos >= 0 && pos < width {
			cells[pos].ch = r
			cells[pos].col = t.Text
		}
	}

	var sb strings.Builder
	for _, c := range cells {
		sb.WriteString(bgStyle(t).Foreground(lipgloss.Color(c.col)).Render(string(c.ch)))
	}

	pulse := float64(w.frame) * 0.08
	var sb2 strings.Builder
	for i := 0; i < width; i++ {
		v := (math.Sin(float64(i)/float64(width)*math.Pi*4-pulse) + 1) / 2 * 0.5
		sb2.WriteString(bgStyle(t).Foreground(lipgloss.Color(lerpColor(t.Surface, t.AccentMuted, v))).Render("▄"))
	}

	return sb.String() + "\n" + sb2.String()
}

// ─────────────────────────────────────────────
// 3. Glitch Scan — the glitch effect used to keep substituting random
// characters for as long as the scan band was on-screen, including well
// past the end of the actual status message (which was padded with
// blank spaces out to the full width) — so it visibly "glitched empty
// space." Confined now to i < msgLen: past the real text, the scan band
// still travels through (driving the same pacing/progress bar below),
// it just never substitutes a glitch character there.
// ─────────────────────────────────────────────

var glitchRunes = []rune("▓█░▒╔╗╚╝║═╠╣╦╩╬⌂◙◘☺☻♦♣♠♥")
var statusMessages = []string{
	"Thinking deeply about your request and context…",
	"Reasoning through the problem space carefully… ",
	"Generating a thoughtful and nuanced response…  ",
	"Consulting knowledge and crafting an answer…   ",
}

func (w *workingAnimState) renderGlitchScan(t theme.Theme, width int) string {
	msg := statusMessages[(w.frame/80)%len(statusMessages)]
	msgRunes := []rune(msg)
	if len(msgRunes) > width {
		msgRunes = msgRunes[:width]
	}
	msgLen := len(msgRunes)

	scanPos := (w.frame*2)%(width+20) - 10
	scanWidth := 8

	rng := w.rng
	line1 := &strings.Builder{}

	for i := 0; i < width; i++ {
		dist := i - scanPos
		inScan := dist >= 0 && dist < scanWidth && i < msgLen

		var ch rune
		var col string
		bold := false

		switch {
		case inScan:
			fade := 1.0 - float64(dist)/float64(scanWidth)
			if rng.Float64() < fade*0.8 {
				ch = glitchRunes[rng.Intn(len(glitchRunes))]
			} else {
				ch = msgRunes[i]
			}
			col = lerpColor(t.Warning, t.Text, fade)
			bold = true
		case i < msgLen:
			ch = msgRunes[i]
			if i < scanPos {
				col = t.AccentMuted
			} else {
				col = t.Highlight
			}
		default:
			ch = ' '
		}

		st := bgStyle(t).Foreground(lipgloss.Color(col))
		if bold {
			st = st.Bold(true)
		}
		line1.WriteString(st.Render(string(ch)))
	}

	progress := float64(scanPos+10) / float64(width+20)
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	filled := int(progress * float64(width))

	var sb2 strings.Builder
	for i := 0; i < width; i++ {
		if i < filled {
			sb2.WriteString(bgStyle(t).Foreground(lipgloss.Color(lerpColor(t.AccentMuted, t.Accent, float64(i)/float64(width)))).Render("▬"))
		} else {
			sb2.WriteString(bgStyle(t).Foreground(lipgloss.Color(t.Highlight)).Render("─"))
		}
	}

	return line1.String() + "\n" + sb2.String()
}

// ─────────────────────────────────────────────
// 4. Cylon Scanner — dropped the "scanning workspaces" label: it read
// as a fake, made-up activity (this app doesn't "scan workspaces"), and
// the scanner motion alone already reads as "something is happening"
// without needing invented text to justify it. One line now.
// ─────────────────────────────────────────────

func (w *workingAnimState) renderCylonScanner(t theme.Theme, width int) string {
	period := 2 * (width - 1)
	pos := w.frame % period
	if pos >= width {
		pos = period - pos
	}

	var sb strings.Builder
	for i := 0; i < width; i++ {
		dist := math.Abs(float64(i - pos))
		if dist < 6 {
			fade := 1.0 - dist/6.0
			col := lerpColor(t.Surface, t.Accent, fade)
			var ch string
			switch {
			case dist == 0:
				ch = "█"
			case dist < 2:
				ch = "▓"
			case dist < 4:
				ch = "▒"
			default:
				ch = "░"
			}
			sb.WriteString(bgStyle(t).Foreground(lipgloss.Color(col)).Render(ch))
		} else {
			sb.WriteString(bgStyle(t).Foreground(lipgloss.Color(t.Highlight)).Render("─"))
		}
	}
	return sb.String()
}

// ─────────────────────────────────────────────
// 5. Bouncing Dots (unchanged — held up fine as-is)
// ─────────────────────────────────────────────

func (w *workingAnimState) renderBouncingDots(t theme.Theme, width int) string {
	dotsCount := 7
	spacing := 4
	start := width/2 - (dotsCount*spacing)/2

	cells := make([]string, width)
	blank := bgStyle(t).Render(" ")
	for i := range cells {
		cells[i] = blank
	}

	for j := 0; j < dotsCount; j++ {
		pos := start + j*spacing
		if pos < 0 || pos >= width {
			continue
		}
		val := (math.Sin(float64(w.frame)*0.25-float64(j)*0.7) + 1.0) / 2.0

		var ch string
		var col string
		switch {
		case val > 0.8:
			ch, col = "●", t.Accent
		case val > 0.5:
			ch, col = "•", t.AccentMuted
		case val > 0.25:
			ch, col = "·", t.TextFaint
		default:
			ch, col = " ", t.Surface
		}
		cells[pos] = bgStyle(t).Foreground(lipgloss.Color(col)).Render(ch)
	}

	var sb strings.Builder
	for _, c := range cells {
		sb.WriteString(c)
	}
	label := bgStyle(t).Foreground(lipgloss.Color(t.TextMuted)).Render(" aligning thoughts ")
	return sb.String() + "\n" + label
}

// ─────────────────────────────────────────────
// 6. Equalizer — was two rows; the top row spent most of its time on a
// literal blank space (the low end of the rune ramp " ▂▃▄▅▆▇█" starts
// with a space), so quiet moments looked like the animation had briefly
// stopped rather than just being low. One row now, and the ramp starts
// at ▁ instead of a blank so there's always *something* visible.
// ─────────────────────────────────────────────

func (w *workingAnimState) renderEqualizer(t theme.Theme, width int) string {
	runes := []rune("▁▂▃▄▅▆▇█")
	var row strings.Builder
	phase := float64(w.frame) * 0.2

	for i := 0; i < width; i++ {
		v := (math.Sin(float64(i)*0.15+phase) + math.Sin(float64(i)*0.37-phase*0.7) + 2.0) / 4.0
		idx := int(v * float64(len(runes)-1))
		col := lerpColor(t.Accent, t.Warning, float64(i)/float64(width))
		row.WriteString(bgStyle(t).Foreground(lipgloss.Color(col)).Render(string(runes[idx])))
	}
	return row.String()
}

// ─────────────────────────────────────────────
// 7. Matrix Rain (unchanged — held up fine as-is)
// ─────────────────────────────────────────────

func (w *workingAnimState) renderMatrixRain(t theme.Theme, width int) string {
	var lines [2]strings.Builder

	for c := 0; c < width; c++ {
		speed := 0.15 + 0.05*math.Sin(float64(c)*3.7)
		offset := math.Sin(float64(c)*1.1) * 10.0
		y := math.Mod(float64(w.frame)*speed+offset, 8.0)

		for r := 0; r < 2; r++ {
			dist := y - float64(r)
			var ch rune
			var col string
			bold := false

			if dist >= 0 && dist < 3.0 {
				fade := 1.0 - (dist / 3.0)
				ch = randomRainRune(w.rng)
				if dist < 0.5 {
					col = t.Text
					bold = true
				} else {
					col = lerpColor(t.Surface, t.Success, fade)
				}
			} else {
				ch = ' '
			}

			st := bgStyle(t).Foreground(lipgloss.Color(col))
			if bold {
				st = st.Bold(true)
			}
			lines[r].WriteString(st.Render(string(ch)))
		}
	}
	return lines[0].String() + "\n" + lines[1].String()
}

// ─────────────────────────────────────────────
// 8. Braille Wave (new) — replaces DNA Weave, which read as "random
// letters" scrolling by since its strand characters (A/C/G/T) are real
// alphabetic glyphs that look like garbled text at a glance. Braille
// dot-pattern characters don't spell anything, so the same "shimmering
// texture" idea reads as abstract activity instead.
// ─────────────────────────────────────────────

var brailleRunes = []rune("⠁⠂⠄⡀⢀⠠⠐⠈⠃⠅⠘⠨⠰⣀⡄⢠⠸⡇⢸⣆⣇⣷⣾⣿")

func (w *workingAnimState) renderBrailleWave(t theme.Theme, width int) string {
	frameVal := float64(w.frame) * 0.08
	var line1, line2 strings.Builder

	for i := 0; i < width; i++ {
		phase := float64(i)/float64(width)*math.Pi*4 - frameVal
		v1 := (math.Sin(phase) + 1) / 2
		v2 := (math.Sin(phase*1.3+math.Pi/3) + 1) / 2

		idx1 := int(v1 * float64(len(brailleRunes)-1))
		idx2 := int(v2 * float64(len(brailleRunes)-1))

		line1.WriteString(bgStyle(t).Foreground(lipgloss.Color(lerpColor(t.Surface, t.Accent, v1))).Render(string(brailleRunes[idx1])))
		line2.WriteString(bgStyle(t).Foreground(lipgloss.Color(lerpColor(t.Surface, t.AccentMuted, v2))).Render(string(brailleRunes[idx2])))
	}
	return line1.String() + "\n" + line2.String()
}

// ─────────────────────────────────────────────
// 9. Radar Sweep (new) — replaces Thought Stream, which typed out a
// fixed, hardcoded set of "reasoning" fragments regardless of what the
// agent was actually doing — it read as scripted because it was. This
// is an honest substitute in the same "something is scanning/searching"
// spirit as Cylon Scanner, but a one-directional wraparound sweep with
// a fading trail instead of a back-and-forth bounce, so the two don't
// feel redundant sitting side by side in the picker.
// ─────────────────────────────────────────────

func (w *workingAnimState) renderRadarSweep(t theme.Theme, width int) string {
	pos := w.frame % width
	trailLen := 10

	var sb strings.Builder
	for i := 0; i < width; i++ {
		d := pos - i
		if d < 0 {
			d += width
		}
		switch {
		case d == 0:
			sb.WriteString(bgStyle(t).Bold(true).Foreground(lipgloss.Color(t.Text)).Render("▐"))
		case d < trailLen:
			fade := 1 - float64(d)/float64(trailLen)
			sb.WriteString(bgStyle(t).Foreground(lipgloss.Color(lerpColor(t.Surface, t.Accent, fade))).Render("·"))
		default:
			sb.WriteString(bgStyle(t).Foreground(lipgloss.Color(t.Highlight)).Render("─"))
		}
	}
	return sb.String()
}

// ─────────────────────────────────────────────
// 10. Slash Trail (new) — a repeating "/" pattern scrolling continuously
// across two lines. The bottom row's slashes sit one column *behind*
// the top row's — column i on the bottom lines up with column i+1 on
// top — rather than directly underneath, so each "/" glyph's own
// diagonal (bottom-left to top-right within its cell) visually
// continues into the next row's glyph instead of reading as two
// unrelated rows of slashes.
// ─────────────────────────────────────────────

const slashTrailPeriod = 6

func (w *workingAnimState) renderSlashTrail(t theme.Theme, width int) string {
	offset := (w.frame / 2) % slashTrailPeriod
	blank := bgStyle(t).Render(" ")

	var top, bottom strings.Builder
	for i := 0; i < width; i++ {
		col := lerpColor(t.AccentMuted, t.Accent, (math.Sin(float64(i)*0.3+float64(w.frame)*0.05)+1)/2)

		if (i+offset)%slashTrailPeriod == 0 {
			top.WriteString(bgStyle(t).Foreground(lipgloss.Color(col)).Render("/"))
		} else {
			top.WriteString(blank)
		}

		if (i+1+offset)%slashTrailPeriod == 0 {
			bottom.WriteString(bgStyle(t).Foreground(lipgloss.Color(col)).Render("/"))
		} else {
			bottom.WriteString(blank)
		}
	}
	return top.String() + "\n" + bottom.String()
}
