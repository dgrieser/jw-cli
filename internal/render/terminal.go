package render

import (
	"os"
	"regexp"
	"strings"
	"sync"

	"charm.land/glamour/v2"
	glamourstyle "charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
// wrapped at the terminal width. Link and image targets become OSC 8 hyperlinks
// on the text itself instead of being spelled out. Rendering never fails the
// caller — on any error the markdown is returned unchanged.
func ToTerminal(md string, o TerminalOptions) string {
	width := o.Width
	if width <= 0 {
		width = TerminalWidth(os.Stdout)
	}
	style := styles.NoTTYStyle
	if !o.NoColor && !noColorEnv() {
		style = DetectStyle()
	}
	return toTerminal(md, style, width)
}

// DetectStyle picks the glamour style matching the terminal: the unstyled layout
// when stdout is not a terminal, otherwise dark or light by background color.
// GLAMOUR_STYLE overrides the choice by name (dark, light, ascii, notty, ...).
//
// Detecting asks the terminal for its background color and briefly switches
// stdin to raw mode, so the answer is resolved once per process. Call this
// before handing the terminal to something else that reads keys.
func DetectStyle() string { return detectStyle() }

var detectStyle = sync.OnceValue(func() string {
	if name := os.Getenv("GLAMOUR_STYLE"); name != "" {
		if _, ok := styles.DefaultStyles[name]; ok {
			return name
		}
	}
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return styles.NoTTYStyle
	}
	// Lip Gloss pairs the background-color request with a device-attributes
	// request, so a terminal that ignores the former still answers the latter
	// and the query returns at once; it falls back to dark on error or timeout.
	if lipgloss.HasDarkBackground(os.Stdin, os.Stdout) {
		return styles.DarkStyle
	}
	return styles.LightStyle
})

func toTerminal(md, style string, width int) string {
	cfg, ok := styles.DefaultStyles[style]
	if !ok || cfg == nil {
		return md
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(styleOverrides(*cfg)),
		glamour.WithWordWrap(width),
		glamour.WithEmoji(),
	)
	if err != nil {
		return md
	}
	out, err := r.Render(expandBareLinks(md))
	if err != nil {
		return md
	}
	// Glamour pads the document with blank lines; the writer adds its own.
	return strings.Trim(dropHiddenTargetSpace(out), "\n")
}

// hiddenTargetSpace matches the separator glamour writes between a link's text
// and its target: an end-of-hyperlink, optional color codes, then one space.
var hiddenTargetSpace = regexp.MustCompile("(\x1b\\]8;;\x07)((?:\x1b\\[[0-9;]*m)*) ")

// dropHiddenTargetSpace removes that separator. The target renders to nothing
// here, so the space is left dangling: it doubles up before the next word and,
// worse, detaches the link text from punctuation ("Matt 17:20 ." for
// "[Matt 17:20](url)."). Glamour hardcodes it, so it goes after the fact.
func dropHiddenTargetSpace(s string) string {
	return hiddenTargetSpace.ReplaceAllString(s, "$1$2")
}

// blankFormat is a template that renders to nothing. Glamour always writes a
// link's target next to its text and offers no switch for it, but it runs the
// target through this format template first — so blanking the template drops
// the spelled-out URL while keeping the OSC 8 hyperlink on the text.
const blankFormat = `{{""}}`

// styleOverrides returns cfg with this project's two departures from the stock
// glamour styles applied.
func styleOverrides(cfg glamourstyle.StyleConfig) glamourstyle.StyleConfig {
	// link and image targets are suppressed: only the target is dropped, the
	// text stays styled and stays a clickable hyperlink
	cfg.Link.Format = blankFormat
	cfg.Image.Format = blankFormat
	// glamour draws a thematic break as eight hyphens; keep the three the
	// markdown source uses, so the styled output and -o raw agree
	cfg.HorizontalRule.Format = "\n" + horizontalRule + "\n"
	return cfg
}

// mdLink matches, in one left-to-right pass, every markdown construct that can
// hold a URL. Earlier alternatives win, so a link's own target can never be
// re-matched by the bare-URL alternative that follows.
var mdLink = regexp.MustCompile(
	"`[^`\n]*`" + // code span: never rewritten
		`|!?\[[^\]]*\]\([^)\s]*\)` + // inline link or image
		`|\]\([^)\s]*\)` + // target of a link whose text has nested brackets
		`|<https?://[^\s>]+>` + // autolink
		`|https?://[^\s<>()\[\]]+`) // bare URL

// trailingPunct is punctuation a bare URL must not swallow; GFM's linkify drops
// it too, so the two agree on where the URL ends.
const trailingPunct = ".,;:!?'\""

// expandBareLinks rewrites autolinks and bare URLs into the inline [text](url)
// form. Autolinks and bare URLs are rendered target-only, and the terminal
// renderer hides targets — without this they would come out empty. As inline
// links they keep their URL as visible, clickable text.
func expandBareLinks(md string) string {
	return mdLink.ReplaceAllStringFunc(md, func(m string) string {
		switch {
		case strings.HasPrefix(m, "`"), strings.HasPrefix(m, "["),
			strings.HasPrefix(m, "!["), strings.HasPrefix(m, "]("):
			return m // already carries its own text, or must not be touched
		case strings.HasPrefix(m, "<"):
			return inlineLink(m[1 : len(m)-1])
		}
		url := strings.TrimRight(m, trailingPunct)
		return inlineLink(url) + m[len(url):]
	})
}

func inlineLink(url string) string { return "[" + url + "](" + url + ")" }

// TerminalWidth returns the clamped column count of the terminal backing w, or
// the default width when w is not a terminal or its size is unavailable.
func TerminalWidth(w any) int {
	f, ok := w.(*os.File)
	if !ok || f == nil {
		return defaultWidth
	}
	cols, _, err := term.GetSize(int(f.Fd()))
	if err != nil || cols <= 0 {
		return defaultWidth
	}
	return ClampWidth(cols)
}

// minWrapWidth is the narrowest column budget worth wrapping into; below it an
// indented block would be shredded into single words.
const minWrapWidth = 20

// WrapIndent wraps s to width columns and indents every line after the first
// with indent, counting the indent against the width. ANSI escapes and wide
// characters do not distort the column count. The caller writes the indent for
// the first line. s is returned unwrapped when width leaves no useful room,
// which is how callers disable wrapping for pipes and files.
func WrapIndent(s, indent string, width int) string {
	limit := width - StringWidth(indent)
	if s == "" || limit < minWrapWidth {
		return s
	}
	return strings.ReplaceAll(ansi.Wrap(s, limit, ""), "\n", "\n"+indent)
}

// StringWidth is the printable column width of s, ignoring ANSI escapes.
func StringWidth(s string) int { return ansi.StringWidth(s) }

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
