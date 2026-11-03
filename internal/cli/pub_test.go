package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// pubMux serves a GETPUBMEDIALINKS response whose file URL points back at the
// same mock server, so download-by-index can be tested end to end.
func pubMux(t *testing.T) *http.ServeMux {
	mux := languagesMux(t)
	mux.HandleFunc("/apis/pub-media/GETPUBMEDIALINKS", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pub") != "w" || r.URL.Query().Get("langwritten") != "X" {
			http.Error(w, "unexpected query "+r.URL.RawQuery, 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"pubName": "Der Wachtturm Mai 2024",
			"pub": "w", "issue": "202405",
			"languages": {"X": {"name": "German", "locale": "de"}},
			"files": {"X": {"PDF": [{
				"title": "Der Wachtturm, Mai 2024",
				"file": {"url": "http://%s/files/w_X_202405.pdf", "checksum": ""},
				"filesize": 11, "label": "", "track": 0, "docid": 2024365,
				"mimetype": "application/pdf"
			}]}}
		}`, r.Host)
	})
	mux.HandleFunc("/files/w_X_202405.pdf", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("PDF CONTENT"))
	})
	return mux
}

func TestPubListAndDownloadByIndex(t *testing.T) {
	mux := pubMux(t)
	cacheDir := t.TempDir()
	dlDir := t.TempDir()

	out, err := runCmdWithCache(t, mux, cacheDir, "pub", "w", "--issue", "202405", "-l", "de")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Der Wachtturm") || !strings.Contains(out, "PDF") {
		t.Fatalf("listing unexpected:\n%s", out)
	}

	out, err = runCmdWithCache(t, mux, cacheDir, "download", "1", "-l", "de", "-d", dlDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Heruntergeladen") { // -l de
		t.Fatalf("download output unexpected:\n%s", out)
	}
	b, err := os.ReadFile(filepath.Join(dlDir, "w_X_202405.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "PDF CONTENT" {
		t.Errorf("file content = %q", b)
	}
}

// runCmdWithCache is runCmd with a caller-controlled cache dir so multiple
// invocations share the results cache. Like runCmd, it runs the command in
// remote mode as well, from a client whose cache is kept for the same test
// and cache dir — so a listing and the jw download <n> after it go through a
// server just as they go locally — and holds the two runs to the same output.
func runCmdWithCache(t *testing.T, mux *http.ServeMux, cacheDir string, args ...string) (string, error) {
	t.Helper()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	out, err := runLocalCmd(t, server.URL, append([]string{"--cache-dir", cacheDir}, args...)...)
	rout, rerr := runRemoteCmd(t, server.URL, append([]string{"--cache-dir", remoteCacheDir(t, cacheDir)}, args...)...)
	checkParity(t, args, out, err, rout, rerr)
	return out, err
}

// remoteCacheDirs are the caches of the clients of remote mode, by the
// local cache dir they stand beside.
var remoteCacheDirs sync.Map

func remoteCacheDir(t *testing.T, local string) string {
	dir, _ := remoteCacheDirs.LoadOrStore(local, t.TempDir())
	return dir.(string)
}
