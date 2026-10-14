package server_test

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
)

// citedMux serves the recorded citation search for every wol search,
// recording the query of each.
func citedMux(t *testing.T) (*http.ServeMux, func() []string) {
	mux := languagesMux(t)
	mux.HandleFunc("/en", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<a href="/en/wol/h/r1/lp-e">home</a>`))
	})
	var mu sync.Mutex
	var queries []string
	mux.HandleFunc("/en/wol/s/r1/lp-e", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.RawQuery)
		mu.Unlock()
		wolFixture(t, "search_citation.html")(w, r)
	})
	return mux, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(queries)
	}
}

// The web has jw bible cited too, with its options; a section of an unfolded
// page names its passage by verse id.
func TestUICited(t *testing.T) {
	mux, queries := citedMux(t)
	srv := newTestServer(t, mux)

	resp, body := get(t, srv, "/cited?lang=en")
	if resp.StatusCode != 200 || !strings.Contains(body, `name="ref"`) || !strings.Contains(body, `name="exclude"`) {
		t.Fatalf("empty form: status %d:\n%s", resp.StatusCode, body)
	}
	if len(queries()) != 0 {
		t.Errorf("the empty form searched: %v", queries())
	}

	// a form sends its blank fields, which ask for nothing
	for _, path := range []string{
		"/cited?ref=Jeremiah+31:15&lang=en&videos=0&excerpts=0",
		"/cited?vid=24031015&to=0&lang=en&videos=false&videos=false&excerpts=false&all=false&include=&exclude=",
	} {
		resp, body = get(t, srv, path)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: status %d:\n%s", path, resp.StatusCode, body)
		}
		for _, want := range []string{
			"66 publications citing Jeremiah 31:15", "31. August–6. September", `value="Jeremiah 31:15"`,
			// read among the bible's pages
			`<a class="active" href="/bible?lang=en">`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %s in:\n%s", path, want, body)
			}
		}
	}
	for _, q := range queries() {
		v, err := url.ParseQuery(q)
		if err != nil {
			t.Fatal(err)
		}
		if cats := v["fc[]"]; !slices.Contains(cats, "w") || slices.Contains(cats, "bi") || slices.Contains(cats, "dx") {
			t.Errorf("bibles and indexes should stay out by default: %v", cats)
		}
		if v.Get("r") != "newest" {
			t.Errorf("newest first by default: %s", q)
		}
	}

	resp, body = get(t, srv, "/cited?ref=Jeremiah+31:15&lang=en&videos=0&excerpts=0&include=zz")
	if resp.StatusCode != 400 || !strings.Contains(body, `unknown publication category &#34;zz&#34;`) {
		t.Errorf("unknown category: status %d:\n%s", resp.StatusCode, body)
	}
}
