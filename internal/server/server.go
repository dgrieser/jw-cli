// Package server exposes the features of the jw CLI over HTTP: a JSON API
// under /api/v1 and a server-rendered web UI on the same routes the CLI's
// commands cover. Authentication is optional HTTP basic auth (see
// Credentials); jw serve binds to localhost unless asked otherwise.
package server

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dgrieser/jw-cli/internal/httpx"
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
	// Auth, when it holds users, puts every route behind HTTP basic
	// authentication.
	Auth *Credentials
	// PageCache is how the browser's copy of the pages is used (see
	// PageCachePolicies); empty is PageCacheAuto.
	PageCache string
}

// Server handles the API and UI routes around one shared service.
type Server struct {
	svc         *service.Service
	defaultLang string
	logf        func(format string, args ...any)
	auth        *Credentials
	pageCache   string
	// apiTimeout is how long one request to the JSON API may take
	// (limitsAPI).
	apiTimeout time.Duration
	// langMemo caches resolved languages by the specs the language list names
	// them by (resolveLanguage), so the list is not re-scanned on every
	// request.
	langMemo sync.Map
	// templates holds one parsed set per UI page, built once at startup.
	templates map[string]*htmlTemplate
	// runs are the verses each unfold run of a page has shown.
	runs runs
	// warm keeps the pages readers start from ready (warm.go); routes is
	// the route table it reads them through
	warm   warmer
	routes http.Handler
}

// New builds a Server. Template parse errors panic: they are programming
// errors, caught at startup and in tests rather than per request.
func New(cfg Config) *Server {
	s := &Server{
		svc:         cfg.Svc,
		defaultLang: cfg.DefaultLang,
		logf:        cfg.Logf,
		auth:        cfg.Auth,
		pageCache:   cfg.PageCache,
		apiTimeout:  apiTimeout,
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
	mux.HandleFunc("GET /api/v1/open", s.apiOpen)
	mux.HandleFunc("GET /api/v1/search", s.apiSearch)
	mux.HandleFunc("GET /api/v1/article", s.apiArticle)
	mux.HandleFunc("GET /api/v1/bible/read", s.apiBibleRead)
	mux.HandleFunc("GET /api/v1/bible/notes", s.apiBibleNotes)
	mux.HandleFunc("GET /api/v1/bible/xrefs", s.apiBibleXrefs)
	mux.HandleFunc("GET /api/v1/bible/research", s.apiBibleResearch)
	mux.HandleFunc("GET /api/v1/bible/media", s.apiBibleMedia)
	mux.HandleFunc("GET /api/v1/bible/cited", s.apiBibleCited)
	mux.HandleFunc("GET /api/v1/bible/books", s.apiBibleBooks)
	mux.HandleFunc("GET /api/v1/bible/nav", s.apiBibleNav)
	mux.HandleFunc("GET /api/v1/bible/verses", s.apiBibleVerses)
	mux.HandleFunc("GET /api/v1/bible/intro", s.apiBibleIntro)
	mux.HandleFunc("GET /api/v1/bible/outline", s.apiBibleOutline)
	mux.HandleFunc("GET /api/v1/bible/gallery", s.apiBibleGallery)
	mux.HandleFunc("GET /api/v1/media/categories", s.apiMediaCategories)
	mux.HandleFunc("GET /api/v1/media/categories/{key}", s.apiMediaCategory)
	mux.HandleFunc("GET /api/v1/media/items/{lank}", s.apiMediaItem)
	mux.HandleFunc("GET /api/v1/media/items/{lank}/text", s.apiMediaText)
	mux.HandleFunc("GET /api/v1/pub", s.apiPub)
	mux.HandleFunc("GET /api/v1/pub/library", s.apiPubLibrary)
	mux.HandleFunc("GET /api/v1/pub/library/{path...}", s.apiPubLibrary)
	mux.HandleFunc("GET /api/v1/pub/publication/{path...}", s.apiPubPublication)
	mux.HandleFunc("GET /api/v1/dailytext", s.apiDailyText)
	mux.HandleFunc("GET /api/v1/meetings", s.apiMeetings)
	mux.HandleFunc("GET /api/v1/meetings/{part}", s.apiMeetingPart)

	// download selection: pick a rendition/file server-side, then redirect to
	// the public CDN URL rather than proxying the bytes
	mux.HandleFunc("GET /download/media/{lank}", s.downloadMedia)
	mux.HandleFunc("GET /download/pub", s.downloadPub)

	// what the web UI loads once a page is on screen: the expansion of one
	// verse or of one paragraph, streamed section by section
	mux.HandleFunc("GET /unfold/verse", s.unfoldVerse)
	mux.HandleFunc("GET /unfold/chapter", s.unfoldChapter)
	mux.HandleFunc("GET /unfold/refs", s.unfoldRefs)
	mux.HandleFunc("GET /unfold/footnote", s.unfoldFootnote)
	mux.HandleFunc("GET /unfold/translations", s.unfoldTranslations)
	mux.HandleFunc("GET /unfold/cited", s.unfoldCited)
	mux.HandleFunc("GET /unfold/article", s.unfoldArticle)

	// web UI
	mux.Handle("GET /static/", staticFiles())
	mux.HandleFunc("GET /sw.js", s.serviceWorker)
	mux.HandleFunc("GET /{$}", s.uiIndex)
	mux.HandleFunc("GET /search", s.uiSearch)
	mux.HandleFunc("GET /article", s.uiArticle)
	mux.HandleFunc("GET /open", s.uiOpen)
	mux.HandleFunc("GET /bible", s.uiBible)
	mux.HandleFunc("GET /cited", s.uiCited)
	mux.HandleFunc("GET /dailytext", s.uiDailyText)
	mux.HandleFunc("GET /meetings", s.uiMeetings)
	mux.HandleFunc("GET /meetings/{part}", s.uiMeetings)
	mux.HandleFunc("GET /media", s.uiMedia)
	mux.HandleFunc("GET /media/category/{key}", s.uiMediaCategory)
	mux.HandleFunc("GET /media/item/{lank}", s.uiMediaItem)
	mux.HandleFunc("GET /pub", s.uiPubRoot)
	mux.HandleFunc("GET /pub/library/{path...}", s.uiPubLibrary)
	mux.HandleFunc("GET /pub/publication/{path...}", s.uiPubPublication)

	s.routes = mux
	return s.logged(s.auth.RequireAuth(s.busy(s.remembersLanguage(private(refreshes(s.limitsAPI(mux)))))))
}

// private marks what a page or the API answers as the reader's own: it
// follows the language cookie (and, behind a login, the login), so no cache
// between the server and the browser may hand it to anybody else, and the
// browser's own cache keeps it apart per cookie. The page's files and the
// service worker say for themselves how they are kept, and a handler that
// keeps nothing (a stream, a refresh) says so over this.
//
// Vary: Cookie leaves the page cache of the service worker (static/sw.js)
// as it was: it keeps and looks up its pages by a URL of its own, which
// never carries a cookie.
func private(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/static/") && r.URL.Path != "/sw.js" {
			w.Header().Set("Cache-Control", "private")
			w.Header().Add("Vary", "Cookie")
		}
		next.ServeHTTP(w, r)
	})
}

