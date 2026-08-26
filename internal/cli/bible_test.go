package cli

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func bibleMux(t *testing.T) *http.ServeMux {
	mux := languagesMux(t)
	mux.HandleFunc("/en", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<a href="/en/wol/h/r1/lp-e">home</a>`))
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
	mux.HandleFunc("/en/wol/marginalreference/r1/lp-e/nwtsty/43/3/96", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<ul><li>For God so loved... (Ge 22:2)</li></ul>`))
	})
	// the citation JSON lives at the locale-less path; /en/wol/pc/... is a
	// navigation redirect to the target page
	mux.HandleFunc("/wol/pc/r1/lp-e/1204433/5/0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items": [{"content": "<p>Jehovah loved the world of redeemable mankind.</p>",
			"title": "God So Loved the World", "url": "/en/wol/d/r1/lp-e/2014486"}]}`))
	})
	return mux
}

func TestBibleRead(t *testing.T) {
	out, err := runCmd(t, bibleMux(t), "bible", "read", "-l", "en", "John 3:16")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "## John 3:16") || !strings.Contains(out, "God loved the world so much") {
		t.Fatalf("read output:\n%s", out)
	}
}

func TestBibleReadRangeText(t *testing.T) {
	out, err := runCmd(t, bibleMux(t), "bible", "read", "-l", "en", "-o", "text", "joh 3:16-17")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "John 3:16-17") || !strings.Contains(out, "did not send his Son") {
		t.Fatalf("range output:\n%s", out)
	}
	if strings.Contains(out, "<") {
		t.Errorf("html leaked:\n%s", out)
	}
}

// TestBibleReadUnfold covers the study material of the verse being read: the
// notes print under the verse, the research-guide passage it points at is
// unfolded, and the entry that names a whole article is listed instead.
func TestBibleReadUnfold(t *testing.T) {
	out, err := runCmd(t, bibleMux(t), "bible", "read", "-l", "en", "-o", "raw", "--unfold", "1", "John 3:16")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## John 3:16",
		"God loved the world so much",
		"### Study notes",
		"**loved:**",
		"**everlasting life:**",
		"### Research guide",
		"Insight, Volume 2, page 274",
		"### References",
		"Jehovah loved the world of redeemable mankind",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestBibleReadUnfoldInlinePerVerse pins where the study material of a verse
// goes: under that verse, not after the whole passage. The second verse follows
// the expansion of the first, and brings its own.
func TestBibleReadUnfoldInlinePerVerse(t *testing.T) {
	out, err := runCmd(t, bibleMux(t), "bible", "read", "-l", "en", "-o", "raw", "--unfold", "1", "joh 3:16-17")
	if err != nil {
		t.Fatal(err)
	}
	// every verse is headed, so its expansion reads as belonging to it
	for _, want := range []string{"## John 3:16-17", "### John 3:16", "### John 3:17", "#### Study notes"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// the fixture gives verse 16 study notes and a research-guide passage and
	// verse 17 a marginal reference, so verse 16 brings the study material
	first, second := strings.Index(out, "### John 3:16\n"), strings.Index(out, "### John 3:17")
	note16 := strings.Index(out, "**loved:**")
	text17 := strings.Index(out, "did not send his Son")
	if first >= note16 || note16 >= second || second >= text17 {
		t.Errorf("verse 16 %d, its note %d, verse 17 %d, its text %d: the study material of a verse "+
			"belongs between that verse and the next:\n%s", first, note16, second, text17, out)
	}
}

// TestBibleReadUnfoldJSON keeps the expansion in the data model, not only in the
// rendered page.
func TestBibleReadUnfoldJSON(t *testing.T) {
	out, err := runCmd(t, bibleMux(t), "bible", "read", "-l", "en", "-o", "json", "--unfold", "1", "John 3:16")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"unfold"`) || !strings.Contains(out, "Study notes") {
		t.Errorf("json missing the expansion:\n%s", out)
	}
}

// TestBibleReadWithoutUnfoldStaysBare pins that the study material is something
// --unfold asks for: reading a verse still prints the verse.
func TestBibleReadWithoutUnfoldStaysBare(t *testing.T) {
	out, err := runCmd(t, bibleMux(t), "bible", "read", "-l", "en", "John 3:16")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Study notes") || strings.Contains(out, "References") {
		t.Errorf("unasked-for study material:\n%s", out)
	}
}

func TestBibleNotes(t *testing.T) {
	out, err := runCmd(t, bibleMux(t), "bible", "notes", "-l", "en", "John 3:16")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## John 3:16", "**loved:**", "everlasting life"} {
		if !strings.Contains(out, want) {
			t.Errorf("notes missing %q:\n%s", want, out)
		}
	}
}

func TestBibleNotesWholeChapter(t *testing.T) {
	out, err := runCmd(t, bibleMux(t), "bible", "notes", "-l", "en", "John 3")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "**loved:**") {
		t.Fatalf("whole-chapter notes output:\n%s", out)
	}
}

func TestBibleXrefs(t *testing.T) {
	out, err := runCmd(t, bibleMux(t), "bible", "xrefs", "-l", "en", "John 3:16")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Ge 22:2, 16; Joh 1:14") {
		t.Fatalf("xrefs output:\n%s", out)
	}
}

func TestBibleXrefsResolve(t *testing.T) {
	out, err := runCmd(t, bibleMux(t), "bible", "xrefs", "-l", "en", "-r", "John 3:16")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "For God so loved") {
		t.Fatalf("resolved xrefs output:\n%s", out)
	}
}

func TestBibleMedia(t *testing.T) {
	out, err := runCmd(t, bibleMux(t), "bible", "media", "-l", "en", "John 3:16")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[image]") || !strings.Contains(out, "Jesus explains birth") {
		t.Fatalf("media output:\n%s", out)
	}
}

func TestBibleResearchExcerpts(t *testing.T) {
	out, err := runCmd(t, bibleMux(t), "bible", "research", "-l", "en", "-x", "John 3:16")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"God So Loved the World", "redeemable mankind", "/en/wol/d/r1/lp-e/2014486"} {
		if !strings.Contains(out, want) {
			t.Errorf("research missing %q:\n%s", want, out)
		}
	}
}

func TestBibleBooks(t *testing.T) {
	out, err := runCmd(t, bibleMux(t), "bible", "books", "-l", "en")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, " 1. Genesis") || !strings.Contains(out, "66. Revelation") {
		t.Fatalf("books output:\n%s", out)
	}
}
