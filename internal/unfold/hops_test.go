package unfold

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/dgrieser/jw-cli/internal/model"
)

// verseHTML is a verse as wol's citation endpoint answers it: bible text in a
// span naming the verse, with whatever links it carries.
func verseHTML(book, chapter, verse int, text string) string {
	return fmt.Sprintf(`<p><span id="v%d-%d-%d-1" class="v">%s</span></p>`, book, chapter, verse, text)
}

// hopResolver is a verse with every part an expansion counted in bible
// references reads: study notes, a footnote, an index passage and a marginal
// reference, each citing a verse of its own.
func hopResolver() *fakeStudyResolver {
	return &fakeStudyResolver{
		fakeResolver: &fakeResolver{content: map[string]model.Tooltip{
			"/wol/bc/root": {Title: "John 3:16", ContentHTML: verseHTML(43, 3, 16,
				`God loved the world`+link("/wol/fn/f", "*")+link("/wol/bc/m", "+"))},
			"/wol/fn/f":   {ContentHTML: `<p>Or “gave” (` + link("/wol/bc/q", "Ro 8:32") + `)</p>`},
			"/wol/bc/m":   {Title: "1 John 4:9", ContentHTML: verseHTML(62, 4, 9, "love was revealed"+link("/wol/bc/x", "+"))},
			"/wol/bc/n":   {Title: "Romans 5:8", ContentHTML: verseHTML(45, 5, 8, "God recommends his own love")},
			"/wol/bc/q":   {Title: "Romans 8:32", ContentHTML: verseHTML(45, 8, 32, "He did not spare his own Son")},
			"/wol/bc/x":   {Title: "Genesis 1:1", ContentHTML: verseHTML(1, 1, 1, "In the beginning")},
			"/wol/bc/p":   {Title: "Matthew 5:44", ContentHTML: verseHTML(40, 5, 44, "Continue to love your enemies")},
			"/wol/pc/r":   {Title: "Love", ContentHTML: `<p>The greatest (` + link("/wol/bc/p", "Mt 5:44") + `; ` + link("/wol/pc/other", "w14 1") + `)</p>`},
			"/wol/pc/oth": {Title: "Other", ContentHTML: "<p>other</p>"},
		}},
		study: map[string]Study{
			"John 3:16": {
				Notes:    []model.StudyNote{{HTML: `<strong>loved:</strong> See ` + link("/wol/bc/n", "Ro 5:8") + ` and ` + link("/wol/pc/note", "note")}},
				Research: []Ref{{Text: "it-2 274", Path: "/wol/pc/r"}},
				Requests: 1,
			},
			"Romans 5:8": {
				Notes:    []model.StudyNote{{HTML: `See ` + link("/wol/bc/x", "Ge 1:1")}},
				Requests: 1,
			},
		},
	}
}

func runHops(t *testing.T, r Resolver, hops int, o Options, refs ...Ref) (*Session, Grouped) {
	t.Helper()
	o.Hops = true
	sess := NewSession(r, o)
	res, err := sess.Run(context.Background(), []Group{{RootRefs: refs, Hops: hops}})
	if err != nil {
		t.Fatal(err)
	}
	return sess, res
}

func paths(nodes []Node) []string {
	var out []string
	for _, n := range nodes {
		out = append(out, n.Ref.Path)
	}
	return out
}

