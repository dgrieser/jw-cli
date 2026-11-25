package cli

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// whatsNewMux answers jw.org's page of what is new with an article of a
// book, an issue of the Watchtower and a news report, and pub-media with
// files for the first two.
func whatsNewMux(t *testing.T) *http.ServeMux {
	mux := languagesMux(t)
	mux.HandleFunc("/finder", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("docid") != "1011267" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body>
<div class="synopsis publications pub-gwt docId-1102026536 docClass-13 PublicationArticle">
  <p class="pubDate">2026-11-24</p><p class="contextTitle">GOD'S WORD IS TRUTH</p>
  <h3><a href="/en/library/books/gods-word-is-truth/zephaniah/">Bible Book 36: Zephaniah</a></h3>
</div>
<div class="synopsis publications pub-w iss-202702 PublicationIssue">
  <p class="pubDate">2026-11-17</p><p class="contextTitle">THE WATCHTOWER</p>
  <h3><a href="/en/library/magazines/watchtower-study-february-2027/">February 2027</a></h3>
</div>
<div class="synopsis news pub-ijwnews26 docId-702026178 NewsArticlePage">
  <p class="pubDate">2026-11-16</p><p class="contextTitle">NEWS</p>
  <h3><a href="/en/news/x/">A report</a></h3>
</div>
</body></html>`)
	})
	mux.HandleFunc("/apis/pub-media/GETPUBMEDIALINKS", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var name string
		switch {
		case q.Get("docid") == "1102026536" && q.Get("pub") == "":
			name = "gwt_E_1102026536.pdf"
		case q.Get("pub") == "w" && q.Get("issue") == "202702":
			name = "w_E_202702.pdf"
		default:
			http.Error(w, "unexpected query "+r.URL.RawQuery, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"pubName": "p", "files": {"E": {"PDF": [{
			"title": "t", "file": {"url": "http://%s/files/%s", "checksum": ""}, "filesize": 4, "mimetype": "application/pdf"
		}]}}}`, r.Host, name)
	})
	mux.HandleFunc("/files/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("file"))
	})
	return mux
}

// What jw new lists downloads as a search result does: an article by its
// document, an issue by its publication and issue; a news report has no
// files.
func TestDownloadNewResult(t *testing.T) {
	mux := whatsNewMux(t)
	cacheDir := t.TempDir()
	out, err := runCmdWithCache(t, mux, cacheDir, "new", "-l", "en", "--kind", "pub")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "Bible Book 36: Zephaniah") || strings.Contains(out, "A report") {
		t.Fatalf("listing:\n%s", out)
	}
	for index, want := range map[string]string{
		"1": "gwt_E_1102026536.pdf", // the article, by its docid
		"2": "w_E_202702.pdf",       // the issue, by pub and issue
	} {
		out, err := runCmdWithCache(t, mux, cacheDir, "download", index, "-l", "en", "-d", t.TempDir())
		if err != nil {
			t.Errorf("download %s: %v\n%s", index, err, out)
			continue
		}
		if !strings.Contains(out, want) {
			t.Errorf("download %s: want %s in\n%s", index, want, out)
		}
	}

	if _, err := runCmdWithCache(t, mux, cacheDir, "new", "-l", "en", "--kind", "news"); err != nil {
		t.Fatal(err)
	}
	_, err = runCmdWithCache(t, mux, cacheDir, "download", "1", "-l", "en", "-d", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "nothing downloadable") {
		t.Errorf("news: want nothing downloadable, got %v", err)
	}
}

// A jw.org that turns the reader away with a page of its own is an error
// for jw new --kind pub, which has nothing else to list, not an empty list.
func TestNewSaysWhenJWOrgTurnsAway(t *testing.T) {
	mux := languagesMux(t)
	mux.HandleFunc("/finder", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><p>Access Denied</p></body></html>`)
	})
	out, err := runCmd(t, mux, "new", "-l", "en", "--kind", "pub")
	if err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Errorf("want jw.org's list unread, got %v\n%s", err, out)
	}
}
