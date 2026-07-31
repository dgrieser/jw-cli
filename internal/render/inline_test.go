package render

import (
	"strings"
	"testing"
)

func TestInline(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"strips highlight tags", "vom <strong>Königreich</strong> bekannt machen?",
			"vom Königreich bekannt machen?"},
		{"decodes entities", "Daniel&nbsp;7:27", "Daniel 7:27"},
		{"collapses newlines", "durch das Königreich\nsogar den Tod", "durch das Königreich sogar den Tod"},
		{"collapses runs of space", "a  \t b", "a b"},
		{"br becomes a space", "one<br/>two", "one two"},
		{"emphasis tags without the option", "an <em>emphasised</em> word", "an emphasised word"},
		{"image alt", `see <img src="x.jpg" alt="a mountain"/>`, "see [image: a mountain]"},
		{"script dropped", "text<script>alert(1)</script>", "text"},
		{"plain text untouched", "just words", "just words"},
		{"empty", "", ""},
	} {
		if got := Inline(tc.in, InlineOptions{}); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
}

func TestInlineEmphasis(t *testing.T) {
	got := Inline("vom <strong>Königreich</strong> und <em>mehr</em>", InlineOptions{Emphasis: true})
	want := "vom " + ansiBold + "Königreich" + ansiReset + " und " + ansiItalic + "mehr" + ansiReset
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// the visible text must be identical either way
	if plain := Inline("vom <strong>Königreich</strong> und <em>mehr</em>", InlineOptions{}); plain != "vom Königreich und mehr" {
		t.Errorf("plain form: %q", plain)
	}
}

func TestInlineEmphasisHonorsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	got := Inline("vom <strong>Königreich</strong>", InlineOptions{Emphasis: true})
	if strings.Contains(got, "\x1b[") {
		t.Errorf("NO_COLOR must suppress emphasis: %q", got)
	}
}
