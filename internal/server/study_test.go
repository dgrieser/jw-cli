package server_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/server"
	"github.com/dgrieser/jw-cli/internal/service"
)

// The week's reading is studied once: its chapters are read with what they
// cite, and a second night of the same week asks nothing.
func TestStudyReadsTheWeeksChaptersOnce(t *testing.T) {
	mux := wolMux(t)
	mux.HandleFunc("/en/wol/meetings/r1/lp-e/2026/43", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<a class="cardContainer pub-mwb" href="/en/wol/d/r1/lp-e/202026258">x</a>`)
	})
	mux.HandleFunc("/en/wol/d/r1/lp-e/202026258", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<article id="article"><header><h1>OCTOBER 19-25</h1>
			<h2><a href="/en/wol/bc/r1/lp-e/202026258/0/0" class="b"><strong>JOHN 3</strong></a></h2></header></article>`)
	})
	mux.HandleFunc("/wol/bc/r1/lp-e/202026258/0/0", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"items": [{"title": "John 3:1-36", "content": "<p>…</p>",
			"url": "/wol/b/r1/lp-e/nwtsty/43/3#v=43:3:1-43:3:36"}]}`)
	})
	var mu sync.Mutex
	var paths []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	defer up.Close()
	hc := httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{CDN: up.URL, JWOrg: up.URL, WOL: up.URL}),
		httpx.WithRetryDelays(time.Millisecond, time.Millisecond))
	srv := server.New(server.Config{Svc: service.New(hc, httpx.OpenCacheAt(t.TempDir())), StudyLangs: []string{"E"}})

	date := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	srv.StudyWeek(context.Background(), date)
	mu.Lock()
	read := strings.Join(paths, "\n")
	paths = nil
	mu.Unlock()
	if !strings.Contains(read, "/en/wol/b/r1/lp-e/nwtsty/43/3") {
		t.Errorf("the week's chapter was not read:\n%s", read)
	}

	srv.StudyWeek(context.Background(), date)
	mu.Lock()
	defer mu.Unlock()
	if len(paths) > 0 {
		t.Errorf("a week studied already was studied again:\n%s", strings.Join(paths, "\n"))
	}
}
