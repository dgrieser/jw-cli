package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/bibleref"
	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/unfold"
)

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

// unfoldedHTML renders an expansion the way a document block carries it: the
// references heading with its nodes, and the note closing an expansion that was
// cut short. What the cases below are about is that shape, not where in the
// document it is inlined.
func unfoldedHTML(res unfold.Result, txt *i18n.Messages) string {
	body := unfoldNodesHTML(res.Nodes, txt, documentUnfoldLevel)
	if body == "" {
		return ""
	}
	return body + unfoldNoteHTML(stoppedNote(res.Stopped, res.Pending, txt))
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
	out := unfoldedHTML(res, txt)
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
	if unfoldedHTML(unfold.Result{}, txt) != "" {
		t.Error("nothing expanded should append nothing")
	}
}

// TestUnfoldInlinesNestedCitations pins that a passage carries what it cites the
// same way a document does: under the paragraph citing it, not after the whole
// passage.
func TestUnfoldInlinesNestedCitations(t *testing.T) {
	res := unfold.Result{Nodes: []unfold.Node{{
		Ref:   unfold.Ref{Text: "w24.11 7", Path: "/wol/pc/1/1"},
		Title: "Facing injustice",
		HTML: `<p>First, on prayer (<a href="/wol/bc/2/2">Rom 12:21</a>).</p>` +
			`<p>Then, on forgiveness (<a href="/wol/bc/3/3">Luke 23:34</a>).</p>`,
		Children: []unfold.Node{
			{Ref: unfold.Ref{Text: "Rom 12:21", Path: "/wol/bc/2/2"}, Title: "Romans 12:21",
				HTML: "<p>conquer evil with good</p>"},
			{Ref: unfold.Ref{Text: "Luke 23:34", Path: "/wol/bc/3/3"}, Title: "Luke 23:34",
				HTML: "<p>Father, forgive them</p>"},
		},
	}}}
	out := unfoldedHTML(res, i18n.EN.Text())
	first := strings.Index(out, "First, on prayer")
	romans := strings.Index(out, "conquer evil with good")
	second := strings.Index(out, "Then, on forgiveness")
	luke := strings.Index(out, "Father, forgive them")
	if first < 0 || romans < 0 || second < 0 || luke < 0 {
		t.Fatalf("paragraph %d, its verse %d, next paragraph %d, its verse %d:\n%s",
			first, romans, second, luke, out)
	}
	if first >= romans || romans >= second || second >= luke {
		t.Errorf("each verse belongs under the paragraph that cites it:\n%s", out)
	}
}

// TestUnfoldNamesTheVerseAMarkerCameFrom pins what a marginal reference is
// headed with: the verse of the passage the marker sits in, not the passage as a
// whole, which would put the same name on every reference the passage carries.
func TestUnfoldNamesTheVerseAMarkerCameFrom(t *testing.T) {
	verse := func(id, num, text, marker string) string {
		return fmt.Sprintf(`<span id="%s" class="v"><a class="vl" href="/wol/dx/%s">%s </a>%s`+
			`<a class="b" href="%s">+</a> </span>`, id, num, num, text, marker)
	}
	res := unfold.Result{Nodes: []unfold.Node{{
		Ref:   unfold.Ref{Text: "Joh 3:16, 17", Path: "/wol/bc/1/1"},
		Title: "John 3:16, 17",
		HTML: "<p>" + verse("v43-3-16-1", "16", "God loved the world", "/wol/bc/2/2") +
			verse("v43-3-17-1", "17", "God did not send his Son", "/wol/bc/3/3") + "</p>",
		Children: []unfold.Node{
			{Ref: unfold.Ref{Text: "+", Path: "/wol/bc/2/2"}, Title: "Romans 5:8",
				HTML: "<p>while we were sinners</p>"},
			{Ref: unfold.Ref{Text: "+", Path: "/wol/bc/3/3"}, Title: "1 John 4:14",
				HTML: "<p>as Savior of the world</p>"},
		},
	}}}
	out := unfoldedHTML(res, i18n.EN.Text())
	for _, want := range []string{
		"Marginal reference John 3:16 → Romans 5:8",
		"Marginal reference John 3:17 → 1 John 4:14",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Marginal reference John 3:16, 17") {
		t.Errorf("the passage as a whole is not where a marker came from:\n%s", out)
	}
}

// TestCollapseRules covers the doubled line a nested expansion leaves behind: it
// closes itself with a rule, and the expansion holding it closes with one right
// after. Only the markup of the document lies between the two, so one goes.
func TestCollapseRules(t *testing.T) {
	doubled := `<p>text</p><hr/>` + "\n</div>\n" + `<p id="p20" class="sb"></p>` + "\n<hr/>\n<p>more</p>"
	got := collapseRules(doubled)
	if strings.Count(got, "<hr") != 1 {
		t.Errorf("want one rule, got %d in:\n%s", strings.Count(got, "<hr"), got)
	}
	for _, want := range []string{"text", "more"} {
		if !strings.Contains(got, want) {
			t.Errorf("collapsing dropped %q:\n%s", want, got)
		}
	}
	// two rules with something between them are two rules
	kept := `<hr/><p>a passage</p><hr/>`
	if n := strings.Count(collapseRules(kept), "<hr"); n != 2 {
		t.Errorf("want both rules, got %d", n)
	}
	// an image is content of its own, with no text to show it
	withImage := `<hr/><p><img src="x.png"/></p><hr/>`
	if n := strings.Count(collapseRules(withImage), "<hr"); n != 2 {
		t.Errorf("an image parts two rules, got %d", n)
	}
}

