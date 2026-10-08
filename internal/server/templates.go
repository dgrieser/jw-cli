package server

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"html/template"
	"io/fs"
	"net/http"
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
// of the site, stamped with what it serves: a build with other pages or
// other files is a new worker, which drops what the old one kept.
func serviceWorker(w http.ResponseWriter, r *http.Request) {
	src, err := staticFS.ReadFile("static/sw.js")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(bytes.ReplaceAll(src, []byte("__BUILD__"), []byte(buildStamp())))
}

// buildStamp names this build's pages and files: the version, and a hash of
// everything embedded, which tells two builds of one commit apart.
var buildStamp = sync.OnceValue(func() string {
	h := sha256.New()
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