// At one reference deep a verse brings everything it has at once, and the
// verses those parts cite come as their text, their own parts left to be
// opened: no study pane is read for them, and nothing they cite is followed.
func TestHopsOneDeep(t *testing.T) {
	r := hopResolver()
	_, res := runHops(t, r, 1, Options{}, Ref{Text: "Joh 3:16", Path: "/wol/bc/root"})
	if len(res.Nodes[0]) != 1 {
		t.Fatalf("roots: %+v", res.Nodes[0])
	}
	v := res.Nodes[0][0]
	if v.Parts == nil || v.Parts.Lazy || v.Hops != 1 {
		t.Fatalf("the verse asked for should unfold: %+v", v)
	}
	if len(v.Notes) != 1 {
		t.Errorf("notes: %+v", v.Notes)
	}
	if got := paths(v.Parts.NoteRefs); !slices.Equal(got, []string{"/wol/bc/n"}) {
		t.Errorf("the notes bring the verses they cite, and nothing else: %v", got)
	}
	if len(v.Parts.Footnotes) != 1 || !slices.Equal(paths(v.Parts.Footnotes[0].Children), []string{"/wol/bc/q"}) {
		t.Errorf("footnotes: %+v", v.Parts.Footnotes)
	}
	if !slices.Equal(paths(v.Children), []string{"/wol/bc/m"}) {
		t.Errorf("marginal references: %v", paths(v.Children))
	}
	if len(v.Parts.Research) != 1 || v.Parts.Research[0].HTML == "" {
		t.Fatalf("the index passage should be read: %+v", v.Parts.Research)
	}
	if got := paths(v.Parts.Research[0].Children); !slices.Equal(got, []string{"/wol/bc/p"}) {
		t.Errorf("an index passage brings the verses it cites, and nothing else: %v", got)
	}
	for _, n := range [][]Node{v.Parts.NoteRefs, v.Parts.Footnotes[0].Children, v.Children, v.Parts.Research[0].Children} {
		if n[0].Parts == nil || !n[0].Parts.Lazy || n[0].HTML == "" {
			t.Errorf("a verse one reference down is its text, its parts left to be opened: %+v", n[0])
		}
		if len(n[0].Children) > 0 || len(n[0].Notes) > 0 {
			t.Errorf("nothing is read for it: %+v", n[0])
		}
	}
	if !slices.Equal(r.askedFor, []string{"John 3:16"}) {
		t.Errorf("only the verse asked for has its study pane read: %v", r.askedFor)
	}
	for _, p := range []string{"/wol/bc/x", "/wol/pc/note", "/wol/pc/other"} {
		if slices.Contains(r.asked, p) {
			t.Errorf("%s is not followed at one reference deep", p)
		}
	}
	if res.Stopped || res.Pending != 0 {
		t.Errorf("nothing left out: %+v", res)
	}
}

// Two deep, the verses one reference down unfold in turn, and theirs are left
// to be opened.
func TestHopsTwoDeep(t *testing.T) {
	r := hopResolver()
	_, res := runHops(t, r, 2, Options{}, Ref{Text: "Joh 3:16", Path: "/wol/bc/root"})
	v := res.Nodes[0][0]
	n := v.Parts.NoteRefs[0]
	if n.Parts == nil || n.Parts.Lazy || n.Hops != 1 || len(n.Notes) != 1 {
		t.Fatalf("a verse one reference down unfolds at two deep: %+v", n)
	}
	x := n.Parts.NoteRefs
	if len(x) != 1 || x[0].Ref.Path != "/wol/bc/x" || !x[0].Parts.Lazy {
		t.Errorf("the verses its notes cite are left to be opened: %+v", x)
	}
	// the marginal reference of 1 John 4:9 is that same verse: shown once
	m := v.Children[0]
	if len(m.Children) != 0 {
		t.Errorf("a verse already shown is not shown again: %+v", m.Children)
	}
}

// A verse is shown once, where it is first reached; a verse shown before the
// session — the one being unfolded, or what an earlier piece of a page showed —
// is not shown at all.
func TestHopsShowsAVerseOnce(t *testing.T) {
	r := &fakeResolver{content: map[string]model.Tooltip{
		"/wol/bc/1": {Title: "Romans 5:8", ContentHTML: verseHTML(45, 5, 8, "love")},
		"/wol/bc/2": {Title: "Romans 5:8", ContentHTML: verseHTML(45, 5, 8, "love")},
		"/wol/bc/3": {Title: "Romans 5:8, 9", ContentHTML: verseHTML(45, 5, 8, "love") + verseHTML(45, 5, 9, "more")},
		"/wol/bc/4": {Title: "John 3:16", ContentHTML: verseHTML(43, 3, 16, "world")},
	}}
	shown := NewVerses()
	shown.Add(43003016)
	_, res := runHops(t, r, 0, Options{Verses: shown},
		Ref{Path: "/wol/bc/1"}, Ref{Path: "/wol/bc/2"}, Ref{Path: "/wol/bc/3"}, Ref{Path: "/wol/bc/4"})
	if got := paths(res.Nodes[0]); !slices.Equal(got, []string{"/wol/bc/1", "/wol/bc/3"}) {
		t.Errorf("kept %v; want the first Romans 5:8 and the passage saying more", got)
	}
	if !shown.Shown([]int{45005008, 45005009}) || shown.Len() != 3 {
		t.Errorf("the verses shown are added to the set shared: %d", shown.Len())
	}
}

