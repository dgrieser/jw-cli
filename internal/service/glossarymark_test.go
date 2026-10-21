package service

import (
	"strings"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
)

// markService is a service whose cache holds a German index: Jehova and
// Immer (a priest) in Insight, Wunder in the glossary and in Insight.
func markService(t *testing.T) (*Service, model.Language) {
	t.Helper()
	s := New(httpx.New(), httpx.OpenCacheAt(t.TempDir()))
	lng := model.Language{Symbol: "X", Locale: "de"}
	g := Glossary{
		Lang:          "X",
		Books:         []GlossaryBook{{ID: "nwtstg", Title: "Worterklärungen"}, {ID: "it", Title: "Einsichten"}},
		DocURL:        "https://wol.jw.org/de/wol/d/r10/lp-x/",
		CaseSensitive: true,
		Suffixes:      []string{"s", "es", "n", "en", "e", "er", "ern"},
		Terms: []GlossaryTerm{
			{Term: "Immer", Book: 1, DocID: 1200002100},
			{Term: "Jehova", Book: 1, DocID: 1200002300},
			{Term: "Gottes Königreich", Book: 0, DocID: 1001077100},
			{Term: "Wunder", Book: 0, DocID: 1001077200},
			{Term: "Wunder", Book: 1, DocID: 1200004900},
		},
	}
	s.Cache.Put(glossaryKey(lng), keptGlossary{At: time.Now().Unix(), Glossary: g})
	return s, lng
}

func marked(out string) []string {
	var words []string
	for _, part := range strings.Split(out, `class="gloss"`)[1:] {
		_, rest, _ := strings.Cut(part, ">")
		w, _, _ := strings.Cut(rest, "</a>")
		words = append(words, w)
	}
	return words
}

func TestMarkGlossary(t *testing.T) {
	s, lng := markService(t)
	for _, tc := range []struct {
		name, in string
		want     []string
	}{
		{"first occurrence only", `<p>Jehova sieht. Und Jehova hört.</p>`, []string{"Jehova"}},
		{"a phrase, the longest first", `<p>Sie predigen Gottes Königreich.</p>`, []string{"Gottes Königreich"}},
		{"an ending mid-sentence", `<p>Sie sahen viele Wundern zu.</p>`, []string{"Wundern"}},
		{"a guessed ending at the start of a sentence", `<p>Ja. Wundere dich nicht.</p>`, nil},
		{"after a verse's number and a marginal mark", `<p>geboren werden.“<a href="/x">+</a> <a class="vl" href="/v">7 </a>Wundere dich nicht.</p>`, nil},
		{"a word the page writes in small letters", `<p>Immer wieder. Er tut es immer.</p>`, nil},
		{"a name mid-sentence", `<p>Der Priester Immer kam.</p>`, []string{"Immer"}},
		{"a name at the start, no small letters", `<p>„Immer kam.“</p>`, []string{"Immer"}},
		{"not in links and headings", `<h2>Jehova</h2><p><a href="/x">Jehova</a></p>`, nil},
		{"each section on its own", `<p>Jehova.</p><div class="section-body"><p>Jehova.</p></div>`, []string{"Jehova", "Jehova"}},
		{"not in its own entry", `<div class="docId-1200002300"><p>Jehova ist Gott.</p></div>`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, lng := markService(t)
			got := marked(s.MarkGlossary(lng, tc.in))
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("marked %q in %s, want %q", got, tc.in, tc.want)
			}
		})
	}

	// the other glossary's entry, and both named on hover
	out := s.MarkGlossary(lng, `<p>Sie sahen Wunder.</p>`)
	for _, want := range []string{`href="https://wol.jw.org/de/wol/d/r10/lp-x/1001077200"`, `data-gloss-book="Worterklärungen"`,
		`title="Worterklärungen · Einsichten"`, `data-gloss-alt="[{&#34;d&#34;:1200004900,&#34;b&#34;:&#34;Einsichten&#34;}]"`} {
		if !strings.Contains(out, want) {
			t.Errorf("no %s in %s", want, out)
		}
	}
}

// What a page wrote in small letters counts on the next pages too, and
// after a restart.
func TestMarkGlossaryLearnsSmallLetters(t *testing.T) {
	s, lng := markService(t)
	if got := marked(s.MarkGlossary(lng, `<p>Immer kam.</p>`)); len(got) != 1 {
		t.Fatalf("a name at the start, nothing known: %q", got)
	}
	s.MarkGlossary(lng, `<p>Er betet immer.</p>`)
	if got := marked(s.MarkGlossary(lng, `<p>Immer kam.</p>`)); len(got) != 0 {
		t.Errorf("at the start, after immer was read: %q", got)
	}
	again := New(httpx.New(), s.Cache)
	if got := marked(again.MarkGlossary(lng, `<p>Immer kam.</p>`)); len(got) != 0 {
		t.Errorf("after a restart: %q", got)
	}
}

// Without an index, a fragment comes back as it was.
func TestMarkGlossaryWithoutIndex(t *testing.T) {
	s := New(httpx.New(), httpx.OpenCacheAt(t.TempDir()))
	in := `<p>Jehova sieht.</p>`
	if out := s.MarkGlossary(model.Language{Symbol: "X", Locale: "de"}, in); out != in {
		t.Errorf("marked without an index: %s", out)
	}
}