// apiTimeout bounds one request to the JSON API. It is generous: an
// expansion forced past the confirmation (force=1) is documented to take
// minutes. A page is read by somebody who can stop it, and a stream says
// how far it has got and stops when its page does; a call to the API is
// often unattended, and without a bound one would run for as long as its
// expansion does.
const apiTimeout = 10 * time.Minute

// limitsAPI answers a request to the JSON API that is not done within
// s.apiTimeout with 504 and the API's error envelope, and stops what it was
// doing. The handler writes to a buffer meanwhile, so whatever it would still
// write once the deadline has passed goes nowhere, and a handler that does
// not notice the deadline at once does not hold the answer up.
func (s *Server) limitsAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), s.apiTimeout)
		defer cancel()
		buf := &bufferedResponse{header: http.Header{}}
		done := make(chan struct{})
		panicked := make(chan any, 1)
		go func() {
			defer func() {
				if p := recover(); p != nil {
					panicked <- p
				}
			}()
			next.ServeHTTP(buf, r.WithContext(ctx))
			close(done)
		}()
		select {
		case p := <-panicked:
			panic(p)
		case <-done:
			buf.writeTo(w)
		case <-ctx.Done():
			if r.Context().Err() != nil {
				return // the client went away; nobody is listening
			}
			select {
			case <-done: // done just in time after all
				buf.writeTo(w)
				return
			default:
			}
			writeError(w, http.StatusGatewayTimeout, "timeout",
				fmt.Sprintf("the request was not done within %s and was stopped", s.apiTimeout))
		}
	})
}

// bufferedResponse holds what a handler writes until it is done.
type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header { return b.header }

func (b *bufferedResponse) WriteHeader(code int) {
	if b.status == 0 {
		b.status = code
	}
}

func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}

// writeTo sends what was held on, over whatever headers the middleware
// around the handler set already.
func (b *bufferedResponse) writeTo(w http.ResponseWriter) {
	maps.Copy(w.Header(), b.header)
	if b.status == 0 {
		b.status = http.StatusOK
	}
	w.WriteHeader(b.status)
	_, _ = b.body.WriteTo(w)
}

// refreshes reads past the cache for a request that asks with ?refresh=1:
// every page of jw.org it needs is fetched anew, once, and replaces what was
// kept. The parameter goes before the handler sees the request, so no link,
// form or remembered page built from it carries it on.
func refreshes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("refresh") != "1" {
			next.ServeHTTP(w, r)
			return
		}
		q.Del("refresh")
		r = r.WithContext(httpx.WithRefresh(r.Context()))
		r.URL.RawQuery = q.Encode()
		r.Form = nil
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
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

// Unwrap lets http.ResponseController reach the connection underneath, so a
// stream can flush through the request log.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// logged writes one line per request: method, path, status, duration.
func (s *Server) logged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		s.logf("%s %s %d %s", r.Method, r.URL.RequestURI(), sw.status, time.Since(start).Round(time.Millisecond))
	})
}
