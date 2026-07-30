package render

import (
	"os"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"golang.org/x/term"
)

// Terminal rendering width bounds. Long lines are hard to read, very narrow
// ones break indented blocks, so the detected width is clamped.
const (
	defaultWidth = 80
	minWidth     = 40
	maxWidth     = 100
)

// TerminalOptions controls how markdown is styled for a terminal.
type TerminalOptions struct {
	// Width is the word-wrap column. Zero means "detect from the terminal".
	Width int
	// NoColor renders the document structure without ANSI colors. The NO_COLOR
	// environment variable forces this on.
	NoColor bool
}

// ToTerminal renders markdown for display in a terminal: headings, lists,
// emphasis, block quotes, and code blocks get ANSI styling and the text is
// wrapped at the terminal width. Rendering never fails the caller — on any
// error the markdown is returned unchanged.
func ToTerminal(md string, o TerminalOptions) string {
	width := o.Width
	if width <= 0 {
		width = TerminalWidth(os.Stdout)
	}
	// AutoStyle picks dark or light from the terminal background, and falls back
	// to the unstyled layout on its own when stdout is not a terminal.
	style := styles.AutoStyle
	if o.NoColor || noColorEnv() {
		style = styles.NoTTYStyle
	}
	return toTerminal(md, style, width)
}

func toTerminal(md, style string, width int) string {
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(width),
		glamour.WithEmoji(),
	)
	if err != nil {
		return md
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	// Glamour pads the document with blank lines; the writer adds its own.
	return strings.Trim(out, "\n")
}

// TerminalWidth returns the clamped column count of the terminal backing f, or
// the default width when f is nil, not a terminal, or its size is unavailable.
func TerminalWidth(f *os.File) int {
	if f == nil {
		return defaultWidth
	}
	w, _, err := term.GetSize(int(f.Fd()))
	if err != nil || w <= 0 {
		return defaultWidth
	}
	return ClampWidth(w)
}

// ClampWidth clamps a raw terminal column count to the rendering bounds.
func ClampWidth(w int) int {
	return min(max(w, minWidth), maxWidth)
}

// IsTerminal reports whether w is an interactive terminal.
func IsTerminal(w any) bool {
	f, ok := w.(*os.File)
	return ok && f != nil && term.IsTerminal(int(f.Fd()))
}

// noColorEnv reports whether the NO_COLOR convention asks for plain output.
func noColorEnv() bool {
	v, set := os.LookupEnv("NO_COLOR")
	return set && v != ""
}
