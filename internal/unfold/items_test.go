package unfold

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/model"
)

// marginalResolver is a verse whose marginal reference wol answers with three
// verses, the way it answers the "+" after "only-begotten Son" in John 3:16
// with seven: each of the three has study notes and a reference of its own.
func marginalResolver() *fakeStudyResolver {
	return &fakeStudyResolver{
		fakeResolver: &fakeResolver{content: map[string]model.Tooltip{
			"/wol/bc/root": {Title: "John 3:16", ContentHTML: verseHTML(43, 3, 16, "his only-begotten Son"+link("/wol/bc/m", "+"))},
			"/wol/bc/m": {
				Title: "Genesis 22:2", ContentHTML: verseHTML(1, 22, 2, "your only son"),
				More: []model.Tooltip{
					{Title: "John 1:14", ContentHTML: verseHTML(43, 1, 14, "the Word became flesh"+link("/wol/bc/x", "+"))},
					{Title: "1 John 4:9", ContentHTML: verseHTML(62, 4, 9, "his only-begotten Son")},
				},
			},
			"/wol/bc/x": {Title: "Isaiah 9:6", ContentHTML: verseHTML(23, 9, 6, "a child has been born")},
		}},
		study: map[string]Study{
			"Genesis 22:2": {Notes: []model.StudyNote{{HTML: "the son of the promise"}}, Requests: 1},
			"John 1:14":    {Notes: []model.StudyNote{{HTML: "only-begotten"}}, Requests: 1},
			"1 John 4:9":   {Notes: []model.StudyNote{{HTML: "the love of God"}}, Requests: 1},
		},
	}
}

func titles(nodes []Node) []string {
	var out []string
	for _, n := range nodes {
		out = append(out, n.Title)
	}
	return out
}

// Every verse a marginal reference names is a reference of its own: its text,
// its study material, and at the next level what it cites in turn.
func TestRunKeepsEveryPassageOfACitation(t *testing.T) {
	r := marginalResolver()
	res, err := Run(context.Background(), r, link("/wol/bc/root", "Joh 3:16"), Options{Depth: 3})
	if err != nil {
		t.Fatal(err)
	}
	marginal := res.Nodes[0].Children
	if got := strings.Join(titles(marginal), "|"); got != "Genesis 22:2|John 1:14|1 John 4:9" {
		t.Fatalf("marginal references = %s", got)
	}
	for i, n := range marginal {
		if n.Item != i+1 || len(n.Notes) != 1 || n.Ref.Text != "+" {
			t.Errorf("item %d: Item=%d notes=%v text=%q", i, n.Item, n.Notes, n.Ref.Text)
		}
	}
	if base, item := SplitItemPath(marginal[1].Ref.Path); base != "/wol/bc/m" || item != 2 || marginal[0].Ref.Path != "/wol/bc/m" {
		t.Errorf("paths = %s, %s", marginal[0].Ref.Path, marginal[1].Ref.Path)
	}
	// what the second passage cites is followed one level further down
	if c := marginal[1].Children; len(c) != 1 || c[0].Title != "Isaiah 9:6" {
		t.Errorf("the second passage's own reference: %+v", c)
	}
	// the citation is asked for once; each verse's study pane is read
	if n := slices.Index(r.asked, "/wol/bc/m"); n < 0 || slices.Index(r.asked[n+1:], "/wol/bc/m") >= 0 {
		t.Errorf("asked %v", r.asked)
	}
	for _, want := range []string{"Genesis 22:2", "John 1:14", "1 John 4:9"} {
		if !slices.Contains(r.askedFor, want) {
			t.Errorf("no study read for %s: %v", want, r.askedFor)
		}
	}
	// the root, the marginal reference, the panes of its three verses, and
	// Isaiah; John 3:16 and Isaiah have no pane in this library
	if res.Requests != 6 {
		t.Errorf("Requests = %d, want 6", res.Requests)
	}
}

