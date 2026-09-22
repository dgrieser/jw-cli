package server

import (
	"net/http"
	"time"

	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
)

// langCookie remembers the language a reader picked, so it survives a link
// that carries no ?lang= of its own.
const langCookie = "lang"

// rememberLanguage writes the cookie when a request names a language of its
// own, so the next page comes back in it. Called before anything is written.
func rememberLanguage(w http.ResponseWriter, r *http.Request) {
	spec := r.URL.Query().Get("lang")
	if spec == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: langCookie, Value: spec, Path: "/",
		MaxAge: int((365 * 24 * time.Hour).Seconds()),
		// nothing here is worth protecting; the cookie only picks a language
		SameSite: http.SameSiteLaxMode,
	})
}

// language resolves the content language of one request: ?lang= wins, then the
// language the reader last picked, then the server's --lang default, then the
// system locale. Resolved languages are memoized per spec, so the language list
// is not re-scanned on every request.
func (s *Server) language(r *http.Request) (model.Language, error) {
	spec := r.FormValue("lang")
	if spec == "" {
		if c, err := r.Cookie(langCookie); err == nil {
			spec = c.Value
		}
	}
	if spec == "" {
		spec = s.defaultLang
	}
	if v, ok := s.langMemo.Load(spec); ok {
		return v.(model.Language), nil
	}
	lng, err := s.svc.Language(r.Context(), spec)
	if err != nil {
		return model.Language{}, err
	}
	s.langMemo.Store(spec, lng)
	return lng, nil
}

// text is the message catalog for a resolved language, so the frame around a
// document follows the document's own language where a catalog exists.
func text(lng model.Language) *i18n.Messages {
	return i18n.TextFor(lng.Locale)
}
