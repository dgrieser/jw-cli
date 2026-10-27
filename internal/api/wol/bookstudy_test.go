package wol

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/model"
)

func studyClient(t *testing.T) *Client {
	mux := http.NewServeMux()
	mux.HandleFunc("/en/wol/d/r1/lp-e/1101990062", serveFile(t, "testdata/study_si_gen.html"))
	mux.HandleFunc("/en/wol/d/r1/lp-e/2007722", serveFile(t, "testdata/study_w_joel.html"))
	mux.HandleFunc("/en/wol/d/r1/lp-e/1200003825", serveFile(t, "testdata/study_it_sam.html"))
	mux.HandleFunc("/en/wol/d/r1/lp-e/1200002205", serveFile(t, "testdata/study_it_isa.html"))
	mux.HandleFunc("/en/wol/d/r1/lp-e/1102026526", serveFile(t, "testdata/study_gwt_eze.html"))
	// the handbook has not come out with Genesis yet
	mux.HandleFunc("/en/wol/d/r1/lp-e/1102026501", http.NotFound)
	return testClient(t, mux)
}

// studyLines is a compact picture of a study's parts: title, verses,
// chapters, link; its items indented under them.
func studyLines(parts []model.StudyPart) string {
	var b strings.Builder
	for _, p := range parts {
		fmt.Fprintf(&b, "%s [%s] %v %s\n", p.Title, p.Label, p.Chapters, p.URL[strings.IndexByte(p.URL, '#')+1:])
		for _, it := range p.Items {
			fmt.Fprintf(&b, "  %s| %s [%s] %v %s\n", it.Group, it.Title, it.Label, it.Chapters, it.URL[strings.IndexByte(it.URL, '#')+1:])
		}
	}
	return b.String()
}

func readStudies(t *testing.T, pub string, book int) []model.BookStudy {
	t.Helper()
	studies, err := studyClient(t).BookStudies(context.Background(), cfgEN, pub, book)
	if err != nil {
		t.Fatal(err)
	}
	return studies
}

func wantLines(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in\n%s", w, got)
		}
	}
}

// The "Inspired" book's summary of a book is the run of paragraphs that name
// the verses they cover; the paragraphs after one belong to it, and a
// paragraph elsewhere quoting a verse is no part.
func TestBookStudyInspired(t *testing.T) {
	studies := readStudies(t, model.StudyInspired, 1)
	if len(studies) != 1 || studies[0].Title != "Bible Book Number 1—Genesis" || studies[0].Book != 1 {
		t.Fatalf("studies: %+v", studies)
	}
	got := studyLines(studies[0].Parts)
	wantLines(t, got,
		"Creation of the heavens and the earth, and the preparation of the earth for human habitation [1:1–2:25] [1 2] h=15:0-15:0\n",
		"Sin and death enter the world; “seed” foretold as deliverer [3:1–5:5] [3 4 5] h=16:0-17:0\n",
		"Wicked men and angels ruin the earth; God brings the Deluge [5:6–11:9] [5 6 7 8 9 10 11] h=18:0-21:0\n",
	)
	if n := strings.Count(got, "\n"); n != 4 {
		t.Errorf("want 4 parts, got %d:\n%s", n, got)
	}
}

// The handbook's summary is broken up by boxes, and English writes the verses
// after the title's full stop; a book it has not come out with is no study,
// and no error.
func TestBookStudyHandbook(t *testing.T) {
	studies := readStudies(t, model.StudyHandbook, 26)
	if len(studies) != 1 {
		t.Fatalf("studies: %+v", studies)
	}
	wantLines(t, studyLines(studies[0].Parts),
		"Jehovah commissions Ezekiel as a prophet [1:1–3:27] [1 2 3] h=23:0-23:0\n",
		"Ezekiel prophesies judgment against Jerusalem and its rebellious people [4:1–24:27] [4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24]",
	)
	if studies := readStudies(t, model.StudyHandbook, 1); len(studies) != 0 {
		t.Errorf("a book the handbook lacks: %+v", studies)
	}
}

