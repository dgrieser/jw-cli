package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
)

// langCookie remembers the language a reader picked, so it survives a link
// that carries no ?lang= of its own.
const langCookie = "lang"

// remembersLanguage keeps the language a reader picks on a page, so it
// survives the next link that carries no ?lang= of its own.
func (s *Server) remembersLanguage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.rememberLanguage(w, r)
		next.ServeHTTP(w, r)
	})
}

// rememberLanguage writes the cookie when a page names a language of its own
// that resolves, so the next page comes back in it. Only a reader picks a
// language this way: a call to the API, a download or a stream names the
// language it wants and is answered in it, without changing what a client
// keeping cookies is answered in next — and a language that does not resolve
// is no pick at all, which would fail every page after it. Called before
// anything is written.
func (s *Server) rememberLanguage(w http.ResponseWriter, r *http.Request) {
	spec := r.URL.Query().Get("lang")
	if spec == "" || !remembersOn(r.URL.Path) || readAhead(r) {
		return
	}
	if _, err := s.resolveLanguage(r.Context(), spec); err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: langCookie, Value: spec, Path: "/",
		MaxAge: int((365 * 24 * time.Hour).Seconds()),
		// nothing here is worth protecting; the cookie only picks a language
		SameSite: http.SameSiteLaxMode,
	})
}

// remembersOn reports whether a path is a page a reader reads, as opposed to
// the API, the downloads, the streams a page loads, and the page's own files.
func remembersOn(path string) bool {
	for _, prefix := range []string{"/api/", "/download/", "/unfold/", "/static/"} {
		if strings.HasPrefix(path, prefix) {
			return false
		}
	}
	return path != "/sw.js"
}

// language resolves the content language of one request: ?lang= wins, then the
// language the reader last picked, then the server's --lang default, then the
// system locale. A remembered language that no longer resolves — a symbol the
// language list dropped — is passed over for the default rather than failing
// every request that carries it.
func (s *Server) language(r *http.Request) (model.Language, error) {
	lng, err := s.requestLanguage(r)
	if err == nil && !httpx.InBackground(r.Context()) {
		s.warm.read(lng.Symbol)
	}
	return lng, err
}

func (s *Server) requestLanguage(r *http.Request) (model.Language, error) {
	if spec := r.FormValue("lang"); spec != "" {
		return s.resolveLanguage(r.Context(), spec)
	}
	if c, err := r.Cookie(langCookie); err == nil && c.Value != "" {
		if lng, err := s.resolveLanguage(r.Context(), c.Value); err == nil {
			return lng, nil
		}
	}
	return s.resolveLanguage(r.Context(), s.defaultLang)
}

// resolveLanguage resolves one language spec. Resolved languages are memoized
// by spec, so the language list is not re-scanned on every request — but only
// the ways the language list itself names a language (its symbol, its locale)
// and the server's default: any BCP-47 tag resolves too, and a memo of every
// one a request made up (de-x-abc123) would grow for as long as the server
// runs. A request reading past the cache (?refresh=1) reads the language list
// anew rather than the memo.
func (s *Server) resolveLanguage(ctx context.Context, spec string) (model.Language, error) {
	key := strings.ToLower(spec)
	if !httpx.Refreshing(ctx) {
		if v, ok := s.langMemo.Load(key); ok {
			return v.(model.Language), nil
		}
	}
	lng, err := s.svc.Language(ctx, spec)
	if err != nil {
		return model.Language{}, err
	}
	if key == strings.ToLower(s.defaultLang) || key == strings.ToLower(lng.Symbol) || key == strings.ToLower(lng.Locale) {
		s.langMemo.Store(key, lng)
	}
	return lng, nil
}

// text is the message catalog for a resolved language, so the frame around a
// document follows the document's own language where a catalog exists.
func text(lng model.Language) *i18n.Messages {
	return i18n.TextFor(lng.Locale)
}

// readAhead reports whether a request asks for a page ahead of the reader,
// who has not picked it, nor its language, yet.
func readAhead(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Sec-Purpose"), "prefetch") ||
		r.Header.Get("Purpose") == "prefetch" || r.Header.Get("X-JW-Ahead") != ""
}
