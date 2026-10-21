package server_test

import (
	"strings"
	"testing"
)

// Without scripts nothing loads a section left to load, so every one of them
// leads to a page of the server showing what it would load.
func TestUILazySectionsFallBack(t *testing.T) {
	srv := newTestServer(t, hopsMux(t))
	for _, path := range []string{"/bible?ref=John+3:16&lang=en&unfold=1", "/article?target=2024360&lang=en&unfold=1"} {
		resp, body := get(t, srv, path)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: status %d", path, resp.StatusCode)
		}
		lazy, fallbacks := strings.Count(body, "data-lazy="), strings.Count(body, `<p class="lazy-fallback">`)
		if lazy == 0 || lazy != fallbacks {
			t.Errorf("%s: %d lazy sections, %d fallbacks:\n%s", path, lazy, fallbacks, body)
		}
	}
	_, body := get(t, srv, "/bible?ref=John+3:16&lang=en&unfold=1")
	for _, want := range []string{
		`data-lazy="/unfold/cited?to=0&amp;vid=43003016"><summary>Quotations<span class="sec-of"> of John 3:16</span></summary><div class="section-body"><p class="lazy-fallback"><a href="/cited?lang=en&amp;to=0&amp;vid=43003016">Show on a page of its own</a></p>`,
		`<a href="/bible?bible=nwtsty&amp;lang=en&amp;to=0&amp;unfold=1&amp;vid=43001029">`,
		`<a href="/bible?all=1&amp;lang=en&amp;to=0&amp;vid=43001029">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in:\n%s", want, body)
		}
	}
}
