package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWOLBrowserHeaders(t *testing.T) {
	var seen http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
	}))
	defer srv.Close()

	c := New(WithBaseURLs(BaseURLs{WOL: srv.URL}))
	request := func(path string, h http.Header) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header = h
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	for _, path := range []string{"/", "/en/wol/h/r1/lp-e"} {
		request(path, http.Header{})
		for name, want := range map[string]string{
			"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/jxl,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
			"Accept-Language":           "en,en-US;q=0.9,de-DE;q=0.8,de;q=0.7",
			"Cache-Control":             "no-cache",
			"Pragma":                    "no-cache",
			"Priority":                  "u=0, i",
			"User-Agent":                defaultUserAgent,
			"Sec-CH-UA":                 `"Google Chrome";v="155", "Chromium";v="155", "Not(A:Brand";v="24"`,
			"Sec-Fetch-Dest":            "document",
			"Sec-Fetch-Mode":            "navigate",
			"Sec-Fetch-Site":            "none",
			"Sec-Fetch-User":            "?1",
			"Upgrade-Insecure-Requests": "1",
			"Sec-CH-UA-Mobile":          "?0",
			"Sec-CH-UA-Platform":        `"Linux"`,
		} {
			if got := seen.Get(name); got != want {
				t.Errorf("%s: %s = %q, want %q", path, name, got, want)
			}
		}
		cookieRequest := &http.Request{Header: seen}
		for name, want := range map[string]string{
			"cookieConsent-STRICTLY_NECESSARY": "true",
			"cookieConsent-FUNCTIONAL":         "true",
			"cookieConsent-DIAGNOSTIC":         "true",
			"cookieConsent-USAGE":              "true",
			"fontScale":                        "1",
			"binavview":                        "grid",
		} {
			cookie, err := cookieRequest.Cookie(name)
			if err != nil || cookie.Value != want {
				t.Errorf("%s: cookie %s = %v, %v", path, name, cookie, err)
			}
		}
	}

	h := XHRHeader()
	h.Set("Accept", "application/json")
	request("/en/wol/bc/r1/lp-e/1", h)
	for name, want := range map[string]string{
		"Accept":           "application/json",
		"X-Requested-With": "XMLHttpRequest",
		"Sec-Fetch-Dest":   "empty",
		"Sec-Fetch-Mode":   "cors",
		"Sec-Fetch-Site":   "same-origin",
	} {
		if got := seen.Get(name); got != want {
			t.Errorf("fetch %s = %q, want %q", name, got, want)
		}
	}
	if got := seen.Get("Upgrade-Insecure-Requests"); got != "" {
		t.Errorf("fetch has navigation header: %q", got)
	}
}

func TestBrowserHeadersOnJWOrgOnly(t *testing.T) {
	var seen http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
	}))
	defer srv.Close()

	c := New(WithBaseURLs(BaseURLs{JWOrg: srv.URL}))
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := seen.Get("Sec-Fetch-Mode"); got != "navigate" {
		t.Errorf("JWOrg navigation mode = %q", got)
	}
	if got := seen.Get("Accept-Language"); got != "en-US,en;q=0.9" {
		t.Errorf("JWOrg language = %q", got)
	}

	c = New(WithBaseURLs(BaseURLs{}))
	req, err = http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := seen.Get("Sec-Fetch-Mode"); got != "" {
		t.Errorf("other host received browser header: %q", got)
	}
}
