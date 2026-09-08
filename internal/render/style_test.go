package render

import (
	"strings"
	"testing"
)

const esc = "\x1b"

func TestStyledTextOff(t *testing.T) {
	frag := `<p>Warum heißt es in <span class="mk"><em>Jeremia</em> 31:15</span>?</p>`
	off, err := StyledText(frag, Options{}, TextStyle{})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Render(frag, Text, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if off != plain {
		t.Errorf("styled-off text differs from plain text:\n%q\n%q", off, plain)
	}
	if strings.Contains(off, esc) {
		t.Errorf("styling written with the style off: %q", off)
	}
}

// --no-color and NO_COLOR mean plain, weights included.
func TestNewTextStyleColorOff(t *testing.T) {
	if NewTextStyle(true, false).On() {
		t.Error("--no-color still styles")
	}
	if NewTextStyle(false, true).On() {
		t.Error("a pipe still styles")
	}
	if !NewTextStyle(true, true).On() {
		t.Error("a terminal is not styled")
	}
}

// wol marks what a search matched; that mark is what has to stand out in a
// passage, and an emphasis inside it must not end it early.
func TestStyledTextMarksTheHit(t *testing.T) {
	style := NewTextStyle(true, true)
	out, err := StyledText(
		`<p>In <span class="mk">Jeremia <em>31:15</em></span> lesen wir.</p>`,
		Options{}, style)
	if err != nil {
		t.Fatal(err)
	}
	mark := style.markSeq()
	if !strings.HasPrefix(out, "In "+mark+"Jeremia ") {
		t.Errorf("hit not marked: %q", out)
	}
	// the italic inside resets, so the mark is written again behind it
	if !strings.Contains(out, ansiReset+mark) {
		t.Errorf("mark not restored after the nested emphasis: %q", out)
	}
	if !strings.HasSuffix(strings.TrimSpace(out), ansiReset+" lesen wir.") {
		t.Errorf("mark not closed where the span ends: %q", out)
	}
}

func TestTextStyleParts(t *testing.T) {
	style := NewTextStyle(true, true)
	for name, got := range map[string]string{
		"Strong": style.Strong("x"),
		"Faint":  style.Faint("x"),
		"Italic": style.Italic("x"),
		"Accent": style.Accent("x"),
		"Mark":   style.Mark("x"),
	} {
		if !strings.HasPrefix(got, esc) || !strings.HasSuffix(got, ansiReset) {
			t.Errorf("%s = %q, want an escape around the text", name, got)
		}
	}
	var off TextStyle
	if got := off.Strong("x") + off.Accent("x"); got != "xx" {
		t.Errorf("zero style wrote %q", got)
	}
}

func TestTruncateKeepsEscapes(t *testing.T) {
	s := "\x1b[1mabcdef\x1b[0m"
	if got := Truncate(s, 10); got != s {
		t.Errorf("short enough but cut: %q", got)
	}
	got := Truncate(s, 3)
	if StringWidth(got) > 4 { // 3 plus the ellipsis
		t.Errorf("truncated to %d columns: %q", StringWidth(got), got)
	}
	// the ellipsis lands inside the styling, before its reset
	if !strings.Contains(got, "…") {
		t.Errorf("no mark that it was cut: %q", got)
	}
}