// TestVerseSource covers the naming itself, including what is not bible text.
func TestVerseSource(t *testing.T) {
	for _, tc := range []struct {
		passage        string
		chapter, verse int
		want           string
	}{
		{"Jeremia 29:1-30:24", 29, 2, "Jeremia 29:2"},
		{"Jeremia 29:1-30:24", 30, 11, "Jeremia 30:11"},
		{"1. Petrus 3:8, 9", 3, 9, "1. Petrus 3:9"},
		// a passage of a publication carries no verse to name
		{"Insight, Volume 1, page 1044", 0, 0, "Insight, Volume 1, page 1044"},
		{"Hals, Nacken", 0, 0, "Hals, Nacken"},
	} {
		if got := verseSource(tc.passage, tc.chapter, tc.verse); got != tc.want {
			t.Errorf("verseSource(%q, %d, %d) = %q, want %q",
				tc.passage, tc.chapter, tc.verse, got, tc.want)
		}
	}
}

// TestUnfoldKeepsUncitedChildrenAfterThePassage is the other side of it: the
// research-guide passages of a verse are references of the verse and not of
// anything its text says, so its text has nowhere to hold them.
func TestUnfoldKeepsUncitedChildrenAfterThePassage(t *testing.T) {
	res := unfold.Result{Nodes: []unfold.Node{{
		Ref:   unfold.Ref{Text: "Joh 3:16", Path: "/wol/bc/1/1"},
		Title: "John 3:16",
		HTML:  "<p>God loved the world</p>",
		Children: []unfold.Node{{
			Ref: unfold.Ref{Text: "it-2 528", Path: "/wol/pc/9/9"}, Title: "Love",
			HTML: "<p>the Greek word</p>",
		}},
	}}}
	out := unfoldedHTML(res, i18n.EN.Text())
	verse, research := strings.Index(out, "God loved the world"), strings.Index(out, "the Greek word")
	if verse < 0 || research < 0 || verse >= research {
		t.Errorf("verse %d, research passage %d: the passage follows the verse:\n%s", verse, research, out)
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

// TestUnfoldNote separates the two ways an expansion leaves references behind.
// Being cut short did not do what was asked and is worth saying; reaching the
// requested depth did exactly that, and is not worth a word.
func TestUnfoldNote(t *testing.T) {
	txt := i18n.EN.Text()
	node := []unfold.Node{{Ref: unfold.Ref{Text: "Matt 24:14"}, HTML: "<p>x</p>"}}

	cutShort := unfoldedHTML(unfold.Result{Nodes: node, Stopped: true, Pending: 137}, txt)
	if !strings.Contains(cutShort, "Stopped here") || !strings.Contains(cutShort, "137") {
		t.Errorf("a declined expansion should say it stopped:\n%s", cutShort)
	}

	// the normal end of --unfold 1: the verses it printed cite 35 more, which
	// is the depth working as asked and needs no remark
	atDepth := unfoldedHTML(unfold.Result{Nodes: node, Pending: 35}, txt)
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
	out := unfoldedHTML(res, i18n.EN.Text())
	if strings.Contains(out, "<script>") {
		t.Errorf("heading not escaped:\n%s", out)
	}
}

func TestUnfoldHTMLReportsAFailedReference(t *testing.T) {
	res := unfold.Result{Nodes: []unfold.Node{{
		Ref: unfold.Ref{Text: "Matt 24:14"},
		Err: errTest,
	}}}
	out := unfoldedHTML(res, i18n.EN.Text())
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
	out := unfoldedHTML(unfold.Result{Nodes: []unfold.Node{node}}, i18n.EN.Text())
	for _, want := range []string{"<h3>level1</h3>", "<h6>level4</h6>", "<p><strong>level5</strong></p>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "<h7>") {
		t.Errorf("h7 does not exist:\n%s", out)
	}
}

// TestVerseRuns pins how the verses of a passage are headed: a verse that
// brought study material of its own stands under its own heading, and the
// verses around it that brought none are headed as the span they are.
func TestVerseRuns(t *testing.T) {
	verses := []Verse{
		{Verse: model.Verse{ID: 43003016, Citation: "John 3:16"}},
		{Verse: model.Verse{ID: 43003017, Citation: "John 3:17"}},
		{Verse: model.Verse{ID: 43003018, Citation: "John 3:18"}, Unfold: "<h4>References</h4>"},
		{Verse: model.Verse{ID: 43003019, Citation: "John 3:19"}},
	}
	runs := verseRuns(verses)
	if len(runs) != 3 {
		t.Fatalf("got %d runs, want the bare pair, the expanded verse and the last: %+v", len(runs), runs)
	}
	tbl := bibleref.English()
	for i, want := range []string{"John 3:16-17", "John 3:18", "John 3:19"} {
		if got := runCitation(runs[i], tbl); got != want {
			t.Errorf("run %d heading = %q, want %q", i, got, want)
		}
	}
	// a passage whose verses are not headed one by one keeps its own heading
	bare := []Verse{{Verse: model.Verse{ID: 43003016}}, {Verse: model.Verse{ID: 43003017}}}
	if got := runCitation(verseRuns(bare)[0], tbl); got != "" {
		t.Errorf("unheaded verses got the heading %q", got)
	}
}
