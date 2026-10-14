package service

import (
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/bibleref"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
)

// Proverbs 8:8, 9 as the chapter page gives them: two lines a verse, joined
// with a break, and each verse starting a line of its own.
var poetryPassage = ReadResult{
	Table: bibleref.English(),
	Passages: []Passage{{Ref: "Proverbs 8:8-9", Verses: []Verse{
		{Verse: model.Verse{ID: 20008008, HTML: ` <a href="/en/wol/dx/r1/lp-e/1001070124/16726" class="vl vp">8 </a>` +
			`All the sayings of my mouth are righteous.<br/>None of them are twisted or crooked.`}, StartsLine: true},
		{Verse: model.Verse{ID: 20008009, HTML: ` <a href="/en/wol/dx/r1/lp-e/1001070124/16727" class="vl vp">9 </a>` +
			`They are all straightforward to the discerning<br/>And right to those who have found knowledge.`}, StartsLine: true},
	}}},
}

// Every line of poetry stays a line of its own, in every format, both within
// a verse and from one verse to the next.
func TestFormatPassagesPoetry(t *testing.T) {
	for _, c := range []struct {
		format render.Format
		want   []string
	}{
		{render.Text, []string{
			"are righteous.\nNone of them",
			"or crooked.\n9 They are all",
			"to the discerning\nAnd right",
		}},
		{render.Markdown, []string{
			"are righteous.  \nNone of them",
			"or crooked.  \n9 They are all",
			"to the discerning  \nAnd right",
		}},
		{render.HTML, []string{
			"are righteous.<br/>None of them",
			"or crooked. <br/>",
			"to the discerning<br/>And right",
		}},
	} {
		got, err := FormatPassages(poetryPassage, c.format, render.Options{NoURLs: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range c.want {
			if !strings.Contains(got, want) {
				t.Errorf("%v misses %q:\n%s", c.format, want, got)
			}
		}
	}
}

// Prose runs on as it did: verses that do not start a line are joined with a
// space, never a break.
func TestFormatPassagesProse(t *testing.T) {
	res := ReadResult{
		Table: bibleref.English(),
		Passages: []Passage{{Ref: "John 3:16-17", Verses: []Verse{
			{Verse: model.Verse{ID: 43003016, HTML: `16 For God loved the world so much.`}},
			{Verse: model.Verse{ID: 43003017, HTML: `17 For God did not send his Son.`}},
		}}},
	}
	got, err := FormatPassages(res, render.Text, render.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "so much. 17 For God") {
		t.Errorf("prose broken apart:\n%s", got)
	}
}
