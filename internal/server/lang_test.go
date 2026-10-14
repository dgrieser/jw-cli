package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// getCookie fetches a path with a lang cookie set, the way a browser that
// picked a language before asks.
func getCookie(t *testing.T, srv *httptest.Server, path, lang string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: "lang", Value: lang})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}

// Only a language a page names and that resolves is remembered: a bad one
// would fail every later request, and the API names its language per call.
func TestLanguageRemembered(t *testing.T) {
	srv := newTestServer(t, languagesMux(t))
	resp, _ := get(t, srv, "/?lang=zz")
	if c := resp.Header.Get("Set-Cookie"); c != "" {
		t.Errorf("an unknown language was remembered: %q", c)
	}
	resp, _ = get(t, srv, "/api/v1/languages?lang=de")
	if c := resp.Header.Get("Set-Cookie"); c != "" {
		t.Errorf("an API call changed the remembered language: %q", c)
	}
	resp, _ = get(t, srv, "/?lang=de")
	if c := resp.Header.Get("Set-Cookie"); !strings.Contains(c, "lang=de") {
		t.Errorf("a page's language was not remembered: %q", c)
	}

	// a remembered language that no longer resolves is passed over, on a
	// page and in the API alike
	resp, body := getCookie(t, srv, "/", "zz")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `<html lang="en">`) {
		t.Errorf("page with a stale cookie: status %d", resp.StatusCode)
	}
	resp, body = getCookie(t, srv, "/api/v1/bible/books", "zz")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("API with a stale cookie: status %d: %.300s", resp.StatusCode, body)
	}
	// one that resolves still counts
	if _, body = getCookie(t, srv, "/", "de"); !strings.Contains(body, `<html lang="de">`) {
		t.Errorf("a good cookie is ignored: %.300s", body)
	}
}
