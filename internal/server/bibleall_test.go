package server_test

import (
	"net/http"
	"strings"
	"testing"
)

// bibleAllMux adds what reading every bible needs: the language's bible list
// and the chapter in a second edition.
func bibleAllMux(t *testing.T) *http.ServeMux {
	mux := wolMux(t)
	mux.HandleFunc("/en/wol/bibles/r1/lp-e", wolFixture(t, "bibles_en.html"))
	mux.HandleFunc("/en/wol/b/r1/lp-e/nwt/43/3", wolFixture(t, "chapter_john3.html"))
	mux.HandleFunc("/en/wol/b/r1/lp-e/int/43/3", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><p>The Greek Scriptures only.</p></body></html>`))
	})
	return mux
}

// The bible form says whether every translation is read, and sends it again.
func TestUIBibleAllTranslations(t *testing.T) {
	srv := newTestServer(t, bibleAllMux(t))
	_, body := get(t, srv, "/bible?ref=John+3:16&lang=en")
	if !strings.Contains(body, `<input type="checkbox" name="all" value="1"> All translations`) {
		t.Errorf("no unticked box:\n%s", body)
	}
	resp, body := get(t, srv, "/bible?ref=John+3:16&lang=en&all=1")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d:\n%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `<input type="checkbox" name="all" value="1" checked> All translations`) {
		t.Errorf("the box is not ticked:\n%s", body)
	}
	if n := strings.Count(body, "God loved the world so much"); n != 2 {
		t.Errorf("verse shown %d times, want 2", n)
	}
	// the chapter links read on in every translation
	if !strings.Contains(body, `<a class="btn prev" href="/bible?all=1&amp;bible=nwtsty&amp;ref=John&#43;2&amp;lang=en"`) {
		t.Errorf("chapter links drop the translations:\n%s", body)
	}
}
