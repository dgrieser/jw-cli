package render

// ANSI sequences for the parts of a listing. Weight and faintness carry
// structure and read the same in any palette; the hues are the part NO_COLOR
// and --no-color turn off.
// OSC 8 is the hyperlink sequence: an opening one carrying the target, the
// text, then an empty one closing it.
const (
	osc8   = "\x1b]8;;"
	oscEnd = "\x1b\\"
)

const (
	ansiFaint  = "\x1b[2m"
	ansiCyan   = "\x1b[36m"
	ansiBlue   = "\x1b[34m"
	ansiYellow = "\x1b[1;33m"
	ansiPurple = "\x1b[1;35m"
)

// TextStyle marks the parts of a plain-text listing up for a terminal: the
// title of a result, the publication it comes from, the words the search
// matched. The zero value writes nothing at all, which is what a pipe, a file,
// -o raw and --no-color get — their output stays exactly what it was.
type TextStyle struct {
	on bool
}

// NewTextStyle turns styling on for an interactive terminal. color is the
// caller's own choice (--no-color); NO_COLOR overrides it either way. Both
// switch the weights off along with the hues, the way ToTerminal does: a
// listing asked for plain output is plain.
func NewTextStyle(styled, color bool) TextStyle {
	return TextStyle{on: styled && color && !noColorEnv()}
}

// On reports whether anything is written at all.
func (t TextStyle) On() bool { return t.on }

// Strong is what a row is about: the title of a result.
func (t TextStyle) Strong(s string) string { return t.wrap(t.strongSeq(), s) }

// Faint is what a row needs but nobody reads twice: its number, the
// publication line, the link.
func (t TextStyle) Faint(s string) string { return t.wrap(t.faintSeq(), s) }

// Italic is a title inside a sentence, and whatever else the page stressed.
func (t TextStyle) Italic(s string) string { return t.wrap(t.italicSeq(), s) }

// Accent names a listing's fixed vocabulary — the kind of a result, the labels
// on an image's metadata.
func (t TextStyle) Accent(s string) string { return t.wrap(t.accentSeq(), s) }

// Mark is the hit itself inside a passage: the words a search matched.
func (t TextStyle) Mark(s string) string { return t.wrap(t.markSeq(), s) }

// Link makes text clickable through OSC 8, the way ToTerminal does for the
// links inside a document, so a listing does not have to spell its targets out
// on lines of their own. Nothing is written where styling is off — a pipe, a
// file and --no-color get the target printed instead.
func (t TextStyle) Link(text, url string) string {
	if !t.on || text == "" || url == "" {
		return text
	}
	return osc8 + url + oscEnd + text + osc8 + oscEnd
}

func (t TextStyle) strongSeq() string { return t.escape(ansiBold) }
func (t TextStyle) faintSeq() string  { return t.escape(ansiFaint) }
func (t TextStyle) italicSeq() string { return t.escape(ansiItalic) }
func (t TextStyle) accentSeq() string { return t.escape(t.hueFor(ansiCyan, ansiBlue)) }
func (t TextStyle) markSeq() string   { return t.escape(t.hueFor(ansiYellow, ansiPurple)) }

// hueFor picks the readable one of two colors for the terminal's background.
// Asking costs a round trip to the terminal, so it is only asked when
// something is going to be colored.
func (t TextStyle) hueFor(dark, light string) string {
	if !t.on || DarkBackground() {
		return dark
	}
	return light
}

func (t TextStyle) escape(seq string) string {
	if !t.on {
		return ""
	}
	return seq
}

func (t TextStyle) wrap(escape, s string) string {
	if escape == "" || s == "" {
		return s
	}
	return escape + s + ansiReset
}
