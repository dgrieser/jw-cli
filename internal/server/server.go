// Package server exposes the features of the jw CLI over HTTP: a JSON API
// under /api/v1 and a server-rendered web UI on the same routes the CLI's
// commands cover. It carries no authentication; jw serve binds to localhost
// unless asked otherwise.
package server

import (
	"net/http"
	"sync"
	"time"

	"github.com/dgrieser/jw-cli/internal/service"
)

// Config is what a Server is built from.
type Config struct {
	Svc *service.Service
	// DefaultLang is the language a request without ?lang= resolves; empty
	// falls back to the system locale, as the CLI does.
	DefaultLang string
	// Logf, when set, receives one line per request.
	Logf func(format string, args ...any)
}

// Server handles the API and UI routes around one shared service.
type Server struct {
	svc         *service.Service
	defaultLang string
	logf        func(format string, args ...any)
	// langMemo caches resolved languages per spec, so the language list is not
	// re-scanned on every request.
	langMemo sync.Map
	// templates holds one parsed set per UI page, built once at startup.
	templates map[string]*htmlTemplate
}

// New builds a Server. Template parse errors panic: they are programming
// errors, caught at startup and in tests rather than per request.
func New(cfg Config) *Server {
	s := &Server{
		svc:         cfg.Svc,
		defaultLang: cfg.DefaultLang,
		logf:        cfg.Logf,
	}
	if s.logf == nil {
		s.logf = func(string, ...any) {}
	}
	s.templates = parseTemplates()
	return s
}

// Handler is the full route table, wrapped in the request log.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// JSON API
	mux.HandleFunc("GET /api/v1/languages", s.apiLanguages)
	mux.HandleFunc("GET /api/v1/search", s.apiSearch)
	mux.HandleFunc("GET /api/v1/article", s.apiArticle)
	mux.HandleFunc("GET /api/v1/bible/read", s.apiBibleRead)
	mux.HandleFunc("GET /api/v1/bible/notes", s.apiBibleNotes)
	mux.HandleFunc("GET /api/v1/bible/xrefs", s.apiBibleXrefs)
	mux.HandleFunc("GET /api/v1/bible/research", s.apiBibleResearch)
	mux.HandleFunc("GET /api/v1/bible/media", s.apiBibleMedia)
	mux.HandleFunc("GET /api/v1/bible/cited", s.apiBibleCited)
	mux.HandleFunc("GET /api/v1/bible/books", s.apiBibleBooks)
	mux.HandleFunc("GET /api/v1/media/categories", s.apiMediaCategories)
	mux.HandleFunc("GET /api/v1/media/categories/{key}", s.apiMediaCategory)
	mux.HandleFunc("GET /api/v1/media/items/{lank}", s.apiMediaItem)
	mux.HandleFunc("GET /api/v1/pub", s.apiPub)
	mux.HandleFunc("GET /api/v1/dailytext", s.apiDailyText)
	mux.HandleFunc("GET /api/v1/meetings", s.apiMeetings)
	mux.HandleFunc("GET /api/v1/meetings/{part}", s.apiMeetingPart)

	// download selection: pick a rendition/file server-side, then redirect to
	// the public CDN URL rather than proxying the bytes
	mux.HandleFunc("GET /download/media/{lank}", s.downloadMedia)
	mux.HandleFunc("GET /download/pub", s.downloadPub)

	// web UI
	mux.Handle("GET /static/", http.FileServerFS(staticFS))
	mux.HandleFunc("GET /{$}", s.uiIndex)
	mux.HandleFunc("GET /search", s.uiSearch)
	mux.HandleFunc("GET /article", s.uiArticle)
	mux.HandleFunc("GET /bible", s.uiBible)
	mux.HandleFunc("GET /dailytext", s.uiDailyText)
	mux.HandleFunc("GET /meetings", s.uiMeetings)
	mux.HandleFunc("GET /meetings/{part}", s.uiMeetings)
	mux.HandleFunc("GET /media", s.uiMedia)
	mux.HandleFunc("GET /media/category/{key}", s.uiMediaCategory)
	mux.HandleFunc("GET /media/item/{lank}", s.uiMediaItem)
	mux.HandleFunc("GET /pub", s.uiPub)
	mux.HandleFunc("GET /languages", s.uiLanguages)

	return s.logged(mux)
}

// statusWriter remembers the status a handler sent, for the request log.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// logged writes one line per request: method, path, status, duration.
func (s *Server) logged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		s.logf("%s %s %d %s", r.Method, r.URL.RequestURI(), sw.status, time.Since(start).Round(time.Millisecond))
	})
}
