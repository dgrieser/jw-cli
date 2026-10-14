package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// pagedSearchMux answers the jw.org search of everything with rows rows and a
// total of total.
func pagedSearchMux(t *testing.T, rows, total int) *http.ServeMux {
	mux := searchMux(t)
	mux.HandleFunc("/apis/search/results/E/all", func(w http.ResponseWriter, r *http.Request) {
		var items []string
		for i := range rows {
			items = append(items, fmt.Sprintf(`{"type": "item", "subtype": "article", "title": "Row %d",
				"links": {"jw.org": "https://www.jw.org/en/row-%d/"}}`, i, i))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"results": [%s], "insight": {"total": {"value": %d}}}`, strings.Join(items, ","), total)
	})
	return mux
}

// The search's options cover what jw search takes: the Online Library's
// scope and publications, and the page size of jw.org.
func TestUISearchOptions(t *testing.T) {
	srv := newTestServer(t, pagedSearchMux(t, 10, 500))
	_, body := get(t, srv, "/search?lang=en")
	for _, want := range []string{
		`<select name="scope">`, `name="all" value="true"`, `name="include"`, `name="exclude"`,
		`<input type="number" name="limit" value="10" min="1" max="36"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("form lacks %s:\n%s", want, body)
		}
	}
	// the whole form sent as it is, blank fields and all, searches
	resp, body := get(t, srv, "/search?q=kingdom&lang=en&engine=jworg&type=all&sort=rel&excerpts=false&excerpts=true"+
		"&scope=par&all=false&include=&exclude=&limit=10")
	if resp.StatusCode != 200 || !strings.Contains(body, "Row 9") {
		t.Fatalf("status %d:\n%s", resp.StatusCode, body)
	}
	// and says which page of how many, as jw search does
	if !strings.Contains(body, `500 results for &#34;kingdom&#34; (page 1, 10 per page)</h2>`) {
		t.Errorf("header:\n%s", body)
	}
	if _, body = get(t, srv, "/search?q=kingdom&lang=en&engine=jworg&include=zz"); !strings.Contains(body, `unknown publication category`) {
		t.Errorf("an unknown category is not refused:\n%s", body)
	}
}

// The first page of everything comes grouped, longer than the page size: the
// header does not claim the page size then.
func TestUISearchGroupedFirstPage(t *testing.T) {
	srv := newTestServer(t, pagedSearchMux(t, 18, 500))
	_, body := get(t, srv, "/search?q=kingdom&lang=en")
	if !strings.Contains(body, `500 results for &#34;kingdom&#34; — page 1</h2>`) || strings.Contains(body, "per page)</h2>") {
		t.Errorf("header:\n%s", body)
	}
}
