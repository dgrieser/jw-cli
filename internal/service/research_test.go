package service

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

// jw bible research -x reads the passages of a verse's index several at a
// time, and one that cannot be read says why on its entry rather than going
// missing as if the entry had none.
func TestResearchExcerptsSayWhatFailed(t *testing.T) {
	s := unfoldUpstream(t, func(mux *http.ServeMux) {
		mux.HandleFunc("/en/wol/b/r1/lp-e/nwtsty/43/3", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(indexChapter(3)))
		})
		mux.HandleFunc("/wol/pc/r1/lp-e/9000/{i}/0", func(w http.ResponseWriter, r *http.Request) {
			if r.PathValue("i") == "2" {
				http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"items":[{"title":"Passage %[1]s","content":"<p>Passage %[1]s says why.</p>"}]}`, r.PathValue("i"))
		})
	})
	entries, err := s.Research(context.Background(), english, "John 3:16", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || len(entries[0].Items) != 3 {
		t.Fatalf("entries = %+v", entries)
	}
	for i, it := range entries[0].Items {
		failed := i == 1
		if (it.ExcerptHTML == "") != failed || (it.ExcerptErr != "") != failed {
			t.Errorf("item %d: excerpt %q, error %q", i+1, it.ExcerptHTML, it.ExcerptErr)
		}
	}
	if got := entries[0].Items[1].ExcerptErr; got != "HTTP 503" {
		t.Errorf("the error = %q, want the status without the address", got)
	}
}
