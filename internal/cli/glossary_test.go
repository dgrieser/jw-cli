package cli

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestUnfoldIncludesGlossaryWordsByDefault(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"bible", []string{"bible", "read", "John 3:16"}},
		{"article verse", []string{"article", "2024360"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := studyUnfoldMux(t)
			var entries, citations atomic.Int32
			for _, book := range []struct{ path, title, id string }{
				{"nwtstg", "Glossary", "1001077007"},
				{"rsg", "Research Guide", "1001077008"},
			} {
				mux.HandleFunc("/en/wol/publication/r1/lp-e/"+book.path, func(w http.ResponseWriter, r *http.Request) {
					fmt.Fprintf(w, `<div id="article"><h1>%s</h1><ul class="directory"><li class="row card"><a class="cardContainer" href="/en/wol/d/r1/lp-e/%s"><div class="cardLine1">World</div></a></li></ul></div>`, book.title, book.id)
				})
				mux.HandleFunc("/en/wol/d/r1/lp-e/"+book.id, func(w http.ResponseWriter, r *http.Request) {
					entries.Add(1)
					fmt.Fprintf(w, `<div id="article"><header><h1>World</h1></header><div class="bodyTxt"><p>Definition from %s. <a class="b" href="/en/wol/bc/r1/lp-e/glossary/0">Genesis 1:1</a></p></div></div>`, book.title)
				})
			}
			mux.HandleFunc("/wol/bc/r1/lp-e/glossary/0", func(w http.ResponseWriter, r *http.Request) {
				citations.Add(1)
				http.Error(w, "glossary citations should stay closed", http.StatusInternalServerError)
			})
			args := append(append([]string{}, tc.args...), "-l", "en", "-o", "raw")
			plain, err := runCmd(t, mux, args...)
			if err != nil {
				t.Fatal(err)
			}
			if entries.Load() != 0 || strings.Contains(plain, "Definition from") {
				t.Fatal("reading without unfold fetched glossary entries")
			}
			out, err := runCmd(t, mux, append(args, "--unfold", "1")...)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"Glossary – World", "Research Guide – World", "Definition from Glossary", "Definition from Research Guide"} {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q:\n%s", want, out)
				}
			}
			if strings.Index(out, "Glossary – World") > strings.Index(out, "Research Guide – World") {
				t.Error("glossary entries are out of order")
			}
			if citations.Load() != 0 || strings.Contains(out, "unfold-lazy") {
				t.Fatal("CLI must include entries without unfolding their citations or leaving lazy placeholders")
			}
		})
	}
}
