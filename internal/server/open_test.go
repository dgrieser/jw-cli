package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/i18n"

	"github.com/dgrieser/jw-cli/internal/bibleref"
)

func TestVidRef(t *testing.T) {
	for _, c := range []struct {
		vid  int
		to   string
		want bibleref.Ref
		ok   bool
	}{
		{43005029, "", bibleref.Ref{Book: 43, Chapter: 5, VerseStart: 29, VerseEnd: 29}, true},
		{43005029, "43005030", bibleref.Ref{Book: 43, Chapter: 5, VerseStart: 29, VerseEnd: 30}, true},
		// a passage running into the next chapter opens where it starts
		{43005029, "43006002", bibleref.Ref{Book: 43, Chapter: 5, VerseStart: 29, VerseEnd: 29}, true},
		{43005029, "43005001", bibleref.Ref{Book: 43, Chapter: 5, VerseStart: 29, VerseEnd: 29}, true},
		{67001001, "", bibleref.Ref{}, false},
		{43000001, "", bibleref.Ref{}, false},
		{43005000, "", bibleref.Ref{}, false},
	} {
		got, ok := vidRef(c.vid, c.to)
		if ok != c.ok || got != c.want {
			t.Errorf("vidRef(%d, %q) = %+v, %v; want %+v, %v", c.vid, c.to, got, ok, c.want, c.ok)
		}
	}
}

func TestRenderKeep(t *testing.T) {
	s := New(Config{})
	for _, c := range []struct {
		err  string
		want string
	}{{"", ""}, {"upstream failed", "no"}} {
		w := httptest.NewRecorder()
		page := errorPage{basePage: basePage{Title: "x", Path: "/search", T: i18n.EN.Text(), Error: c.err}}
		s.render(w, http.StatusOK, "error", page)
		if got := w.Header().Get(keepHeader); got != c.want {
			t.Errorf("Error %q: %s = %q, want %q", c.err, keepHeader, got, c.want)
		}
	}
}

func TestServiceWorkerPolicy(t *testing.T) {
	for _, c := range []struct {
		name   string
		policy string
		users  bool
		header string
		want   string
	}{
		{"open", "", false, "", PageCacheFirst},
		{"own login", "", true, "", PageCacheFallback},
		{"proxy basic auth", "", false, "Authorization", PageCacheFallback},
		{"proxy user", PageCacheAuto, false, "X-Forwarded-User", PageCacheFallback},
		{"asked first", PageCacheFirst, true, "", PageCacheFirst},
		{"asked fallback", PageCacheFallback, false, "", PageCacheFallback},
		{"off", PageCacheOff, false, "", PageCacheOff},
	} {
		var creds Credentials
		if c.users {
			creds.Add("anne", "secret")
		}
		s := New(Config{Auth: &creds, PageCache: c.policy})
		r := httptest.NewRequest(http.MethodGet, "/sw.js", nil)
		if c.header != "" {
			r.Header.Set(c.header, "anne")
		}
		w := httptest.NewRecorder()
		s.serviceWorker(w, r)
		body := w.Body.String()
		if want := `var POLICY = "` + c.want + `";`; !strings.Contains(body, want) {
			t.Errorf("%s: want %q in the worker", c.name, want)
		}
		if strings.Contains(body, "__POLICY__") || strings.Contains(body, "__BUILD__") {
			t.Errorf("%s: the worker is not stamped", c.name)
		}
	}
}
