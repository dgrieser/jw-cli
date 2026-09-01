package cli

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// sentCategories reads the fc[] values off a recorded query string. Matching
// them as substrings would not do: "bi" is a prefix of "bibleteachings".
func sentCategories(t *testing.T, rawQuery string) []string {
	t.Helper()
	v, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatalf("query %q: %v", rawQuery, err)
	}
	return v["fc[]"]
}

// citedMux serves the recorded citation-search page for every wol search,
// recording the query string of each request.
func citedMux(t *testing.T, queries *[]string) *http.ServeMux {
	mux := languagesMux(t)
	mux.HandleFunc("/en", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<a href="/en/wol/h/r1/lp-e">home</a>`))
	})
	mux.HandleFunc("/en/wol/s/r1/lp-e", func(w http.ResponseWriter, r *http.Request) {
		*queries = append(*queries, r.URL.RawQuery)
		b, err := os.ReadFile(filepath.Join("..", "api", "wol", "testdata", "search_citation.html"))
		if err != nil {
			t.Errorf("fixture: %v", err)
			http.Error(w, "missing", 500)
			return
		}
		w.Write(b)
	})
	return mux
}

func TestBibleCited(t *testing.T) {
	var queries []string
	out, err := runCmd(t, citedMux(t, &queries), "bible", "cited", "Jeremiah 31:15", "-l", "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 1 {
		t.Fatalf("requests = %v", queries)
	}
	q := queries[0]
	for _, want := range []string{"q=%28Jeremiah+31%3A15%29", "r=newest", "p=par"} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing %s: %s", want, q)
		}
	}
	cats := sentCategories(t, q)
	if !slices.Contains(cats, "w") || !slices.Contains(cats, "it") {
		t.Errorf("categories = %v", cats)
	}
	// the two categories that cite every verse by construction stay out
	for _, unwanted := range []string{"bi", "dx"} {
		if slices.Contains(cats, unwanted) {
			t.Errorf("query still covers %q: %v", unwanted, cats)
		}
	}
	for _, want := range []string{
		"66 publications citing Jeremiah 31:15",
		"31. August–6. September",
		"mwb26 Juli S. 14-15 - Leben und Dienst: Arbeitsheft (2026)",
		"Wie hat sich diese Prophezeiung",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// Several references are one OR query, so the listing keeps one total and one
// page sequence.
func TestBibleCitedMultipleRefs(t *testing.T) {
	var queries []string
	_, err := runCmd(t, citedMux(t, &queries), "bible", "cited", "Jeremiah 31:15; Matthew 2:18", "-l", "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 1 || !strings.Contains(queries[0], "q=%28Jeremiah+31%3A15%29+%7C+%28Matthew+2%3A18%29") {
		t.Errorf("queries = %v", queries)
	}
}

func TestBibleCitedCategoryFlags(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		want, deny []string
	}{
		{"all", []string{"--all"}, nil, []string{"w", "it", "bi"}},
		{"include", []string{"--include", "w,it"}, []string{"w", "it"}, []string{"g", "bi"}},
		{"exclude", []string{"--exclude", "w"}, []string{"it", "bi", "dx"}, []string{"w"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var queries []string
			args := append([]string{"bible", "cited", "Jeremiah 31:15", "-l", "en"}, tc.args...)
			if _, err := runCmd(t, citedMux(t, &queries), args...); err != nil {
				t.Fatal(err)
			}
			if len(queries) == 0 {
				t.Fatal("no request")
			}
			cats := sentCategories(t, queries[0])
			for _, want := range tc.want {
				if !slices.Contains(cats, want) {
					t.Errorf("categories %v missing %q", cats, want)
				}
			}
			for _, deny := range tc.deny {
				if slices.Contains(cats, deny) {
					t.Errorf("categories %v should not contain %q", cats, deny)
				}
			}
		})
	}
}

func TestBibleCitedConflictingFlags(t *testing.T) {
	var queries []string
	_, err := runCmd(t, citedMux(t, &queries),
		"bible", "cited", "Jeremiah 31:15", "-l", "en", "--all", "--include", "w")
	if err == nil {
		t.Fatal("want an error for --all together with --include")
	}
}

func TestBibleCitedUnknownCategory(t *testing.T) {
	var queries []string
	_, err := runCmd(t, citedMux(t, &queries),
		"bible", "cited", "Jeremiah 31:15", "-l", "en", "--include", "nosuchpub")
	if err == nil || !strings.Contains(err.Error(), "unknown publication category") {
		t.Fatalf("err = %v", err)
	}
}

// The category list is language-dependent. When the page names one the sent
// whitelist did not know, the search runs again with it — otherwise that
// category's documents would be silently missing.
func TestBibleCitedUnknownCategoryRetried(t *testing.T) {
	var queries []string
	mux := languagesMux(t)
	mux.HandleFunc("/en", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<a href="/en/wol/h/r1/lp-e">home</a>`))
	})
	mux.HandleFunc("/en/wol/s/r1/lp-e", func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		w.Write([]byte(`<html><body>
		<div id="searchFilterContainer">
		  <a id="fc-bi" href="#">Bibles</a>
		  <a id="fc-w" href="#">Watchtower</a>
		  <a id="fc-zz" href="#">A category only this language has</a>
		</div>
		<main><div class="resultsContainer"><ul class="results resultContentDocument">
		  <li class="caption"><a class="lnk" href="/en/wol/d/r1/lp-e/2024360">Preach</a></li>
		  <li class="result"><ul class="resultItems">
		    <li class="searchResult"><article><div class="document"><p>Jer 31:15 …</p></div></article></li>
		    <li class="ref">w24 September - The Watchtower</li>
		  </ul></li>
		</ul></div></main>
		<input type="hidden" id="searchResultsTotal" value="1"/>
		</body></html>`))
	})
	if _, err := runCmd(t, mux, "bible", "cited", "Jeremiah 31:15", "-l", "en"); err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 {
		t.Fatalf("want a retry with the corrected list, got %d requests: %v", len(queries), queries)
	}
	retry := sentCategories(t, queries[1])
	if !slices.Equal(retry, []string{"w", "zz"}) {
		t.Errorf("retry categories = %v", retry)
	}
	if slices.Contains(sentCategories(t, queries[0]), "zz") {
		t.Errorf("the first query could not have known zz: %s", queries[0])
	}
}

