package render

import (
	"strings"
	"testing"
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
		"": Markdown, "md": Markdown, "markdown": Markdown,
		"html": HTML, "text": Text, "txt": Text, "json": JSON,
	} {
		got, err := ParseFormat(in)
		if err != nil || got != want {
			t.Errorf("ParseFormat(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := ParseFormat("yaml"); err == nil {
		t.Error("want error for invalid format")
	}
}