// A passage of another publication is no step of its own: cited at one deep,
// the verses it quotes come left to be opened.
func TestHopsPassageBringsItsVerses(t *testing.T) {
	r := hopResolver()
	_, res := runHops(t, r, 1, Options{}, Ref{Text: "it-2 274", Path: "/wol/pc/r"})
	p := res.Nodes[0][0]
	if p.Parts != nil || p.HTML == "" {
		t.Fatalf("passage: %+v", p)
	}
	if len(p.Children) != 1 || p.Children[0].Ref.Path != "/wol/bc/p" || !p.Children[0].Parts.Lazy {
		t.Errorf("the passage brings its verses, left to be opened: %+v", p.Children)
	}
	if slices.Contains(r.asked, "/wol/pc/other") {
		t.Error("nothing else a passage cites is followed")
	}
}

// What a step costs is asked about before it is spent, and declining it keeps
// what came before.
func TestHopsConfirm(t *testing.T) {
	r := hopResolver()
	var asked []int
	_, res := runHops(t, r, 1, Options{Threshold: -1, Confirm: func(level, requests int) (bool, error) {
		asked = append(asked, requests)
		return level == 1 && len(asked) == 1, nil
	}}, Ref{Path: "/wol/bc/root"})
	// the verse and its chapter page, then its footnote and index passage
	if len(asked) != 2 || asked[0] != 2 || asked[1] != 2 {
		t.Errorf("asked about %v", asked)
	}
	v := res.Nodes[0][0]
	if !res.Stopped || len(v.Parts.Research) != 0 || len(v.Parts.Footnotes) != 0 {
		t.Errorf("declined: %+v", res)
	}
}

// Show counts a passage as shown without showing it: an index loaded on its
// own leaves out what the other one lists.
func TestHopsShow(t *testing.T) {
	r := &fakeResolver{content: map[string]model.Tooltip{
		"/wol/pc/guide": {Title: "Love", URL: "/wol/d/1#h=1", ContentHTML: "<p>the same words</p>"},
		"/wol/pc/index": {Title: "Love", URL: "/wol/d/1#h=2", ContentHTML: "<p>the same words</p>"},
		"/wol/pc/own":   {Title: "Other", URL: "/wol/d/2", ContentHTML: "<p>other words</p>"},
	}}
	sess := NewSession(r, Options{Hops: true})
	if err := sess.Show(context.Background(), []Ref{{Path: "/wol/pc/guide"}}); err != nil {
		t.Fatal(err)
	}
	res, err := sess.Run(context.Background(), []Group{{RootRefs: []Ref{
		{Path: "/wol/pc/index", Rank: 2}, {Path: "/wol/pc/own", Rank: 2},
	}, Hops: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(res.Nodes[0]); !slices.Equal(got, []string{"/wol/pc/own"}) {
		t.Errorf("kept %v", got)
	}
	if sess.Requests() != 3 {
		t.Errorf("requests %d", sess.Requests())
	}
}

func TestVerseIDs(t *testing.T) {
	got := VerseIDs(verseHTML(43, 3, 16, "a") + `<span id="v43-3-16-2"></span>` + verseHTML(43, 3, 17, "b"))
	if !slices.Equal(got, []int{43003016, 43003017}) {
		t.Errorf("got %v", got)
	}
	if VerseIDs("<p>no verse</p>") != nil {
		t.Error("not bible text")
	}
}

// A tracked view adds to the set it tracks at once, and gives back what it
// added when the piece it served failed.
func TestVersesTrackRelease(t *testing.T) {
	run := NewVerses()
	run.Add(1)
	a, b := run.Track(), run.Track()
	if a.Claim([]int{2, 3}) || !b.Shown([]int{2, 3}) || !b.Claim([]int{1, 2}) {
		t.Fatal("views share the set they track")
	}
	a.Release()
	if run.Shown([]int{2}) || !run.Shown([]int{1}) || run.Len() != 1 {
		t.Errorf("release takes back what the view added, and only that: %d", run.Len())
	}
}