// A citation search reads every page, so the listing is the complete answer.
func TestBibleCitedReadsEveryPage(t *testing.T) {
	var queries []string
	mux := languagesMux(t)
	mux.HandleFunc("/en", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<a href="/en/wol/h/r1/lp-e">home</a>`))
	})
	mux.HandleFunc("/en/wol/s/r1/lp-e", func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		// a full page, then the short one that ends the walk
		n := 40
		if r.URL.Query().Get("pg") == "2" {
			n = 2
		}
		var b strings.Builder
		b.WriteString(`<html><body><main><div class="resultsContainer">`)
		for i := range n {
			id := 1000 + i
			if r.URL.Query().Get("pg") == "2" {
				id = 2000 + i
			}
			fmt.Fprintf(&b, `<ul class="results resultContentDocument">
			  <li class="caption"><a class="lnk" href="/en/wol/d/r1/lp-e/%d">Doc %d</a></li>
			  <li class="result"><ul class="resultItems">
			    <li class="searchResult"><article><div class="document"><p>Jer 31:15</p></div></article></li>
			    <li class="ref">w24 - The Watchtower</li>
			  </ul></li>
			</ul>`, id, id)
		}
		b.WriteString(`</div></main>
		<input type="hidden" id="searchResultsPageSize" value="40"/>
		<input type="hidden" id="searchResultsTotal" value="42"/>
		</body></html>`)
		w.Write([]byte(b.String()))
	})

	out, err := runCmd(t, mux, "bible", "cited", "Jeremiah 31:15", "-l", "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 2 {
		t.Fatalf("want one request per page, got %d: %v", len(queries), queries)
	}
	if strings.Contains(queries[0], "pg=") || !strings.Contains(queries[1], "pg=2") {
		t.Errorf("queries = %v", queries)
	}
	if !strings.Contains(out, "42 publications citing Jeremiah 31:15") {
		t.Errorf("header missing the total:\n%s", out)
	}
	// both pages are numbered into one listing
	for _, want := range []string{" 1. [article] Doc 1000", " 41. [article] Doc 2000", " 42. [article] Doc 2001"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Doc 2002") {
		t.Errorf("walked past the short page:\n%s", out)
	}
}
