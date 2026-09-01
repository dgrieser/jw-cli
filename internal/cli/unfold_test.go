package cli

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/render"
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

// TestArticleUnfoldInlinesUnderTheCitingBlock pins where an expansion goes: under
// the paragraph that cites it, not in an appendix after the document.

// TestArticleUnfoldInlinesUnderTheCitingBlock pins where an expansion goes: under
// the paragraph that cites it, not in an appendix after the document.
func TestArticleUnfoldInlinesUnderTheCitingBlock(t *testing.T) {
	mux := studyUnfoldMux(t)
	mux.HandleFunc("/en/wol/d/r1/lp-e/2024361", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><div id="article">
		  <header><h1>God So Loved</h1></header>
		  <div class="bodyTxt">
		    <h2>What Jesus said</h2>
		    <p>Consider it.
		      (<a href="/en/wol/bc/r1/lp-e/2024360/0/0" class="b">Joh 3:16</a>)</p>
		    <p>And the last word.</p>
		  </div>
		</div></body></html>`))
	})
	out, err := runCmd(t, mux, "article", "2024361", "-l", "en", "-o", "raw", "--unfold", "1")
	if err != nil {
		t.Fatal(err)
	}
	cite := strings.Index(out, "Consider it.")
	refs := strings.Index(out, "### References")
	verse := strings.Index(out, "For God loved the world so much")
	rule := strings.Index(out, "\n---")
	after := strings.Index(out, "And the last word.")
	if cite < 0 || refs < 0 || verse < 0 || rule < 0 || after < 0 {
		t.Fatalf("citation %d, references %d, verse %d, rule %d, next paragraph %d:\n%s",
			cite, refs, verse, rule, after, out)
	}
	if cite >= refs || refs >= verse || verse >= rule || rule >= after {
		t.Errorf("the expansion belongs between the paragraph that cites it and the next one:\n%s", out)
	}
	// the references of a paragraph inside an h2 section sit under that section
	if strings.Contains(out, "## References") && !strings.Contains(out, "### References") {
		t.Errorf("the heading should sit below the section it is in:\n%s", out)
	}
}

// TestArticleUnfoldOneLevelKeepsResearchPending pins the request curve: the
// notes come with the verse, the passages it references wait for level two.

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
