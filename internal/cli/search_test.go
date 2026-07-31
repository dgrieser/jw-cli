package cli

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func searchMux(t *testing.T) *http.ServeMux {
	mux := languagesMux(t)
	mux.HandleFunc("/tokens/jworg.jwt", func(w http.ResponseWriter, r *http.Request) {
		payload := base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, `{"exp":%d}`, time.Now().Add(time.Hour).Unix()))
		fmt.Fprint(w, "h."+payload+".s")
	})
	mux.HandleFunc("/apis/search/results/X/videos", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// the live API marks up matched words and spells references with &nbsp;
		fmt.Fprint(w, `{
			"results": [{"type": "item", "subtype": "video", "title": "Daniel&nbsp;7:27 Schöpfung", "lank": "pub-xyz_1_VIDEO",
			             "snippet": "vom <strong>Königreich</strong>\nbekannt machen?",
			             "duration": "3:10", "links": {"jw.org": "https://www.jw.org/finder?lank=pub-xyz_1_VIDEO"}}],
			"insight": {"total": {"value": 1}}
		}`)
	})
	return mux
}

func TestSearchCommand(t *testing.T) {
	out, err := runCmd(t, searchMux(t), "search", "-l", "de", "-t", "videos", "Schöpfung")
	if err != nil {
		t.Fatal(err)
	}
	// tags and entities are rendered away, and the snippet's newline is
	// collapsed so it cannot break the indented listing
	for _, want := range []string{
		"1 results",
		"[video] Daniel 7:27 Schöpfung (3:10)",
		"     vom Königreich bekannt machen?\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"<strong>", "&nbsp;", "\x1b["} {
		if strings.Contains(out, unwanted) {
			t.Errorf("listing still contains %q:\n%s", unwanted, out)
		}
	}
}

// TestSearchJSONKeepsSourceMarkup pins that -o json stays the API's data: the
// highlight markup is only rendered away for human-readable listings.
func TestSearchJSONKeepsSourceMarkup(t *testing.T) {
	out, err := runCmd(t, searchMux(t), "search", "-l", "de", "-t", "videos", "-o", "json", "Schöpfung")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `\u003cstrong\u003e`) {
		t.Errorf("json should keep the source markup:\n%s", out)
	}
}

func TestSearchThenOpen(t *testing.T) {
	mux := searchMux(t)
	cacheDir := t.TempDir()
	if _, err := runCmdWithCache(t, mux, cacheDir, "search", "-l", "de", "-t", "videos", "Schöpfung"); err != nil {
		t.Fatal(err)
	}
	out, err := runCmdWithCache(t, mux, cacheDir, "open", "1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "finder?lank=pub-xyz_1_VIDEO") {
		t.Errorf("open output: %s", out)
	}
}

func TestSearchJSON(t *testing.T) {
	out, err := runCmd(t, searchMux(t), "search", "-l", "de", "-t", "videos", "-o", "json", "Schöpfung")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"lank": "pub-xyz_1_VIDEO"`) || !strings.Contains(out, `"index": 1`) {
		t.Errorf("json output:\n%s", out)
	}
}
