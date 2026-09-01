package server

import (
	"net/http"

	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
)

// language resolves the content language of one request: ?lang= wins, then the
// server's --lang default, then the system locale. Resolved languages are
// memoized per spec, so the language list is not re-scanned on every request.
func (s *Server) language(r *http.Request) (model.Language, error) {
	spec := r.FormValue("lang")
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
