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

// The ways the browser's copy of the pages (static/sw.js) is used:
//
//   - PageCacheFirst: a page read once comes back from the copy, without the
//     server, until it is out of date — at once, and offline;
//   - PageCacheFallback: every page is asked of the server first, so whoever
//     decides who may read it (a login here, or in front of the server) does,
//     every time; the copy only stands in while the server cannot be reached;
//   - PageCacheOff: no page is kept;
//   - PageCacheAuto: PageCacheFallback behind a login — this server's own, or
//     one in front of it that the request for the worker shows (an
//     Authorization header, or a header naming the user a proxy let in) —
//     and PageCacheFirst otherwise.
const (
	PageCacheAuto     = "auto"
	PageCacheFirst    = "first"
	PageCacheFallback = "fallback"
	PageCacheOff      = "off"
)

// PageCachePolicies are the values Config.PageCache takes.
var PageCachePolicies = []string{PageCacheAuto, PageCacheFirst, PageCacheFallback, PageCacheOff}

// proxyUserHeaders are where authenticating proxies name the user they let
// through (oauth2-proxy, Authelia, Authentik, Cloudflare Access, Apache).
var proxyUserHeaders = []string{
	"Authorization", "X-Forwarded-User", "X-Forwarded-Email", "X-Auth-Request-User",
	"X-Auth-Request-Email", "Remote-User", "X-Remote-User", "Cf-Access-Authenticated-User-Email",
}

// pageCachePolicy is the way this request's worker uses its copy.
func (s *Server) pageCachePolicy(r *http.Request) string {
	switch s.pageCache {
	case PageCacheFirst, PageCacheFallback, PageCacheOff:
		return s.pageCache
	}
	if !s.auth.Empty() {
		return PageCacheFallback
	}
	for _, h := range proxyUserHeaders {
		if r.Header.Get(h) != "" {
			return PageCacheFallback
		}
	}
	return PageCacheFirst
}

// serviceWorker serves static/sw.js from the root, so it may keep every page
// of the site, stamped with the build serving it — another build is a new
// worker, which drops what the old one kept — and with the way it uses its
// copy (pageCachePolicy).
func (s *Server) serviceWorker(w http.ResponseWriter, r *http.Request) {
	src, err := staticFS.ReadFile("static/sw.js")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	// a worker fetched through a login is not one to hand to another
	w.Header().Set("Vary", "Authorization")
	src = bytes.ReplaceAll(src, []byte("__BUILD__"), []byte(buildStamp()))
	src = bytes.ReplaceAll(src, []byte("__POLICY__"), []byte(s.pageCachePolicy(r)))
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
