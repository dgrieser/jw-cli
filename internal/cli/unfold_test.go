package cli

import (
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/i18n"
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

func TestUnfoldHeading(t *testing.T) {
	for _, tc := range []struct {
		name string
		node unfold.Node
		want string
	}{
		// a publication passage: the citation points, the title says what it is
		{"publication citation and title",
			unfold.Node{Ref: unfold.Ref{Text: "6 Abs. 15", Path: "/wol/pc/1/2"}, Title: "Vertraue dem Richter"},
			"6 Abs. 15: Vertraue dem Richter"},
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
		// a cross reference inside a verse is written as a bare marker, so only
		// the resolved content says which passage it was
		{"marker falls back to title even for a verse",
			unfold.Node{Ref: unfold.Ref{Text: "+", Path: "/wol/bc/9/9"}, Title: "Isaiah 2:2"}, "Isaiah 2:2"},
		{"asterisk marker", unfold.Node{Ref: unfold.Ref{Text: "*"}, Title: "Daniel 12:13"}, "Daniel 12:13"},
		{"empty falls back to title", unfold.Node{Title: "Acts 24:15"}, "Acts 24:15"},
		{"marker with no title stays", unfold.Node{Ref: unfold.Ref{Text: "+"}}, "+"},
	} {
		if got := unfoldHeading(tc.node); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
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
		"<h4>Isaiah 2:2</h4>",
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
