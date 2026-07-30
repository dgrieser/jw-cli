package render

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/glamour/styles"
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

// TestToTerminalStyles exercises the styled path with a fixed style: glamour's
// auto style degrades to the unstyled layout when stdout is not a terminal, so
// a test process would never see ANSI otherwise.
func TestToTerminalStyles(t *testing.T) {
	out := toTerminal(markdownDoc, styles.DarkStyle, 60)
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("expected ANSI styling:\n%q", out)
	}
	// the link markup is consumed, both text and target survive
	if strings.Contains(out, "](") {
		t.Errorf("link markup not rendered:\n%q", out)
	}
	for _, want := range []string{"Title", "emphasis", "link", "https://example.com/a", "one", "two"} {
		if !strings.Contains(out, want) {
			t.Errorf("styled output missing %q:\n%s", want, out)
		}
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
