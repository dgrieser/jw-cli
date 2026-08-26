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

func TestWrapIndent(t *testing.T) {
	const indent = "     " // 5 columns, as the result listings use
	// width 25 leaves a 20-column budget per line
	got := WrapIndent("one two three four five six", indent, 25)
	want := "one two three four\n     five six"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// every line, indent included, has to fit the width
	for line := range strings.SplitSeq(indent+got, "\n") {
		if StringWidth(line) > 25 {
			t.Errorf("line exceeds width: %q (%d)", line, StringWidth(line))
		}
	}
}

func TestWrapIndentCountsColumnsNotBytes(t *testing.T) {
	const indent = "  "
	// the bold escapes and the multi-byte umlauts must not shift the wrap point
	styled := WrapIndent("Das \x1b[1mKönigreich\x1b[0m und die Herrschaft", indent, 22)
	plain := WrapIndent("Das Koenigreich und die Herrschaft", indent, 22)
	if strings.Count(styled, "\n") != strings.Count(plain, "\n") {
		t.Errorf("escape codes changed the wrapping:\nstyled %q\nplain  %q", styled, plain)
	}
	if !strings.Contains(styled, "\x1b[1mKönigreich\x1b[0m") {
		t.Errorf("escape sequence broken by wrapping: %q", styled)
	}
}

