package cli

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
	"github.com/dgrieser/jw-cli/internal/unfold"
)

func TestAffirmative(t *testing.T) {
	for _, yes := range []string{"y", "Y", "yes", "YES", "j", "ja", " j \n"} {
		if !affirmative(yes) {
			t.Errorf("%q should mean yes", yes)
		}
	}
	for _, no := range []string{"", "\n", "n", "no", "nein", "maybe", "yy"} {
		if affirmative(no) {
			t.Errorf("%q should not mean yes", no)
		}
	}
}

// TestStatusLineErasesItself covers the progress of an expansion: every count
// overwrites the one before it and the level's last one is taken back, so the
// rendered document is not printed under a leftover status line.
func TestStatusLineErasesItself(t *testing.T) {
	var b strings.Builder
	txt := i18n.EN.Text()
	progress := statusLine(&b, txt)
	for done := 1; done <= 12; done++ {
		progress(1, done, 12)
	}
	out := b.String()
	if strings.Contains(out, "\n") {
		t.Errorf("the status line must stay on its own line: %q", out)
	}
	if !strings.HasSuffix(out, "\r") {
		t.Errorf("the last write must leave the cursor at the start of the line: %q", out)
	}
	// what the terminal is left showing: the last carriage return means only
	// what follows it counts, and nothing does
	if last := out[strings.LastIndex(out[:len(out)-1], "\r")+1:]; strings.TrimSpace(last) != "" {
		t.Errorf("the line was not erased: %q", last)
	}
	// a shorter count must not leave the tail of a longer one behind
	progress(2, 9, 100)
	progress(2, 10, 100)
	wide := render.StringWidth(fmt.Sprintf(txt.UnfoldProgress, 2, 9, 100))
	if got := render.StringWidth(strings.TrimPrefix(b.String()[len(out):], "\r")); got < wide {
		t.Errorf("write is %d columns wide, want at least %d", got, wide)
	}
}

