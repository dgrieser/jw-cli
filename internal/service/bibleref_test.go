package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/dgrieser/jw-cli/internal/api/pubmedia"
	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
)

// bibleUpstream serves the English library with John 3 (verses 16 and 17
// only) in nwtsty and its list of three bibles. Every other chapter page is
// refused with a bare 400, as wol refuses an edition it does not know and a
// chapter an edition does not have.
func bibleUpstream(t *testing.T) *Service {
	t.Helper()
	file := func(path string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("fixture %s: %v", path, err)
			}
			w.Write(b)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/en", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/en/wol/h/r1/lp-e", http.StatusFound)
	})
	mux.HandleFunc("/en/wol/h/r1/lp-e", func(w http.ResponseWriter, r *http.Request) {})
	// Kachin has a library, but neither a bible list nor a bible page
	mux.HandleFunc("/kac", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/kac/wol/h/r539/lp-ah", http.StatusFound)
	})
	mux.HandleFunc("/kac/wol/h/r539/lp-ah", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("/en/wol/bibles/r1/lp-e", file("testdata/bibles_en.html"))
	mux.HandleFunc("/en/wol/b/r1/lp-e/nwtsty/43/3", file("testdata/chapter_john3.html"))
	mux.HandleFunc("/en/wol/b/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "", http.StatusBadRequest)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	hc := httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{CDN: srv.URL, JWOrg: srv.URL, WOL: srv.URL}))
	return New(hc, httpx.OpenCacheAt(t.TempDir()))
}

var english = model.Language{Symbol: "E", Locale: "en", Name: "English"}

// A reference past the end of what is there is answered in words, and as the
// mistake it is: a chapter or verse the book does not have, an edition the
// language does not have, are bad input (untyped, so the server answers 400);
// a chapter the edition does not cover is not there (pubmedia.ErrNotFound, so
// the server answers 404). None of them reaches the reader as a raw HTTP 400.
func TestReadPassagesErrors(t *testing.T) {
	s := bibleUpstream(t)
	ctx := context.Background()
	txt := i18n.TextFor("en")
	for _, c := range []struct {
		ref, edition string
		want         string
		notFound     bool
	}{
		{"Joh 22", "nwtsty", "there is no John 22: John has 21 chapters", false},
		{"Joh 3:99", "nwtsty", "there is no John 3:99: John 3 has 17 verses", false},
		{"Jude 2:1", "nwtsty", "there is no Jude 2: Jude has only one chapter", false},
		{"Joh 3:16", "bogus", `there is no bible edition "bogus" in English; the bibles in English are nwtsty, nwt, int`, false},
		{"Ge 1:1", "int", "Genesis 1 is not in The Kingdom Interlinear Translation of the Greek Scriptures (int, 1985)", true},
	} {
		_, err := s.ReadPassages(ctx, english, ReadRequest{Refs: c.ref, Edition: c.edition, NoOutlines: true}, txt)
		if err == nil {
			t.Errorf("%s in %s: want an error", c.ref, c.edition)
			continue
		}
		if err.Error() != c.want {
			t.Errorf("%s in %s: got %q, want %q", c.ref, c.edition, err, c.want)
		}
		var se *httpx.StatusError
		if errors.As(err, &se) {
			t.Errorf("%s in %s: the upstream status leaked: %v", c.ref, c.edition, err)
		}
		if got := errors.Is(err, pubmedia.ErrNotFound); got != c.notFound {
			t.Errorf("%s in %s: not found = %v, want %v", c.ref, c.edition, got, c.notFound)
		}
	}
}

// A range running past the chapter's last verse ends with it, and the passage
// is headed with the verses it shows.
func TestReadPassagesClampsRangeEnd(t *testing.T) {
	s := bibleUpstream(t)
	for _, ref := range []string{"Joh 3:16-3:99", "Joh 3:16-99"} {
		res, err := s.ReadPassages(context.Background(), english,
			ReadRequest{Refs: ref, Edition: "nwtsty", NoOutlines: true}, i18n.TextFor("en"))
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Passages) != 1 || res.Passages[0].Ref != "John 3:16-17" || len(res.Passages[0].Verses) != 2 {
			t.Errorf("%s: got %+v", ref, res.Passages)
		}
	}
}

// A language with publications in the library but no bible says so, and as
// something that is not there.
func TestReadPassagesNoBible(t *testing.T) {
	s := bibleUpstream(t)
	kachin := model.Language{Symbol: "AH", Locale: "kac", Name: "Kachin"}
	_, err := s.ReadPassages(context.Background(), kachin,
		ReadRequest{Refs: "Joh 3:16", Edition: "nwtsty", NoOutlines: true}, i18n.TextFor("en"))
	if err == nil || err.Error() != "the library has no bible in Kachin" || !errors.Is(err, pubmedia.ErrNotFound) {
		t.Errorf("got %v", err)
	}
}