func TestWrapIndentDisabled(t *testing.T) {
	const s = "a long line that would otherwise be wrapped into several lines"
	// width 0 is how callers turn wrapping off for pipes and files
	if got := WrapIndent(s, "     ", 0); got != s {
		t.Errorf("width 0 must not wrap: %q", got)
	}
	// too narrow to be worth wrapping
	if got := WrapIndent(s, "     ", 20); got != s {
		t.Errorf("narrow width must not wrap: %q", got)
	}
	if got := WrapIndent("", "     ", 80); got != "" {
		t.Errorf("empty input: %q", got)
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

// linkCard is wol's publication link, as it appears on the daily text and
// meetings pages: a cover thumbnail with no alt text, then one <div> per line,
// all inside the anchor.
const linkCard = `
<div class="linkCard">
  <a class="jwac chrome
    cardContainer
    noTooltips lnk" href="/de/wol/d/r10/lp-x/202026245">
    <div class="cardThumbnail ">
      <img class="cardThumbnailImage thumbnail publication" data-pub-symbol="mwb26"
           src="https://wol.jw.org/de/wol/d/r10/lp-x/202026245/thumbnail"/>
    </div>
    <div class="cardTitleBlock">
      <div class="    cardLine1
    ellipsized"><span class="sectionIcon"></span>
        3.-9. August
      </div>
      <div class="    cardLine2
    ellipsized">
        Leben und Dienst: Arbeitsheft (2026) | Juli
      </div>
    </div>
    <div class="cardTitleDetail"></div>
    <div class="cardChevron"><div class="icon"></div></div>
  </a>
</div>`

// TestRenderFlattensLinkCards pins the card down to a single markdown link.
// Converted verbatim it becomes an empty image plus hard line breaks inside the
// link text, which reads as five ragged lines for one link.
func TestRenderFlattensLinkCards(t *testing.T) {
	out, err := Render(linkCard, Markdown, Options{BaseURL: "https://wol.jw.org"})
	if err != nil {
		t.Fatal(err)
	}
	const want = "[3.-9. August — Leben und Dienst: Arbeitsheft (2026) | Juli]" +
		"(https://wol.jw.org/de/wol/d/r10/lp-x/202026245)"
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("card not flattened:\n got %q\nwant %q", got, want)
	}
}

func TestRenderFlattensLinkCardsToText(t *testing.T) {
	out, err := Render(linkCard, Text, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(out), "3.-9. August — Leben und Dienst: Arbeitsheft (2026) | Juli"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRenderKeepsCaptionedImages guards the flattening from over-reaching: a
// normal body image is not a card decoration and has to survive.
func TestRenderKeepsCaptionedImages(t *testing.T) {
	out, err := Render(sample, Markdown, Options{BaseURL: "https://wol.jw.org"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "![A mountain](https://cdn.example/img_xl.jpg)") {
		t.Errorf("body image dropped:\n%s", out)
	}
}

// TestRenderStripsSoftHyphens covers wol's hyphenation hints. A browser hides
// U+00AD unless it breaks the word there; every other renderer shows a gap
// mid-word, which is not in the text at all.
func TestRenderStripsSoftHyphens(t *testing.T) {
	// soft hyphen inside the compound, no-break spaces around a number and a date
	const frag = "<p>während der Tausendjahr\u00adherrschaft Jesu, 144\u00a0000 ab 3.\u00a0August</p>"
	for _, f := range []Format{Markdown, Raw, Text, HTML} {
		out, err := Render(frag, f, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "\u00ad") {
			t.Errorf("%v: soft hyphen survived: %q", f, out)
		}
		if !strings.Contains(out, "Tausendjahrherrschaft") {
			t.Errorf("%v: compound not rejoined: %q", f, out)
		}
		// the no-break spaces are meaningful typography, not artifacts
		if n := strings.Count(out, "\u00a0"); n != 2 {
			t.Errorf("%v: want 2 no-break spaces, got %d: %q", f, n, out)
		}
	}
}

// TestHorizontalRule pins the thematic break to three hyphens in both the
// markdown source and the styled output. The converter's default is "* * *" and
// glamour draws eight hyphens; neither is what a markdown document looks like,
// and the two disagreeing is worse than either.
func TestHorizontalRule(t *testing.T) {
	md, err := Render("<p>a</p><hr/><p>b</p>", Markdown, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "\n---\n") {
		t.Errorf("markdown rule: %q", md)
	}
	for _, unwanted := range []string{"* * *", "--------"} {
		if strings.Contains(md, unwanted) {
			t.Errorf("markdown still contains %q: %q", unwanted, md)
		}
	}
	styled := visibleText(toTerminal(md, styles.DarkStyle, 60))
	if !strings.Contains(styled, "---") || strings.Contains(styled, "--------") {
		t.Errorf("styled rule: %q", styled)
	}
}

// noURLSample carries the three shapes --no-urls has to deal with: a link, an
// image with alt text, and an image without any.
const noURLSample = `<p>Faith moves mountains. See
<a href="/en/wol/bc/r1/lp-e/123/1/0">Matt 17:20</a>.</p>
<figure><img src="/img/a.jpg" alt="A mountain"/><figcaption>A mountain range</figcaption></figure>
<p><a href="/z"><img src="/img/b.jpg"/></a></p>`

func TestRenderNoURLs(t *testing.T) {
	for _, tc := range []struct {
		format Format
		want   []string
	}{
		{Markdown, []string{"Matt 17:20", "image: A mountain", "A mountain range"}},
		{Raw, []string{"Matt 17:20", "image: A mountain", "A mountain range"}},
		{HTML, []string{"Matt 17:20", "[image: A mountain]", "A mountain range"}},
		{Text, []string{"Matt 17:20", "[image: A mountain]", "A mountain range"}},
	} {
		t.Run(tc.format.String(), func(t *testing.T) {
			out, err := Render(noURLSample, tc.format, Options{BaseURL: "https://wol.jw.org", NoURLs: true})
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q:\n%s", want, out)
				}
			}
			// no target survives in any spelling: absolutized, relative, or as
			// the markdown/HTML syntax that would carry one
			for _, unwanted := range []string{"wol.jw.org", "/img/a.jpg", "/img/b.jpg", "href=", "<img", "](", "!["} {
				if strings.Contains(out, unwanted) {
					t.Errorf("still contains %q:\n%s", unwanted, out)
				}
			}
		})
	}
}

// TestRenderNoURLsKeepsURLsOff pins that the flag is opt-in: without it the
// targets are rendered as before.
func TestRenderNoURLsKeepsURLsOff(t *testing.T) {
	out, err := Render(noURLSample, Markdown, Options{BaseURL: "https://wol.jw.org"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[Matt 17:20](https://wol.jw.org/en/wol/bc/r1/lp-e/123/1/0)") {
		t.Errorf("link target dropped without NoURLs:\n%s", out)
	}
}