// An article of highlights on two books keeps each one's sections to it, the
// questions and lessons on its verses as items under their subheadings.
func TestBookStudyHighlights(t *testing.T) {
	joel := readStudies(t, model.StudyHighlights, 29)
	if len(joel) != 1 || len(joel[0].Parts) != 1 {
		t.Fatalf("Joel: %+v", joel)
	}
	wantLines(t, studyLines(joel[0].Parts),
		"“ALAS FOR THE DAY”—WHY? [Joel 1:1–3:21] [1 2 3] h=5:0-19:0\n",
		"  Scriptural Questions Answered:| What is “the day of Jehovah”? [1:15; 2:1, 11, 31; 3:14] [1 2 3] h=10:0-10:0\n",
		"  Lessons for Us:| Genuine repentance and acknowledgment of Jehovah as the true God are essential for salvation. [1:13, 14] [1] h=15:0-15:0\n",
	)
	// a question comes with its answer
	var answer string
	for _, it := range joel[0].Parts[0].Items {
		if it.Label == "2:32" {
			answer = it.Text
		}
	}
	if want := "To call on God’s name means to know that name, to respect it deeply, and to rely upon and trust in the bearer of that name.—Romans 10:13, 14."; answer != want {
		t.Errorf("answer to 2:32: %q, want %q", answer, want)
	}
	amos := readStudies(t, model.StudyHighlights, 30)
	if len(amos) != 1 || len(amos[0].Parts) != 1 || !strings.HasPrefix(amos[0].Parts[0].Title, "“GET READY TO MEET YOUR GOD”") {
		t.Fatalf("Amos: %+v", amos)
	}
	wantLines(t, studyLines(amos[0].Parts),
		"  Scriptural Questions Answered:| Who are pictured by the “cows of Bashan”? [4:1] [4] h=25:0-25:0\n",
	)
}

// Insight's entry on books written as one work has a box of highlights for
// each in turn; a heading's lines name no verses of their own.
func TestBookStudyInsightBoxes(t *testing.T) {
	first := readStudies(t, model.StudyInsight, 9)
	second := readStudies(t, model.StudyInsight, 10)
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("Samuel: %+v %+v", first, second)
	}
	got := studyLines(first[0].Parts)
	wantLines(t, got, "Jehovah raises up Samuel as prophet in Israel [1:1–7:17] [1 2 3 4 5 6 7] h=18:0-23:0\n")
	if strings.Contains(got, "Hebron") {
		t.Errorf("the second book's box in the first's:\n%s", got)
	}
	wantLines(t, studyLines(second[0].Parts), "David becomes king and rules from Hebron [1:1–4:12] [1 2 3 4]")
	if items := first[0].Parts[0].Items; len(items) == 0 || items[0].Chapters != nil {
		t.Errorf("a heading's lines: %+v", items)
	}
}

// A box as the library lays it out now: its headings open in bold.
func TestBookStudyInsightNewLayout(t *testing.T) {
	studies := readStudies(t, model.StudyInsight, 23)
	if len(studies) != 1 {
		t.Fatalf("Isaiah: %+v", studies)
	}
	got := studyLines(studies[0].Parts)
	wantLines(t, got,
		"The guilt of Judah and Jerusalem; Isaiah’s commission [1:1–6:13] [1 2 3 4 5 6] h=22:0-28:0\n",
		"Threatened enemy invasions and promise of relief [7:1–12:6] [7 8 9 10 11 12] h=29:0-31:0\n",
	)
	if strings.Contains(got, "Prophecies to stimulate") {
		t.Errorf("the box's description taken for a heading:\n%s", got)
	}
}

// A part comes with its paragraphs as the document has them: a section's
// heading with its verses, a question with its answer.
func TestBookStudyKeepsParagraphs(t *testing.T) {
	joel := readStudies(t, model.StudyHighlights, 29)[0].Parts[0]
	if !strings.Contains(joel.HTML, `class="ss"`) || !strings.Contains(joel.HTML, "Joel 1:1–3:21") {
		t.Errorf("section heading: %s", joel.HTML)
	}
	if it := joel.Items[0]; !strings.HasPrefix(it.HTML, `<p class="sb" data-pid="10"`) || !strings.Contains(it.HTML, "Babylon the Great") {
		t.Errorf("question and answer: %s", it.HTML)
	}
	si := readStudies(t, model.StudyInspired, 1)[0].Parts[1]
	if strings.Count(si.HTML, "<p ") != 2 || !strings.Contains(si.HTML, "The ravages of sin continue outside Eden") {
		t.Errorf("a part of the summary and the paragraph after it: %s", si.HTML)
	}
}

