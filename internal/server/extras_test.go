package server_test

import (
	"net/http"
	"strings"
	"testing"
)

// dailyTextMux serves the daily text of 2026-07-15, citing a verse and
// showing a picture.
func dailyTextMux(t *testing.T) *http.ServeMux {
	mux := wolMux(t)
	mux.HandleFunc("/en/wol/dt/r1/lp-e/2026/7/15", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><div class="todayItem pub-es">
		  <p class="themeScrp">Be courageous. (<a href="/en/wol/bc/r1/lp-e/1102026206/0/0" class="b">Josh. 1:9</a>)</p>
		  <p>Joshua was strong.</p>
		  <figure><img src="/img/joshua.jpg" alt="Joshua at the Jordan" width="1200" height="600"><figcaption>Joshua leads</figcaption></figure>
		</div></body></html>`))
	})
	return mux
}

// The daily text lists the pictures it shows, as an article's page does, each
// with its alt text and size; the verses it cites are links in its text and
// not listed again.
func TestUIDailyTextExtras(t *testing.T) {
	srv := newTestServer(t, dailyTextMux(t))
	resp, body := get(t, srv, "/dailytext?date=2026-07-15&lang=en")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d:\n%s", resp.StatusCode, body)
	}
	if strings.Contains(body, `<li>Josh. 1:9</li>`) {
		t.Errorf("the cited verses are listed below the text:\n%s", body)
	}
	for _, want := range []string{
		`<summary>Images <span class="count">1</span></summary>`,
		`<p class="imagemeta">Alt text: Joshua at the Jordan</p>`,
		`<p class="imagemeta">Size: 1200×600 px</p>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in:\n%s", want, body)
		}
	}
}
