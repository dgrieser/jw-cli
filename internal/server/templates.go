package server

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/dgrieser/jw-cli/internal/version"
)

//go:embed templates
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// htmlTemplate keeps server.go readable without importing html/template there.
type htmlTemplate = template.Template

// uiPages are the page templates, each parsed together with the base layout
// and the shared listing partial.
var uiPages = []string{
	"index", "search", "article", "bible", "dailytext", "meetings",
	"media", "media_category", "media_item", "pub", "error", "confirm",
}

// parseTemplates builds one template set per page at startup, so a parse error
// fails the process (and the tests) rather than a request.
func parseTemplates() map[string]*template.Template {
	out := make(map[string]*template.Template, len(uiPages))
	for _, page := range uiPages {
		out[page] = template.Must(template.New("base.html").ParseFS(templatesFS,
			"templates/base.html", "templates/listing.html", "templates/"+page+".html"))
	}
	return out
}

// serviceWorker serves static/sw.js from the root, so it may keep every page
// of the site, stamped with the build serving it: another build is a new
// worker, which drops what the old one kept. Behind a login, the worker asks
// the server for every page first, so its answer — and a revoked login with
// it — is never bypassed; the kept copy then only stands in while the server
// cannot be reached at all.
func (s *Server) serviceWorker(w http.ResponseWriter, r *http.Request) {
	src, err := staticFS.ReadFile("static/sw.js")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	src = bytes.ReplaceAll(src, []byte("__BUILD__"), []byte(buildStamp()))
	src = bytes.ReplaceAll(src, []byte("__AUTH__"), []byte(strconv.FormatBool(!s.auth.Empty())))
	_, _ = w.Write(src)
}

// buildStamp names this build: the version, and a hash of the binary itself,
// which tells two builds of one commit apart whatever changed in them (the
// embedded files alone, when the binary cannot be read).
var buildStamp = sync.OnceValue(func() string {
	h := sha256.New()
	if exe, err := os.Executable(); err == nil {
		if f, err := os.Open(exe); err == nil {
			_, err = io.Copy(h, f)
			_ = f.Close()
			if err == nil {
				return version.String() + "-" + hex.EncodeToString(h.Sum(nil))[:12]
			}
			h.Reset()
		}
	}
	for _, root := range []fs.FS{templatesFS, staticFS} {
		_ = fs.WalkDir(root, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := fs.ReadFile(root, path)
			if err == nil {
				h.Write([]byte(path))
				h.Write(b)
			}
			return nil
		})
	}
	return version.String() + "-" + hex.EncodeToString(h.Sum(nil))[:12]
})