// The verses past the first are priced once they are known, before their
// material is read, and progress counts them on after the citations.
func TestRunAsksAboutThePassagesOfACitation(t *testing.T) {
	r := marginalResolver()
	var asked, progress []string
	_, err := Run(context.Background(), r, link("/wol/bc/m", "+"), Options{
		Depth: 1, Threshold: 1,
		Confirm: func(level, requests int) (bool, error) {
			asked = append(asked, fmt.Sprintf("%d:%d", level, requests))
			return true, nil
		},
		Progress: func(level, done, total int) { progress = append(progress, fmt.Sprintf("%d:%d/%d", level, done, total)) },
	})
	if err != nil {
		t.Fatal(err)
	}
	// the citation and its pane, then the panes of the two further verses
	if !slices.Equal(asked, []string{"1:2", "1:2"}) {
		t.Errorf("asked %v", asked)
	}
	if !slices.Equal(progress, []string{"1:1/1", "1:2/3", "1:3/3"}) {
		t.Errorf("progress %v", progress)
	}

	// declined, the further verses stay their text and the run says so
	r = marginalResolver()
	calls := 0
	res, err := Run(context.Background(), r, link("/wol/bc/m", "+"), Options{
		Depth: 1, Threshold: 1,
		Confirm: func(int, int) (bool, error) { calls++; return calls == 1, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Stopped || res.Pending != 2 || slices.Contains(r.askedFor, "John 1:14") {
		t.Errorf("stopped=%v pending=%d study=%v", res.Stopped, res.Pending, r.askedFor)
	}
}

// Counted in bible references, each verse of a citation is shown once like any
// other: one shown already is left out before its study pane is read.
func TestHopsKeepEveryPassageOfACitation(t *testing.T) {
	r := marginalResolver()
	verses := NewVerses()
	verses.Add(62_004_009) // 1 John 4:9 is on the page already
	_, res := runHops(t, r, 2, Options{Verses: verses}, Ref{Text: "Joh 3:16", Path: "/wol/bc/root"})
	marginal := res.Nodes[0][0].Children
	if got := strings.Join(titles(marginal), "|"); got != "Genesis 22:2|John 1:14" {
		t.Fatalf("marginal references = %s", got)
	}
	for _, n := range marginal {
		if n.Parts == nil || n.Parts.Lazy || len(n.Notes) != 1 {
			t.Errorf("%s should unfold with its notes: %+v", n.Title, n)
		}
	}
	if slices.Contains(r.askedFor, "1 John 4:9") {
		t.Errorf("the study pane of a verse shown already was read: %v", r.askedFor)
	}
	// John 1:14's own reference comes at the next step
	if c := marginal[1].Children; len(c) != 1 || c[0].Title != "Isaiah 9:6" || c[0].Parts == nil || !c[0].Parts.Lazy {
		t.Errorf("John 1:14's reference: %+v", c)
	}
}

// A passage another index lists is shown as said already, whichever of the
// passages of its citation it is.
func TestHopsShowCountsEveryPassage(t *testing.T) {
	r := &fakeResolver{content: map[string]model.Tooltip{
		"/wol/pc/guide": {Title: "A", ContentHTML: "<p>first</p>", URL: "/d/1#h=1",
			More: []model.Tooltip{{Title: "B", ContentHTML: "<p>second</p>", URL: "/d/2#h=1"}}},
		"/wol/pc/index": {Title: "B", ContentHTML: "<p>second</p>", URL: "/d/2#h=3"},
	}}
	s := NewSession(r, Options{Hops: true})
	if err := s.Show(context.Background(), []Ref{{Path: "/wol/pc/guide"}}); err != nil {
		t.Fatal(err)
	}
	res, err := s.Run(context.Background(), []Group{{RootRefs: []Ref{{Path: "/wol/pc/index", Rank: 2}}, Hops: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Nodes[0]) != 0 {
		t.Errorf("a passage shown already was shown again: %+v", res.Nodes[0])
	}
}

func TestItemPath(t *testing.T) {
	for _, c := range []struct {
		path string
		item int
	}{{"/de/wol/bc/r10/lp-x/1001070147/96", 1}, {"/de/wol/bc/r10/lp-x/1001070147/96", 7}, {"/wol/pc/1/2", 2}} {
		p := ItemPath(c.path, c.item)
		if base, item := SplitItemPath(p); base != c.path || item != c.item {
			t.Errorf("SplitItemPath(%q) = %q, %d; want %q, %d", p, base, item, c.path, c.item)
		}
		if CitationPath(p) != c.path {
			t.Errorf("CitationPath(%q) = %q", p, CitationPath(p))
		}
	}
	if ItemPath("/wol/bc/1", 1) != "/wol/bc/1" {
		t.Error("the first passage keeps the citation's path")
	}
}
