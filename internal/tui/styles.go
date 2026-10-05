package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Colors come from the terminal's 16-color ANSI palette, so lazy1mcp follows the terminal theme.
var (
	cRed     = lipgloss.Color("1")
	cGreen   = lipgloss.Color("2")
	cYellow  = lipgloss.Color("3")
	cMagenta = lipgloss.Color("5")
	cCyan    = lipgloss.Color("6")
	cGray    = lipgloss.Color("8")

	cAccent = cCyan

	dimStyle    = lipgloss.NewStyle().Faint(true)
	boldStyle   = lipgloss.NewStyle().Bold(true)
	accentStyle = lipgloss.NewStyle().Foreground(cAccent)
	goodStyle   = lipgloss.NewStyle().Foreground(cGreen)
	badStyle    = lipgloss.NewStyle().Foreground(cRed)
	warnStyle   = lipgloss.NewStyle().Foreground(cYellow)
	keyStyle    = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	labelStyle  = lipgloss.NewStyle().Faint(true)
	appName     = lipgloss.NewStyle().Bold(true).Foreground(cMagenta)
	tagStyle    = lipgloss.NewStyle().Foreground(cMagenta)
)

// selectedBG is the SGR sequence of the selected-row background (ANSI bright black).
const selectedBG = "\x1b[100m"

// width is the display width (grapheme aware, ANSI escapes ignored).
func width(s string) int { return ansi.StringWidth(s) }

// fit truncates s to w cells (with an ellipsis) and pads it with spaces to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if width(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if d := w - width(s); d > 0 {
		s += strings.Repeat(" ", d)
	}
	return s
}

// highlight paints a fitted line with the selection background, surviving inner SGR resets.
func highlight(s string) string {
	return selectedBG + strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m"+selectedBG) + "\x1b[0m"
}

// tildify shows paths under $HOME with ~.
func tildify(p string) string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" || h == "/" {
		return p
	}
	if p == h {
		return "~"
	}
	if strings.HasPrefix(p, h+string(filepath.Separator)) {
		return "~" + p[len(h):]
	}
	return p
}

// san makes external text safe to print on one line: ANSI escapes removed, control characters
// replaced, only the first line kept.
func san(s string) string {
	s = ansi.Strip(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return '?'
		}
		return r
	}, s)
}

// box draws a rounded box of exactly w×h cells with a title in the top border.
func box(title string, lines []string, w, h int, focused bool) []string {
	bc := lipgloss.NewStyle().Foreground(cGray)
	if focused {
		bc = lipgloss.NewStyle().Foreground(cAccent)
	}
	inner := w - 4
	if inner < 1 || h < 2 {
		return nil
	}
	t := ""
	if title != "" {
		t = " " + title + " "
		if width(t) > w-4 {
			t = ansi.Truncate(t, w-4, "…")
		}
	}
	out := []string{bc.Render("╭─") + t + bc.Render(strings.Repeat("─", max(0, w-3-width(t)))+"╮")}
	for i := 0; i < h-2; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out = append(out, bc.Render("│")+" "+fit(l, inner)+" "+bc.Render("│"))
	}
	return append(out, bc.Render("╰"+strings.Repeat("─", w-2)+"╯"))
}

// overlay draws the block fg centered over the screen lines bg.
func overlay(bg []string, fg []string, w int) []string {
	fw := 0
	for _, l := range fg {
		fw = max(fw, width(l))
	}
	x := max(0, (w-fw)/2)
	y := max(0, (len(bg)-len(fg))/2)
	out := append([]string(nil), bg...)
	for i, l := range fg {
		if y+i >= len(out) {
			break
		}
		b := fit(out[y+i], w)
		out[y+i] = ansi.Truncate(b, x, "") + "\x1b[0m" + fit(l, fw) + "\x1b[0m" + ansi.TruncateLeft(b, x+fw, "")
	}
	return out
}