func TestUnfoldHeading(t *testing.T) {
	for _, tc := range []struct {
		name string
		node unfold.Node
		want string
	}{
		// a publication passage: the citation points, the title says what it is
		{"publication citation and title",
			unfold.Node{Ref: unfold.Ref{Text: "6 Abs. 15", Path: "/wol/pc/1/2"}, Title: "Vertraue dem Richter"},
			"6 Abs. 15 → Vertraue dem Richter"},
		// a verse: wol titles it with the same reference spelled out, so the
		// title would only say it twice
		{"verse title dropped",
			unfold.Node{Ref: unfold.Ref{Text: "Apg. 24:15", Path: "/wol/bc/1/2"}, Title: "Apostelgeschichte 24:15"},
			"Apg. 24:15"},
		{"trailing punctuation trimmed",
			unfold.Node{Ref: unfold.Ref{Text: "Joh. 5:29;", Path: "/wol/bc/1/2"}, Title: "Johannes 5:29"},
			"Joh. 5:29"},
		{"no title", unfold.Node{Ref: unfold.Ref{Text: "Joh. 5:29;", Path: "/wol/pc/1/2"}}, "Joh. 5:29"},
		{"title equal to the citation is not repeated",
			unfold.Node{Ref: unfold.Ref{Text: "Matt 24:14", Path: "/wol/pc/1/2"}, Title: "Matt 24:14"}, "Matt 24:14"},
		// a marker inside a verse names nothing, so both ends are named: the
		// passage it sits in and the one it points at
		{"verse marker names both ends",
			unfold.Node{Ref: unfold.Ref{Text: "+", Path: "/wol/bc/9/9"}, Title: "Isaiah 2:2"},
			"Marginal reference Acts 24:15 → Isaiah 2:2"},
		{"asterisk marker", unfold.Node{Ref: unfold.Ref{Text: "*", Path: "/wol/bc/1/2"}, Title: "Daniel 12:13"},
			"Marginal reference Acts 24:15 → Daniel 12:13"},
		// not a verse: no marginal reference to speak of, just the title
		{"publication marker keeps the title alone",
			unfold.Node{Ref: unfold.Ref{Text: "*", Path: "/wol/pc/1/2"}, Title: "Insight, page 390"}, "Insight, page 390"},
		{"empty falls back to title", unfold.Node{Title: "Acts 24:15"}, "Acts 24:15"},
		{"marker with no title stays", unfold.Node{Ref: unfold.Ref{Text: "+"}}, "+"},
	} {
		if got := unfoldHeading(tc.node, "Acts 24:15", i18n.EN.Text()); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestUnfoldHeadingCitationAlreadySaysIt covers a research-guide entry, which
// cites an article by its own headline; wol then answers with that headline
// again, and the heading must not carry it twice.
func TestUnfoldHeadingCitationAlreadySaysIt(t *testing.T) {
	n := unfold.Node{
		Ref:   unfold.Ref{Text: "“God So Loved the World”, The Watchtower, 7/1/2014", Path: "/wol/pc/1/2"},
		Title: "God So Loved the World",
	}
	want := "“God So Loved the World”, The Watchtower, 7/1/2014"
	if got := unfoldHeading(n, "", i18n.EN.Text()); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// a title that says something else still completes the citation
	other := unfold.Node{Ref: unfold.Ref{Text: "it-2 528", Path: "/wol/pc/1/2"}, Title: "Love"}
	if got := unfoldHeading(other, "", i18n.EN.Text()); got != "it-2 528 → Love" {
		t.Errorf("got %q", got)
	}
}

// TestUnfoldHeadingMarkerWithoutSource covers a marker among the document's own
// citations: there is no enclosing passage to name, so the label carries the
// target alone rather than a dangling separator.
func TestUnfoldHeadingMarkerWithoutSource(t *testing.T) {
	n := unfold.Node{Ref: unfold.Ref{Text: "+", Path: "/wol/bc/9/9"}, Title: "Isaiah 2:2"}
	if got, want := unfoldHeading(n, "", i18n.EN.Text()), "Marginal reference Isaiah 2:2"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestUnfoldSource pins how a passage is named when it is cited as the origin of
// a reference found inside it: the resolved title, so both ends of the reference
// are spelled out the same way.
func TestUnfoldSource(t *testing.T) {
	if got := unfoldSource(unfold.Node{Title: "Acts 24:15"}, "Apg. 24:15"); got != "Acts 24:15" {
		t.Errorf("title should win, got %q", got)
	}
	if got := unfoldSource(unfold.Node{}, "Apg. 24:15"); got != "Apg. 24:15" {
		t.Errorf("without a title the label stands in, got %q", got)
	}
}

func TestUnfoldHTML(t *testing.T) {
	txt := i18n.EN.Text()
	res := unfold.Result{Nodes: []unfold.Node{{
		Ref:   unfold.Ref{Text: "Matt 24:14", Path: "/wol/bc/1/1"},
		Title: "Matthew 24:14",
		HTML:  "<p>this good news</p>",
		Children: []unfold.Node{{
			Ref:   unfold.Ref{Text: "+", Path: "/wol/bc/9/9"},
			Title: "Isaiah 2:2",
			HTML:  "<p>the mountain</p>",
		}},
	}}}
	out := unfoldHTML(res, txt)
	// the citation and what it turned out to be share one heading; what that
	// passage cited nests one level deeper
	for _, want := range []string{
		"<h2>References</h2>",
		"<h3>Matt 24:14</h3>",
		"<p>this good news</p>",
		"<h4>Marginal reference Matthew 24:14 → Isaiah 2:2</h4>",
		"<p>the mountain</p>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// no separate subtitle paragraph is left over
	if strings.Contains(out, "<em>Matthew 24:14</em>") {
		t.Errorf("the title belongs in the heading, not a paragraph of its own:\n%s", out)
	}
	if unfoldHTML(unfold.Result{}, txt) != "" {
		t.Error("nothing expanded should append nothing")
	}
}

// TestWriteStudy covers the study material under an unfolded verse: the notes,
// then the research-guide entries that name a whole article and so have no
// passage to unfold.
func TestWriteStudy(t *testing.T) {
	var b strings.Builder
	writeStudy(&b, unfold.Node{
		Notes: []model.StudyNote{{Lemma: "loved", HTML: "<p><strong>loved:</strong> a·ga·pa'o</p>"}},
		Links: []model.ResearchItem{{
			Title:      "Insight, Volume 2, page 274",
			Source:     "Research Guide",
			ArticleURL: "/en/wol/d/r1/lp-e/1102014204",
		}},
	}, 4, i18n.EN.Text())
	out := b.String()
	for _, want := range []string{
		"<h4>Study notes</h4>",
		"<strong>loved:</strong>",
		"<h4>Research guide</h4>",
		`<a href="/en/wol/d/r1/lp-e/1102014204">Insight, Volume 2, page 274</a> (Research Guide)`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// a verse without study material adds nothing
	var empty strings.Builder
	writeStudy(&empty, unfold.Node{Title: "John 3:16"}, 4, i18n.EN.Text())
	if empty.String() != "" {
		t.Errorf("nothing to say should say nothing: %q", empty.String())
	}
}

// TestWriteStudyReportsAnUnreadablePane pins that a study pane that could not be
// read says so instead of leaving the verse looking like it has none.
func TestWriteStudyReportsAnUnreadablePane(t *testing.T) {
	var b strings.Builder
	writeStudy(&b, unfold.Node{StudyErr: errTest}, 4, i18n.EN.Text())
	if !strings.Contains(b.String(), "could not be read") || !strings.Contains(b.String(), "boom") {
		t.Errorf("an unreadable study pane should say so:\n%s", b.String())
	}
}

// studyUnfoldMux serves a document citing a verse, that verse's citation
// endpoint, the chapter page its study pane lives on, and the research-guide
// passage the pane points at — the whole chain an unfolded verse walks.
func studyUnfoldMux(t *testing.T) *http.ServeMux {
	mux := languagesMux(t)
	mux.HandleFunc("/en", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<a href="/en/wol/h/r1/lp-e">home</a>`))
	})
	mux.HandleFunc("/en/wol/d/r1/lp-e/2024360", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><div id="article">
		  <header><h1>God So Loved</h1></header>
		  <div class="bodyTxt"><p>Consider what Jesus said.
		    (<a href="/en/wol/bc/r1/lp-e/2024360/0/0" data-bid="1-1" class="b">Joh 3:16</a>)</p></div>
		</div></body></html>`))
	})
	mux.HandleFunc("/wol/bc/r1/lp-e/2024360/0/0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items": [{"title": "John 3:16",
			"content": "<p>For God loved the world so much</p>", "url": "/en/wol/b/r1/lp-e/nwtsty/43/3"}]}`))
	})
	mux.HandleFunc("/en/wol/b/r1/lp-e/nwtsty/43/3", func(w http.ResponseWriter, r *http.Request) {
		b, err := os.ReadFile(filepath.Join("..", "api", "wol", "testdata", "chapter_john3.html"))
		if err != nil {
			t.Errorf("fixture: %v", err)
			http.Error(w, "missing", 500)
			return
		}
		w.Write(b)
	})
	mux.HandleFunc("/wol/pc/r1/lp-e/1204433/5/0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items": [{"content": "<p>Jehovah loved the world of redeemable mankind.</p>",
			"title": "God So Loved the World", "url": "/en/wol/d/r1/lp-e/2014486"}]}`))
	})
	return mux
}

