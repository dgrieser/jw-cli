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
	mux.HandleFunc("/en/wol/pc/r1/lp-e/1204433/5/0", func(w http.ResponseWriter, r *http.Request) {
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
