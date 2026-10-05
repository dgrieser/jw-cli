package server_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// hopsMux is studyMux with the verse John 3:16's study note cites.
func hopsMux(t *testing.T) *http.ServeMux {
	mux := studyMux(t)
	mux.HandleFunc("/wol/bc/r1/lp-e/1001070671/25/0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items": [{"title": "John 1:29", "url": "/en/wol/b/r1/lp-e/nwtsty/43/1",
			"content": "<p><span id=\"v43-1-29-1\" class=\"v\">Look, the Lamb of God</span></p>"}]}`))
	})
	return mux
}

type hopEvent struct {
	Type, HTML, Text string
	Unwrap           bool
}

func hopEvents(t *testing.T, body string) []hopEvent {
	t.Helper()
	var out []hopEvent
	for line := range strings.SplitSeq(strings.TrimSpace(body), "\n") {
		var ev hopEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("bad line %q: %v", line, err)
		}
		out = append(out, ev)
	}
	return out
}

// sectionsHTML is every section a stream sent, and the event closing it.
func sectionsHTML(t *testing.T, body string) (string, hopEvent) {
	t.Helper()
	var b strings.Builder
	evs := hopEvents(t, body)
	for _, ev := range evs {
		if ev.Type == "section" {
			b.WriteString(ev.HTML)
		}
	}
	return b.String(), evs[len(evs)-1]
}

// At depth 1 a verse brings its study notes with the verses they cite, and each
// of those is its text with every section a heading that loads once opened.
func TestUnfoldVerseHopsOneDeep(t *testing.T) {
	srv := newTestServer(t, hopsMux(t))
	resp, body := get(t, srv, "/unfold/verse?vid=43003016&depth=1&lang=en")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	html, done := sectionsHTML(t, body)
	if !strings.Contains(html, "Look, the Lamb of God") {
		t.Fatalf("the study notes should bring the verse they cite:\n%s", html)
	}
	for _, want := range []string{
		`data-lazy="/unfold/verse?bible=nwtsty&amp;depth=1&amp;part=notes&amp;to=0&amp;vid=43001029"`,
		`data-lazy="/unfold/verse?bible=nwtsty&amp;depth=1&amp;kind=guide&amp;part=indexes&amp;to=0&amp;vid=43001029"`,
		`data-lazy="/unfold/verse?bible=nwtsty&amp;depth=1&amp;kind=pubindex&amp;part=indexes&amp;to=0&amp;vid=43001029"`,
		`data-lazy="/unfold/verse?bible=nwtsty&amp;depth=1&amp;part=marginal&amp;to=0&amp;vid=43001029"`,
		`data-lazy="/unfold/translations?bible=nwtsty&amp;to=0&amp;vid=43001029"`,
		`data-lazy="/unfold/cited?to=0&amp;vid=43001029"`,
		"<summary>Research Guide</summary>", "<summary>Publications Index</summary>",
		"<summary>Marginal references of John 1:29</summary>", "<summary>Quotations of John 1:29</summary>",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %s in:\n%s", want, html)
		}
	}
	if done.Type != "done" {
		t.Errorf("stream should close with done: %+v", done)
	}
}

// The streams of one unfold run (run=) show a verse once between them.
func TestUnfoldVerseHopsRun(t *testing.T) {
	srv := newTestServer(t, hopsMux(t))
	_, body := get(t, srv, "/unfold/verse?vid=43003016&depth=1&lang=en&run=run-12345678")
	if html, _ := sectionsHTML(t, body); !strings.Contains(html, "Lamb of God") {
		t.Fatalf("first stream of the run:\n%s", body)
	}
	// a paragraph citing the verse the run showed already brings nothing
	_, body = get(t, srv, "/unfold/refs?lang=en&depth=1&run=run-12345678&path=%2Fwol%2Fbc%2Fr1%2Flp-e%2F1001070671%2F25%2F0&text=Joh+1%3A29")
	if html, _ := sectionsHTML(t, body); strings.Contains(html, "Lamb of God") {
		t.Errorf("a verse the run showed should not come again:\n%s", html)
	}
	// another run, or none, starts afresh
	_, body = get(t, srv, "/unfold/verse?vid=43003016&depth=1&lang=en&run=other-run-1")
	if html, _ := sectionsHTML(t, body); !strings.Contains(html, "Lamb of God") {
		t.Errorf("another run:\n%s", html)
	}
}

// What a note goes on with after the verse it cites stays in the note, not
// in the sections of that verse.
func TestUnfoldVerseHopsNoteAfterVerse(t *testing.T) {
	srv := newTestServer(t, hopsMux(t))
	_, body := get(t, srv, "/unfold/verse?vid=43003016&depth=1&lang=en")
	html, _ := sectionsHTML(t, body)
	cited := strings.Index(html, `<summary>Quotations of John 1:29</summary><div class="section-body"></div>`)
	next := strings.Index(html, "everlasting life:")
	if cited < 0 || next < cited {
		t.Errorf("the next note should follow the verse's sections, outside them:\n%s", html)
	}
}

// One index loads on its own, without what the other one lists.
func TestUnfoldVerseHopsIndexKind(t *testing.T) {
	srv := newTestServer(t, hopsMux(t))
	_, body := get(t, srv, "/unfold/verse?vid=43003016&depth=1&lang=en&part=indexes&kind=guide")
	html, _ := sectionsHTML(t, body)
	if !strings.Contains(html, "God So Loved the World") || strings.Contains(html, "it-2 274") {
		t.Errorf("research guide alone:\n%s", html)
	}
	for _, ev := range hopEvents(t, body) {
		if ev.Type == "section" && !ev.Unwrap {
			t.Errorf("a part loaded alone goes into the section on the page: %+v", ev)
		}
	}
	_, body = get(t, srv, "/unfold/verse?vid=43003016&depth=1&lang=en&part=indexes&kind=pubindex")
	html, _ = sectionsHTML(t, body)
	if !strings.Contains(html, "it-2 274") || strings.Contains(html, "God So Loved the World") {
		t.Errorf("publications index alone:\n%s", html)
	}
	resp, _ := get(t, srv, "/unfold/verse?vid=43003016&depth=1&lang=en&part=indexes&kind=other")
	if resp.StatusCode != 400 {
		t.Errorf("unknown kind: status %d", resp.StatusCode)
	}
}

// Two deep, the verses the notes cite unfold in turn.
func TestUnfoldVerseHopsTwoDeep(t *testing.T) {
	srv := newTestServer(t, hopsMux(t))
	_, body := get(t, srv, "/unfold/verse?vid=43003016&depth=2&lang=en")
	html, _ := sectionsHTML(t, body)
	if !strings.Contains(html, "Look, the Lamb of God") ||
		strings.Contains(html, "part=notes&amp;to=0&amp;vid=43001029") {
		t.Errorf("John 1:29 should unfold rather than wait to be opened:\n%s", html)
	}
}

// A document's verse citation is the verse unfolded; the API counts the same
// way the page does.
func TestArticleAPIHops(t *testing.T) {
	srv := newTestServer(t, hopsMux(t))
	resp, body := get(t, srv, "/api/v1/article?target=2024360&lang=en&unfold=1")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var art struct{ Body string }
	if err := json.Unmarshal([]byte(body), &art); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"For God loved the world", "Study notes", "Look, the Lamb of God",
		"part=notes&amp;to=0&amp;vid=43001029"} {
		if !strings.Contains(art.Body, want) {
			t.Errorf("missing %q in:\n%s", want, art.Body)
		}
	}
}

// A reading unfolds every verse it reads the way the page does.
func TestBibleReadAPIHops(t *testing.T) {
	srv := newTestServer(t, hopsMux(t))
	resp, body := get(t, srv, "/api/v1/bible/read?ref=John+3:16&lang=en&unfold=1")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var read struct{ Body string }
	if err := json.Unmarshal([]byte(body), &read); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Study notes", "Look, the Lamb of God", "part=notes&amp;to=0&amp;vid=43001029",
		"Research Guide", "/unfold/cited?to=0&amp;vid=43003016"} {
		if !strings.Contains(read.Body, want) {
			t.Errorf("missing %q in:\n%s", want, read.Body)
		}
	}
}

// countingMux counts what upstream is asked, by path.
func countingMux(t *testing.T, mux *http.ServeMux) (http.Handler, func(string) int) {
	var mu sync.Mutex
	hits := map[string]int{}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
		mux.ServeHTTP(w, r)
	})
	return h, func(path string) int {
		mu.Lock()
		defer mu.Unlock()
		return hits[path]
	}
}

// A verse read once is not read again, through whichever link it is reached:
// another document's citation of it is told by its text, and a verse of a
// chapter of the study bible read before comes out of that chapter.
func TestUnfoldHopsReadsAVerseOnce(t *testing.T) {
	mux := hopsMux(t)
	mux.HandleFunc("/wol/bc/r1/lp-e/9999/1/0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items": [{"title": "John 1:29", "content": "<p><span id=\"v43-1-29-1\" class=\"v\">Look, the Lamb of God</span></p>"}]}`))
	})
	mux.HandleFunc("/wol/bc/r1/lp-e/9999/2/0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items": [{"title": "John 3:16", "content": "<p><span id=\"v43-3-16-1\" class=\"v\">For God</span></p>"}]}`))
	})
	h, hits := countingMux(t, mux)
	up := http.NewServeMux()
	up.Handle("/", h)
	srv := newTestServer(t, up)
	// John 3:16's notes read John 1:29, and the study bible's John 3
	get(t, srv, "/unfold/verse?vid=43003016&depth=1&lang=en")
	if hits("/wol/bc/r1/lp-e/1001070671/25/0") != 1 {
		t.Fatalf("John 1:29 read %d times", hits("/wol/bc/r1/lp-e/1001070671/25/0"))
	}
	// another document citing both, through links of its own
	_, body := get(t, srv, "/unfold/refs?lang=en&depth=1&path=%2Fwol%2Fbc%2Fr1%2Flp-e%2F9999%2F1%2F0&text=John+1%3A29"+
		"&path=%2Fwol%2Fbc%2Fr1%2Flp-e%2F9999%2F2%2F0&text=John+3%3A16")
	if n := hits("/wol/bc/r1/lp-e/9999/1/0") + hits("/wol/bc/r1/lp-e/9999/2/0"); n != 0 {
		t.Errorf("verses read before were asked for again %d times", n)
	}
	if html, _ := sectionsHTML(t, body); !strings.Contains(html, "Look, the Lamb of God") {
		t.Errorf("John 1:29 should come from what was read:\n%s", body)
	}
}