// TestArticleUnfoldBringsVerseStudyMaterial covers the whole chain: the document
// cites a verse, the verse brings its study notes, and the research-guide
// passage of that verse is unfolded one level deeper.
func TestArticleUnfoldBringsVerseStudyMaterial(t *testing.T) {
	out, err := runCmd(t, studyUnfoldMux(t), "article", "2024360", "-l", "en", "-o", "raw", "--unfold", "2")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## References",
		"Joh 3:16",
		"For God loved the world so much",
		"Study notes",
		"loved:",            // the note's lemma
		"Research guide",    // the entry that points at a whole article
		"Insight, Volume 2", // and its title
		"Jehovah loved the world of redeemable mankind", // the research passage, one level deeper
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestArticleUnfoldOneLevelKeepsResearchPending pins the request curve: the
// notes come with the verse, the passages it references wait for level two.
func TestArticleUnfoldOneLevelKeepsResearchPending(t *testing.T) {
	out, err := runCmd(t, studyUnfoldMux(t), "article", "2024360", "-l", "en", "-o", "raw", "--unfold", "1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Study notes") {
		t.Errorf("the notes come with the verse:\n%s", out)
	}
	if strings.Contains(out, "redeemable mankind") {
		t.Errorf("the research passage belongs to level two:\n%s", out)
	}
}

// TestUnfoldNote separates the two ways an expansion leaves references behind.
// Being cut short did not do what was asked and is worth saying; reaching the
// requested depth did exactly that, and is not worth a word.
func TestUnfoldNote(t *testing.T) {
	txt := i18n.EN.Text()
	node := []unfold.Node{{Ref: unfold.Ref{Text: "Matt 24:14"}, HTML: "<p>x</p>"}}

	cutShort := unfoldHTML(unfold.Result{Nodes: node, Stopped: true, Pending: 137}, txt)
	if !strings.Contains(cutShort, "Stopped here") || !strings.Contains(cutShort, "137") {
		t.Errorf("a declined expansion should say it stopped:\n%s", cutShort)
	}

	// the normal end of --unfold 1: the verses it printed cite 35 more, which
	// is the depth working as asked and needs no remark
	atDepth := unfoldHTML(unfold.Result{Nodes: node, Pending: 35}, txt)
	if strings.Contains(atDepth, "35") || strings.Contains(atDepth, "unfold") {
		t.Errorf("reaching the requested depth should say nothing:\n%s", atDepth)
	}
	if !strings.Contains(atDepth, "<p>x</p>") {
		t.Errorf("the expansion itself is still there:\n%s", atDepth)
	}
}

// TestUnfoldHTMLEscapesHeadings guards against a citation text that carries
// markup breaking out of its heading.
func TestUnfoldHTMLEscapesHeadings(t *testing.T) {
	res := unfold.Result{Nodes: []unfold.Node{{Ref: unfold.Ref{Text: `a<script>x</script>b`}}}}
	out := unfoldHTML(res, i18n.EN.Text())
	if strings.Contains(out, "<script>") {
		t.Errorf("heading not escaped:\n%s", out)
	}
}

func TestUnfoldHTMLReportsAFailedReference(t *testing.T) {
	res := unfold.Result{Nodes: []unfold.Node{{
		Ref: unfold.Ref{Text: "Matt 24:14"},
		Err: errTest,
	}}}
	out := unfoldHTML(res, i18n.EN.Text())
	if !strings.Contains(out, "could not be unfolded") || !strings.Contains(out, "boom") {
		t.Errorf("a dead reference should say so:\n%s", out)
	}
}

var errTest = errStr("boom")

type errStr string

func (e errStr) Error() string { return string(e) }

// TestDemoteHeadings covers a passage that brings its source article's own
// headings along: a sidebar <h2> inside a reference rendered at <h3> would
// otherwise outrank it and take over the outline from there down.
func TestDemoteHeadings(t *testing.T) {
	const frag = `<p>a</p><h2>Was meinte Judas?</h2><p>b</p><h3>deeper</h3>`
	got := demoteHeadings(frag, 3)
	for _, want := range []string{"<h5>Was meinte Judas?</h5>", "<h6>deeper</h6>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	// relative order survives: the h2 stays above the h3
	if i, j := strings.Index(got, "<h5>"), strings.Index(got, "<h6>"); i < 0 || i > j {
		t.Errorf("relative order lost: %q", got)
	}
	// everything else is untouched
	for _, want := range []string{"<p>a</p>", "<p>b</p>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

// TestHeadingHTML pins the fallback past h6: clamping every deeper level onto
// h6 would make separate nesting levels look identical.
func TestHeadingHTML(t *testing.T) {
	for _, tc := range []struct {
		level int
		want  string
	}{
		{2, "<h2>x</h2>"},
		{6, "<h6>x</h6>"},
		{7, "<p><strong>x</strong></p>"},
		{9, "<p><strong>x</strong></p>"},
	} {
		if got := headingHTML(tc.level, "x"); got != tc.want {
			t.Errorf("headingHTML(%d) = %q, want %q", tc.level, got, tc.want)
		}
	}
}

func TestDemoteHeadingsBoldPastSix(t *testing.T) {
	// h4 inside a reference at level 3 lands at 7, past the last heading
	got := demoteHeadings(`<h4>x</h4>`, 3)
	if !strings.Contains(got, "<strong>x</strong>") || strings.Contains(got, "<h") {
		t.Errorf("should render as bold: %q", got)
	}
}

func TestDemoteHeadingsLeavesHeadinglessContent(t *testing.T) {
	// a verse is the common case: no headings, and a <hr> must not be mistaken
	// for one
	for _, frag := range []string{"", "<p>15 Ich setze meine Hoffnung</p>", "<p>a</p><hr/><p>b</p>"} {
		if got := demoteHeadings(frag, 3); got != frag {
			t.Errorf("demoteHeadings(%q) = %q, want it unchanged", frag, got)
		}
	}
}

// TestUnfoldHTMLDeepNestingGoesBold walks references deep enough to run out of
// heading levels: the fifth level sits at h7, which markdown does not have.
func TestUnfoldHTMLDeepNestingGoesBold(t *testing.T) {
	// build a chain five references deep
	node := unfold.Node{Ref: unfold.Ref{Text: "level5"}, HTML: "<p>deepest</p>"}
	for _, text := range []string{"level4", "level3", "level2", "level1"} {
		node = unfold.Node{Ref: unfold.Ref{Text: text}, Children: []unfold.Node{node}}
	}
	out := unfoldHTML(unfold.Result{Nodes: []unfold.Node{node}}, i18n.EN.Text())
	for _, want := range []string{"<h3>level1</h3>", "<h6>level4</h6>", "<p><strong>level5</strong></p>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "<h7>") {
		t.Errorf("h7 does not exist:\n%s", out)
	}
}