// A summary that names no verses but has a paragraph for each chapter — the
// "Inspired" book's on Lamentations — goes through the chapters in turn.
func TestBookStudyChapterByChapter(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<article id="article"><p class="sb" data-pid="1">Introduction, quoting (2:3).</p><p class="ss" data-pid="2">CONTENTS</p>`)
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&b, `<p class="sb" data-pid="%d">%d Poem %d laments. More.</p>`, 10+i, 7+i, i)
	}
	b.WriteString(`<p class="ss" data-pid="20">WHY BENEFICIAL</p><p class="sb" data-pid="21">It helps.</p></article>`)
	mux := http.NewServeMux()
	mux.HandleFunc("/en/wol/d/r1/lp-e/1101990086", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, b.String()) })
	studies, err := testClient(t, mux).BookStudies(context.Background(), cfgEN, model.StudyInspired, 25)
	if err != nil || len(studies) != 1 {
		t.Fatalf("%v %+v", err, studies)
	}
	wantLines(t, studyLines(studies[0].Parts), "Poem 1 laments. [] [1] h=11:0-11:0\n", "Poem 5 laments. [] [5] h=15:0-15:0\n")
}

func TestRefChapters(t *testing.T) {
	for _, tc := range []struct {
		ref  string
		book int
		want []int
	}{
		{"1:1–2:25", 1, []int{1, 2}},
		{"11:10 bis 25:26", 1, []int{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25}},
		{"3:1-5, 16-23", 1, []int{3}},
		{"3:18, 19; 5:5; 6:7; 7:23", 1, []int{3, 5, 6, 7}},
		{"1. Mose 1:1 bis 7:24", 1, []int{1, 2, 3, 4, 5, 6, 7}},
		{"Psalm 1\u2060—\u206041", 19, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41}},
		{"vss. 1-16", 65, []int{1}},
		{"Philem. 1-25", 57, []int{1}},
		{"1:1-17; 6:8-13; 7:8", 3, []int{1, 6, 7}},
		{"50:20–52:3", 1, []int{50}}, // Genesis has 50 chapters
		{"no verses", 1, nil},
	} {
		if got := refChapters(tc.ref, tc.book); !slices.Equal(got, tc.want) {
			t.Errorf("refChapters(%q, %d) = %v, want %v", tc.ref, tc.book, got, tc.want)
		}
	}
}

func TestSplitSentence(t *testing.T) {
	for in, want := range map[string][2]string{
		"How could God create light? The Hebrew word ...":                        {"How could God create light?", "The Hebrew word ..."},
		"Jehovah was pleased with Abel (Hebrews 11:4). Cain, though, ...":        {"Jehovah was pleased with Abel (Hebrews 11:4).", "Cain, though, ..."},
		"Peleg lived from 2269 to 2030 B.C.E. and in his days ...":               {"Peleg lived from 2269 to 2030 B.C.E. and in his days ...", ""},
		"Satan has “the means . . . to cause death.” Does that mean he can ...?": {"Satan has “the means . . . to cause death.” Does that mean he can ...?", ""},
		"What is meant by “the day of Jehovah?” It is the time ...":              {"What is meant by “the day of Jehovah?”", "It is the time ..."},
	} {
		if first, rest := splitSentence(in); first != want[0] || rest != want[1] {
			t.Errorf("splitSentence(%q) = %q, %q, want %q, %q", in, first, rest, want[0], want[1])
		}
	}
	long := strings.Repeat("word ", 100)
	first, rest := splitSentence(long)
	words := len(strings.Fields(strings.TrimSuffix(first, "…") + " " + strings.TrimPrefix(rest, "…")))
	if !strings.HasSuffix(first, "…") || len([]rune(first)) > 222 || !strings.HasPrefix(rest, "…") || words != 100 {
		t.Errorf("a long sentence is not cut short, the rest going on: %q / %q", first, rest)
	}
}
