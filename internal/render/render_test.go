package render

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"charm.land/glamour/v2/styles"
)

const sample = `
<div class="bodyTxt">
  <h2>Subheading</h2>
  <p id="p2" data-pid="2">Faith moves mountains. See
    <a href="/en/wol/bc/r1/lp-e/123/1/0" data-bid="1-1" class="b">Matt 17:20</a>.
  </p>
  <figure>
    <span class="jsRespImg" data-img-att-alt="A mountain"
          data-img-size-sm="https://cdn.example/img_sm.jpg"
          data-img-size-lg="https://cdn.example/img_lg.jpg"
          data-zoom="https://cdn.example/img_xl.jpg">
      <noscript><img src="https://cdn.example/img_xs.jpg" alt="A mountain"/></noscript>
    </span>
    <figcaption>A mountain range</figcaption>
  </figure>
  <ul><li>First point</li><li>Second point</li></ul>
  <script>alert("nope")</script>
</div>`

func TestRenderMarkdown(t *testing.T) {
	out, err := Render(sample, Markdown, Options{BaseURL: "https://wol.jw.org"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Subheading",
		"[Matt 17:20](https://wol.jw.org/en/wol/bc/r1/lp-e/123/1/0)",
		"![A mountain](https://cdn.example/img_xl.jpg)",
		"- First point",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "alert") {
		t.Error("script content leaked into markdown")
	}
}

func TestRenderText(t *testing.T) {
	out, err := Render(sample, Text, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Subheading", "Faith moves mountains", "Matt 17:20", "- First point", "[image: A mountain]"} {
		if !strings.Contains(out, want) {
			t.Errorf("text missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "<") {
		t.Errorf("html leaked into text output:\n%s", out)
	}
	if strings.Contains(out, "\n\n\n") {
		t.Error("excess blank lines")
	}
}

func TestRenderHTML(t *testing.T) {
	out, err := Render(sample, HTML, Options{BaseURL: "https://wol.jw.org"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `href="https://wol.jw.org/en/wol/bc/r1/lp-e/123/1/0"`) {
		t.Errorf("href not absolutized:\n%s", out)
	}
	if !strings.Contains(out, `<img src="https://cdn.example/img_xl.jpg"`) {
		t.Errorf("jsRespImg not materialized:\n%s", out)
	}
	if strings.Contains(out, "script") {
		t.Error("script not removed")
	}
}

func TestParseFormat(t *testing.T) {
	for in, want := range map[string]Format{
		"": Markdown, "md": Markdown, "markdown": Markdown, "raw": Raw,
		"html": HTML, "text": Text, "txt": Text, "json": JSON,
	} {
		got, err := ParseFormat(in)
		if err != nil || got != want {
			t.Errorf("ParseFormat(%q) = %v, %v", in, got, err)
		}
		if got.String() != want.String() {
			t.Errorf("ParseFormat(%q).String() = %q", in, got.String())
		}
	}
	if _, err := ParseFormat("yaml"); err == nil {
		t.Error("want error for invalid format")
	}
}

// TestRenderRaw pins -o raw to the same markdown body as -o markdown; only the
// terminal styling applied afterwards differs.
func TestRenderRaw(t *testing.T) {
	md, err := Render(sample, Markdown, Options{BaseURL: "https://wol.jw.org"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Render(sample, Raw, Options{BaseURL: "https://wol.jw.org"})
	if err != nil {
		t.Fatal(err)
	}
	if raw != md {
		t.Errorf("raw differs from markdown:\n%q\n%q", raw, md)
	}
	if !Raw.IsMarkdown() || !Markdown.IsMarkdown() {
		t.Error("Raw and Markdown must both report IsMarkdown")
	}
	for _, f := range []Format{HTML, Text, JSON} {
		if f.IsMarkdown() {
			t.Errorf("%v must not report IsMarkdown", f)
		}
	}
}

const markdownDoc = "# Title\n\nSome *emphasis* and a [link](https://example.com/a).\n\n- one\n- two\n"

func TestExpandBareLinks(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"autolink", "see <https://a.example/x> here",
			"see [https://a.example/x](https://a.example/x) here"},
		{"bare url", "see https://a.example/x here",
			"see [https://a.example/x](https://a.example/x) here"},
		{"bare url keeps trailing period", "see https://a.example/x.",
			"see [https://a.example/x](https://a.example/x)."},
		{"inline link untouched", "see [text](https://a.example/x) here",
			"see [text](https://a.example/x) here"},
		{"image untouched", "![alt](https://a.example/i.jpg)",
			"![alt](https://a.example/i.jpg)"},
		{"nested brackets in link text", "[a [b] c](https://a.example/x)",
			"[a [b] c](https://a.example/x)"},
		{"code span untouched", "run `curl https://a.example/x` now",
			"run `curl https://a.example/x` now"},
		{"non-url angle brackets untouched", "run `jw download <n>` now",
			"run `jw download <n>` now"},
		{"mixed", "[v](https://a.example/1) and <https://b.example/2>",
			"[v](https://a.example/1) and [https://b.example/2](https://b.example/2)"},
	} {
		if got := expandBareLinks(tc.in); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
}

// TestToTerminalHidesLinkTargets pins the reading experience: an inline link
// shows only its text, hyperlinked with OSC 8, while a URL that is its own text
// stays visible. A spelled-out target next to its text is the glamour default
// this deliberately suppresses.
func TestToTerminalHidesLinkTargets(t *testing.T) {
	const url = "https://wol.jw.org/de/wol/bc/r10/lp-x/1001070144/839"
	md := "Verse [14](" + url + ") of the chapter.\n\nSource: <" + url + ">\n"
	out := toTerminal(md, styles.DarkStyle, 200)

	// both links are hyperlinked: two OSC 8 opening sequences carrying the URL
	if got := strings.Count(out, "\x1b]8;id="); got != 2 {
		t.Errorf("expected 2 OSC 8 hyperlinks, got %d:\n%q", got, out)
	}
	if !strings.Contains(out, ";"+url+"\x07") {
		t.Errorf("hyperlink does not carry the URL:\n%q", out)
	}

	// spacing is exact: no gap left where the target was, and none inserted
	// between the link text and the punctuation that follows it
	if raw := ansiSeq.ReplaceAllString(out, ""); !strings.Contains(raw, "Verse 14 of the chapter.") {
		t.Errorf("spacing around the hidden target is wrong:\n%q", raw)
	}
	visible := visibleText(out)
	if strings.Count(visible, url) != 1 {
		t.Errorf("URL should appear once (the autolink), not next to the anchor:\n%q", visible)
	}
	// the former autolink keeps the URL as its visible, clickable text
	if !strings.Contains(visible, "Source: "+url) {
		t.Errorf("autolink target lost:\n%q", visible)
	}

	// a link followed directly by punctuation must not gain a space
	punct := toTerminal("See [Matt 17:20]("+url+"), then stop.", styles.DarkStyle, 200)
	if got := visibleText(punct); !strings.Contains(got, "See Matt 17:20, then stop.") {
		t.Errorf("space inserted before punctuation:\n%q", got)
	}
}

// ansiSeq matches SGR sequences and OSC 8 hyperlink sequences (BEL-terminated,
// which is what glamour emits).
var ansiSeq = regexp.MustCompile("\x1b\\[[0-9;]*m|\x1b\\]8;[^\x07]*\x07")

// visibleText reduces rendered output to what a reader sees: escapes removed and
// runs of whitespace collapsed, since glamour pads lines out to the wrap width.
func visibleText(s string) string {
	return strings.Join(strings.Fields(ansiSeq.ReplaceAllString(s, "")), " ")
}

// TestToTerminalStyles exercises the styled path with a fixed style: glamour's
// auto style degrades to the unstyled layout when stdout is not a terminal, so
// a test process would never see ANSI otherwise.
func TestToTerminalStyles(t *testing.T) {
	out := toTerminal(markdownDoc, styles.DarkStyle, 60)
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("expected ANSI styling:\n%q", out)
	}
	visible := visibleText(out)
	if strings.Contains(visible, "](") {
		t.Errorf("link markup not rendered:\n%q", visible)
	}
	for _, want := range []string{"Title", "emphasis", "link", "one", "two"} {
		if !strings.Contains(visible, want) {
			t.Errorf("styled output missing %q:\n%s", want, visible)
		}
	}
	if strings.Contains(visible, "https://example.com/a") {
		t.Errorf("inline link target should not be visible:\n%q", visible)
	}
	if strings.HasPrefix(out, "\n") || strings.HasSuffix(out, "\n") {
		t.Errorf("output should be trimmed of blank lines:\n%q", out)
	}
}

func TestToTerminalWraps(t *testing.T) {
	const width = 40
	long := "# T\n\n" + strings.Repeat("wrap me please ", 30)
	for line := range strings.SplitSeq(toTerminal(long, styles.NoTTYStyle, width), "\n") {
		if len([]rune(line)) > width {
			t.Fatalf("line exceeds width %d (%d): %q", width, len([]rune(line)), line)
		}
	}
}

func TestToTerminalNoColor(t *testing.T) {
	out := ToTerminal(markdownDoc, TerminalOptions{Width: 60, NoColor: true})
	if strings.Contains(out, "\x1b[") {
		t.Errorf("NoColor output must not contain ANSI escapes:\n%q", out)
	}
	if !strings.Contains(out, "Title") {
		t.Errorf("NoColor output missing content:\n%s", out)
	}
}

func TestToTerminalNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if out := ToTerminal(markdownDoc, TerminalOptions{Width: 60}); strings.Contains(out, "\x1b[") {
		t.Errorf("NO_COLOR must disable ANSI escapes:\n%q", out)
	}
}

func TestClampWidth(t *testing.T) {
	for in, want := range map[int]int{0: minWidth, 10: minWidth, 72: 72, 500: maxWidth} {
		if got := ClampWidth(in); got != want {
			t.Errorf("ClampWidth(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestIsTerminal(t *testing.T) {
	// a temp file is a valid *os.File that is not a terminal
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if IsTerminal(f) {
		t.Error("a regular file is not a terminal")
	}
	if IsTerminal(new(strings.Builder)) {
		t.Error("a non-file writer is not a terminal")
	}
	if got := TerminalWidth(f); got != defaultWidth {
		t.Errorf("TerminalWidth(file) = %d, want %d", got, defaultWidth)
	}
}
