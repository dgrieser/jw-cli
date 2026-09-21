package service

import (
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/model"
)

// Real passages, taken from what a citation search for Jeremia 34:7 answers.
const (
	workbookHeading  = `<h2 id="p2"><span class="mk"><a class="b" href="/de/wol/bc/x/0/0"><strong>JEREMIA 34-35</strong></a></span></h2>`
	workbookQuestion = `<p id="p13"><span class="mk"><a class="b" href="/de/wol/bc/x/4/0">Jer 34:7</a></span> – Welche` +
		` archäologischen Funde stützen die Ereignisse, die in diesem Vers beschrieben werden?` +
		` (<a href="/de/wol/pc/x/4/0"><em>it</em> „Archäologie“ Abs. 27-28</a>)</p>`
	readingPlan = `<p id="p47" class="sc"><span class="txtSrcBullet">14 </span>` +
		`<a class="b" href="/de/wol/bc/x/35/0"><span class="mk">Jeremia 34-35</span></a></p>`
	scriptureIndex = `<p id="p1703" class="sk"><span class="mk"><a class="b" href="/de/wol/bc/x/1676/0">34:7</a></span>` +
		` <a href="/de/wol/pc/x/1676/0"><strong>1:</strong>179,</a><a href="/de/wol/pc/x/1676/2"> 214;</a></p>`
	insightProse = `<p class="sb">In der Prophezeiung Hoseas wurde vorausgesagt, dass Gott Feuer in die` +
		` Städte Judas senden werde (<a class="b" href="/de/wol/bc/x/1/0">Hos 8:14</a>). Im 14. Jahr` +
		` der Regierung König Hiskias kam Sanherib gegen all die befestigten Städte von Juda herauf.</p>`
)

func TestTells(t *testing.T) {
	tests := []struct {
		name     string
		fragment string
		want     bool
	}{
		{"a section headed by the reference", workbookHeading, false},
		{"a question about the verse", workbookQuestion, true},
		{"a reading schedule", readingPlan, false},
		{"a scripture index line", scriptureIndex, false},
		{"prose that cites the verse along the way", insightProse, true},
		{"the numbers of a reading checklist", "33 34 35 36", false},
		{"a checkbox line", "/ 34-36 □", false},
		{"nothing at all", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tells(tc.fragment); got != tc.want {
				t.Errorf("tells = %v, want %v", got, tc.want)
			}
		})
	}
}

// One document does both: a workbook heads its section with the reference and
// then asks a question about it. Only the heading goes.
func TestKeepTellingFiltersBlockByBlock(t *testing.T) {
	items := []model.Result{
		{Title: "14.-20. September", Excerpt: workbookHeading + "\n" + workbookQuestion},
		{Title: "Bibelleseplan", Excerpt: readingPlan},
		{Title: "Schriftstellenverzeichnis", Excerpt: scriptureIndex},
		{Title: "Hosea (Buch)", Excerpt: insightProse},
		{Title: "Mein persönliches Bibellesen", Snippet: "33 34 35 36"},
		{Title: "Lassen wir Gott täglich zu uns sprechen?", Snippet: "<p>/ 34-36 □</p>"},
	}
	got := keepTelling(items)
	var titles []string
	for _, item := range got {
		titles = append(titles, item.Title)
	}
	if strings.Join(titles, ", ") != "14.-20. September, Hosea (Buch)" {
		t.Fatalf("kept %v", titles)
	}
	if strings.Contains(got[0].Excerpt, "JEREMIA 34-35") {
		t.Errorf("the heading survived:\n%s", got[0].Excerpt)
	}
	if !strings.Contains(got[0].Excerpt, "archäologischen Funde") {
		t.Errorf("the question did not:\n%s", got[0].Excerpt)
	}
}

// A result whose teaser could not be placed in its document is judged on the
// teaser, and kept when the teaser says something.
func TestKeepTellingJudgesTheTeaserWhenThereIsNoPassage(t *testing.T) {
	items := []model.Result{{Title: "w14", Snippet: insightProse}}
	if got := keepTelling(items); len(got) != 1 {
		t.Errorf("a telling teaser should stand in for a passage: %+v", got)
	}
}
