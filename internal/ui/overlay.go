package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// overlay composites fg on top of bg at column x, row y (0-indexed,
// top-left origin), splicing each fg line into the corresponding bg line.
// lipgloss has no built-in layered rendering, so this is the standard
// ansi.Cut-based technique for it: cut the background around the overlay's
// column range (ANSI- and wide-rune-aware, so styling on either side of the
// splice stays intact) and stitch the overlay line in between.
func overlay(bg, fg string, x, y, width int) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")

	for i, fgLine := range fgLines {
		row := y + i
		if row < 0 || row >= len(bgLines) {
			continue
		}

		bgLine := padLine(bgLines[row], width)
		fgWidth := lipgloss.Width(fgLine)

		left := ansi.Cut(bgLine, 0, x)
		right := ansi.Cut(bgLine, x+fgWidth, width)
		bgLines[row] = left + fgLine + right
	}

	return strings.Join(bgLines, "\n")
}

// padLine right-pads a line with spaces so ansi.Cut has real content to cut
// out to width, even past whatever the line's natural (unstyled) length is.
func padLine(line string, width int) string {
	w := lipgloss.Width(line)
	if w >= width {
		return line
	}
	return line + strings.Repeat(" ", width-w)
}
