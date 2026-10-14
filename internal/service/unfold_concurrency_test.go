package service

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/bibleref"
	"github.com/dgrieser/jw-cli/internal/i18n"
)

// slowUpstream is wol answering every citation after delay, the way it takes a
// second or more per passage: John 3:16 with a research guide of n passages,
// each citing a verse of its own. It counts the citations asked at once.
type slowUpstream struct {
	delay time.Duration
	mu    sync.Mutex
	now   int
	peak  int
	asked int
}

func (u *slowUpstream) slow(w http.ResponseWriter, body string) {
	u.mu.Lock()
	u.now++
	u.asked++
	u.peak = max(u.peak, u.now)
	u.mu.Unlock()
	time.Sleep(u.delay)
	u.mu.Lock()
	u.now--
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, body)
}

// indexChapter is John 3 of the study bible with n passages in the research
// guide of verse 16.
func indexChapter(n int) string {
	var b strings.Builder
	b.WriteString(`<html><body><div id="article"><div class="bodyTxt"><p>` +
		`<span id="v43-3-16-1" class="v"><a class="vl vx vp study" href="#">16 </a>For God loved the world.</span>` +
		`</p></div></div><div id="studyDiscover" class="studyPane active tabContent">` +
		`<div class="section" data-key="43-3-16" data-section-citation="043003016043003016">` +
		`<h3 class="title"><span class="scalableui">John 3:16</span></h3>` +
		`<div class="group index"><h4 class="title">Research Guide</h4><ul class="content">`)
	for i := range n {
		fmt.Fprintf(&b, `<li class="item ref-rsg"><div class="subtitle">Research Guide</div>`+
			`<a href="/en/wol/pc/r1/lp-e/9000/%d/0">w%d %d</a></li>`, i+1, i+1, i+1)
	}
	b.WriteString(`</ul></div></div></div></body></html>`)
	return b.String()
}

func newSlowUpstream(t *testing.T, n int, delay time.Duration) (*Service, *slowUpstream) {
	u := &slowUpstream{delay: delay}
	s := unfoldUpstream(t, func(mux *http.ServeMux) {
		mux.HandleFunc("/en/wol/b/r1/lp-e/nwtsty/43/3", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(indexChapter(n)))
		})
		mux.HandleFunc("/wol/pc/r1/lp-e/9000/{i}/0", func(w http.ResponseWriter, r *http.Request) {
			i := r.PathValue("i")
			u.slow(w, fmt.Sprintf(`{"items":[{"title":"Passage %[1]s",`+
				`"content":"<p>Passage %[1]s says why (<a href=\"/en/wol/bc/r1/lp-e/9000/%[1]s/1\">Ps %[1]s:1</a>).</p>",`+
				`"url":"/wol/d/r1/lp-e/9%[1]s#h=1:0-1:0"}]}`, i))
		})
		mux.HandleFunc("/wol/bc/r1/lp-e/9000/{i}/1", func(w http.ResponseWriter, r *http.Request) {
			i := r.PathValue("i")
			u.slow(w, fmt.Sprintf(`{"items":[{"title":"Psalm %[1]s:1",`+
				`"content":"<p><span id=\"v19-%[1]s-1-1\" class=\"v\">Happy is the man %[1]s</span></p>"}]}`, i))
		})
	})
	return s, u
}

// The passages of a verse's index are read several at a time, as each answer
// takes a while, and they still come out in the order the index lists them:
// that is the difference between minutes and seconds for John 3:16, whose
// research guide lists 312 of them.
func TestIndexesResolveConcurrently(t *testing.T) {
	const n = 48
	s, u := newSlowUpstream(t, n, 40*time.Millisecond)
	var progress []string
	cfg := UnfoldConfig{Depth: 1, Progress: func(level, done, total int) {
		progress = append(progress, fmt.Sprintf("%d:%d/%d", level, done, total))
	}}
	start := time.Now()
	var bodies []string
	_, requests, err := s.StreamPassageParts(context.Background(), english, "nwtsty",
		bibleref.Ref{Book: 43, Chapter: 3, VerseStart: 16, VerseEnd: 16},
		PassageParts{Only: PartIndexes, Kind: KindGuide}, cfg, i18n.EN.Text(),
		UnfoldStream{Section: func(sec UnfoldSection) { bodies = append(bodies, sec.Body) }})
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	t.Logf("%d citations answered after %v each took %v, at most %d at once", u.asked, u.delay, took, u.peak)
	if u.asked != 2*n {
		t.Errorf("asked %d citations, want %d", u.asked, 2*n)
	}
	if u.peak < 2 || u.peak > unfoldWorkers {
		t.Errorf("at most %d citations at once, want between 2 and %d", u.peak, unfoldWorkers)
	}
	// the order of the index, and every verse the passages cite under its passage
	body := strings.Join(bodies, "")
	last := -1
	for i := range n {
		at := strings.Index(body, fmt.Sprintf("Passage %d says why", i+1))
		verse := strings.Index(body, fmt.Sprintf("Happy is the man %d<", i+1))
		if at < last || verse < at {
			t.Fatalf("passage %d out of order (at %d, verse at %d, previous at %d)", i+1, at, verse, last)
		}
		last = at
	}
	// one chapter page, the passages and the verses they cite
	if requests != 1+2*n {
		t.Errorf("requests = %d, want %d", requests, 1+2*n)
	}
	// every count of a level once, in order, up to its total
	for _, level := range []int{1, 2} {
		var want []string
		for i := range n {
			want = append(want, fmt.Sprintf("%d:%d/%d", level, i+1, n))
		}
		got := slices.DeleteFunc(slices.Clone(progress), func(p string) bool { return !strings.HasPrefix(p, fmt.Sprint(level, ":")) })
		if !slices.Equal(got, want) {
			t.Errorf("progress of level %d = %v", level, got)
		}
	}
}

// jw bible read --unfold, counted in levels, reads the passages of a verse's
// index several at a time as well, and prints them in the index's order.
func TestReadUnfoldResolvesConcurrently(t *testing.T) {
	const n = 24
	s, u := newSlowUpstream(t, n, 40*time.Millisecond)
	start := time.Now()
	res, err := s.ReadPassages(context.Background(), english, ReadRequest{
		Refs: "John 3:16", Edition: "nwtsty", NoOutlines: true, Unfold: UnfoldConfig{Depth: 1},
	}, i18n.EN.Text())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d citations answered after %v each took %v, at most %d at once", u.asked, u.delay, time.Since(start), u.peak)
	if u.asked != n || u.peak < 2 || u.peak > unfoldWorkers {
		t.Errorf("asked %d citations, at most %d at once", u.asked, u.peak)
	}
	out := res.Passages[0].Verses[0].Unfold
	last := -1
	for i := range n {
		at := strings.Index(out, fmt.Sprintf("Passage %d says why", i+1))
		if at < last {
			t.Fatalf("passage %d out of order:\n%s", i+1, out)
		}
		last = at
	}
}
