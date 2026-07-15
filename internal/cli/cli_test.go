package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/app"
)

// runCmd executes the jw command tree with all base URLs pointed at a mock
// server built from mux, returning captured stdout.
func runCmd(t *testing.T, mux *http.ServeMux, args ...string) (string, error) {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	a := app.New(app.Flags{})
	var out bytes.Buffer
	a.Stdout = &out
	a.Stderr = &out

	root := NewRootCmd(a)
	full := append([]string{
		"--base-cdn", srv.URL, "--base-jworg", srv.URL, "--base-wol", srv.URL,
		"--cache-dir", t.TempDir(),
	}, args...)
	root.SetArgs(full)
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	return out.String(), err
}

// fixture serves a testdata file with the given content type.
func fixture(t *testing.T, path, contentType string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := os.ReadFile(filepath.Join("testdata", path))
		if err != nil {
			t.Errorf("fixture %s: %v", path, err)
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Write(b)
	}
}

func languagesMux(t *testing.T) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/apis/mediator/v1/languages/E/web", fixture(t, "languages.json", "application/json"))
	return mux
}

func TestLanguagesCommand(t *testing.T) {
	out, err := runCmd(t, languagesMux(t), "languages")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SYMBOL", "X", "de", "German", "Deutsch"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestLanguagesSearch(t *testing.T) {
	out, err := runCmd(t, languagesMux(t), "languages", "-s", "german")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "German") || strings.Contains(out, "French") {
		t.Errorf("filter failed:\n%s", out)
	}
}

func TestLanguagesJSON(t *testing.T) {
	out, err := runCmd(t, languagesMux(t), "languages", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"symbol": "X"`) || !strings.Contains(out, `"locale": "de"`) {
		t.Errorf("json output unexpected:\n%s", out)
	}
}
