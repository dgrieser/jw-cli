package server_test

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

var siteLink = regexp.MustCompile(`class="ps-row ps-item ps-site" href="([^"]*)"`)

// siteOf is where a page's settings offer to open it on jw.org or
// wol.jw.org, empty for none.
func siteOf(body string) string {
	if m := siteLink.FindStringSubmatch(body); m != nil {
		return html.UnescapeString(m[1])
	}
	return ""
}

// Every page with a counterpart on jw.org or wol.jw.org offers to open it.
func TestSiteURLs(t *testing.T) {
	mux := bookDocsMux(t)
	mux.HandleFunc("/en/wol/binav/r1/lp-e/nwtsty", wolFixture(t, "binav_de.html"))
	srv := newTestServer(t, mux)
	for _, tc := range []struct{ path, want string }{
		// a reading: its chapter of the edition, at its first verse
		{"/bible?ref=John+3:16&lang=en", "/en/wol/b/r1/lp-e/nwtsty/43/3#study=discover&v=43:3:16"},
		{"/bible?lang=en", "/en/wol/binav/r1/lp-e/nwtsty"},
		{"/bible?book=40&lang=en", "/en/wol/binav/r1/lp-e/nwtsty/40"},
		{"/bible?book=40&view=intro&lang=en", "/de/wol/bibledocument/r10/lp-x/nwtsty/40/introduction"},
		{"/bible?book=40&view=outline&lang=en", "/nwtsty/40/overview"},
		{"/bible?book=40&view=gallery&lang=en", "/de/wol/gallery/r10/lp-x/nwtsty/40"},
	} {
		resp, body := get(t, srv, tc.path)
		if resp.StatusCode != 200 {
			t.Errorf("%s: status %d", tc.path, resp.StatusCode)
			continue
		}
		if got := siteOf(body); !strings.HasSuffix(got, tc.want) {
			t.Errorf("%s: site %q, want …%s", tc.path, got, tc.want)
		}
	}

	srv = newTestServer(t, searchMux(t))
	_, body := get(t, srv, "/search?q=Sch%C3%B6pfung&type=videos&lang=de")
	if got := siteOf(body); got != "https://www.jw.org/finder?q=Sch%C3%B6pfung&wtlocale=X" {
		t.Errorf("jw.org search: site %q", got)
	}

	mux, _ = citedMux(t)
	srv = newTestServer(t, mux)
	_, body = get(t, srv, "/cited?ref=Jeremiah+31:15&lang=en&videos=0&excerpts=0")
	if got := siteOf(body); !strings.Contains(got, "/en/wol/s/r1/lp-e?") || !strings.Contains(got, "q=%28Jeremiah+31%3A15%29") ||
		!strings.Contains(got, "r=newest") {
		t.Errorf("quotations: site %q", got)
	}

	srv = newTestServer(t, mediaMux(t))
	_, body = get(t, srv, "/media?lang=en")
	if got := siteOf(body); got != "https://www.jw.org/finder?docid=1011214&prefer=content&wtlocale=E" {
		t.Errorf("media: site %q", got)
	}
	_, body = get(t, srv, "/media/category/VODChildren?lang=en")
	if got := siteOf(body); got != "https://www.jw.org/finder?docid=1011214&prefer=content&wtlocale=E#en/categories/VODChildren" {
		t.Errorf("media category: site %q", got)
	}
}

// Every level of the publications is a page of wol.
func TestSiteURLPublications(t *testing.T) {
	mux := wolMux(t)
	mux.HandleFunc("/en/wol/library/r1/lp-e", wolFixture(t, "library_en.html"))
	srv := newTestServer(t, mux)
	resp, body := get(t, srv, "/pub?lang=en")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if got := siteOf(body); !strings.HasSuffix(got, "/en/wol/library/r1/lp-e") {
		t.Errorf("start page: site %q", got)
	}
}
