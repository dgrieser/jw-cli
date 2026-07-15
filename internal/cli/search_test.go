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
		payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, time.Now().Add(time.Hour).Unix())))
		fmt.Fprint(w, "h."+payload+".s")
	})
	mux.HandleFunc("/apis/search/results/X/videos", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "no token", 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"results": [{"type": "item", "subtype": "video", "title": "Schöpfung", "lank": "pub-xyz_1_VIDEO",
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
	for _, want := range []string{"1 results", "[video] Schöpfung (3:10)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
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
