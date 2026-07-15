package ui

import (
	"tui-testing/internal/theme"

	"charm.land/lipgloss/v2"
)

// BootInfo backs the boot banner. Seeded at NewApp time, then kept in
// sync afterward — Model/Specialists on every successful backend
// reconnect (see the keySetMsg handler in Update), Theme on every theme
// change (see applyTheme) — so it reflects current reality rather than
// being a frozen record of launch conditions. Not currently rendered
// (see renderBootArt, which just prints ascii art for now) — kept live
// regardless, since showing it again (alongside or instead of the art)
// is a likely next step, not a hypothetical one.
type BootInfo struct {
	Model       string // "" if unknown
	Theme       string
	Specialists []string // sub-agents discovered at startup; nil/empty if none
}

// bootArtGlyphs is a tiny 5-row block font, just enough for the letters
// bootArtWord actually spells — not a general-purpose typeface. '#' is a
// filled cell, anything else is empty; every glyph is 5 columns wide.
var bootArtGlyphs = map[rune][5]string{
	'A': {
		".###.",
		"#...#",
		"#####",
		"#...#",
		"#...#",
	},
	'G': {
		".###.",
		"#....",
		"#.###",
		"#...#",
		".###.",
	},
	'E': {
		"#####",
		"#....",
		"###..",
		"#....",
		"#####",
	},
	'N': {
		"#...#",
		"##..#",
		"#.#.#",
		"#..##",
		"#...#",
	},
	'T': {
		"#####",
		"..#..",
		"..#..",
		"..#..",
		"..#..",
	},
}

// bootArtWord is what the boot banner spells — hardcoded for now.
const bootArtWord = "AGENT"

// bootArtLines renders word through bootArtGlyphs into 5 lines of plain
// text (no color yet — renderBootArt styles each line as a whole).
// Every glyph cell is doubled horizontally ("██"/"  ") since a terminal
// cell is roughly twice as tall as it is wide — a 1:1 glyph would come
// out looking thin and squashed rather than square. An unrecognized rune
// (anything outside bootArtGlyphs) is silently skipped rather than
// erroring, since this is only ever fed the hardcoded word above.
func bootArtLines(word string) []string {
	lines := make([]string, 5)
	for _, r := range word {
		glyph, ok := bootArtGlyphs[r]
		if !ok {
			continue
		}
		for row := range 5 {
			for _, cell := range glyph[row] {
				if cell == '#' {
					lines[row] += "██"
				} else {
					lines[row] += "  "
				}
			}
			lines[row] += "  " // gap before the next letter
		}
	}
	return lines
}

// renderBootArt draws the boot banner: bootArtWord in block letters,
// centered, printed once as the transcript's first entry. No border or
// panel — just the art on the app's ordinary background.
func renderBootArt(s theme.Styles, width int) string {
	lines := bootArtLines(bootArtWord)
	styled := make([]string, len(lines))
	for i, line := range lines {
		styled[i] = s.BootArt.Render(line)
	}
	art := lipgloss.JoinVertical(lipgloss.Left, styled...)

	whitespace := lipgloss.NewStyle().Background(lipgloss.Color(s.Theme.Background))
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, art, lipgloss.WithWhitespaceStyle(whitespace))
}
