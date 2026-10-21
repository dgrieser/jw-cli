package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/service"
)

// DefaultGlossaryLangs are the languages whose glossaries are indexed when
// Config names none: English and German.
var DefaultGlossaryLangs = []string{"E", "X"}

// glossaryLang reports whether the glossaries of lng are indexed.
func (s *Server) glossaryLang(ctx context.Context, lng model.Language) bool {
	for _, spec := range s.glossaryLangs {
		if l, err := s.resolveLanguage(ctx, spec); err == nil && l.Symbol == lng.Symbol {
			return true
		}
	}
	return false
}

// markGlossary marks the words the glossaries of lng explain in a fragment of
// a page (service.MarkGlossary), when they are indexed.
func (s *Server) markGlossary(ctx context.Context, lng model.Language, fragment string, own ...int) string {
	if !s.glossaryLang(ctx, lng) {
		return fragment
	}
	return s.svc.MarkGlossary(lng, fragment, own...)
}

// apiGlossary is the index of the words the glossaries of a language explain
// (service.Glossary): GET /api/v1/glossary?lang=de. A language whose
// glossaries are not indexed has no terms.
func (s *Server) apiGlossary(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	g := service.Glossary{Lang: lng.Symbol, Books: []service.GlossaryBook{}, Terms: []service.GlossaryTerm{}}
	if s.glossaryLang(r.Context(), lng) {
		if g, err = s.svc.Glossary(r.Context(), lng); err != nil {
			failJSON(w, r, err)
			return
		}
	}
	// thousands of terms: not indented
	b, err := json.Marshal(g)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(append(b, '\n'))
}

// glossaryWarm reads the glossary indexes ahead, once a week like the week's
// bible reading: an index read only in part is read again at the next round.
type glossaryWarm struct {
	mu sync.Mutex
	at map[string]time.Time // when each language's index was read whole
}

// warmGlossaries reads ahead the index of every language configured whose
// index was not read whole within readingEvery.
func (s *Server) warmGlossaries(ctx context.Context) {
	for _, spec := range s.glossaryLangs {
		lng, err := s.resolveLanguage(httpx.Background(ctx), spec)
		if err != nil {
			continue
		}
		s.glossWarm.mu.Lock()
		due := time.Since(s.glossWarm.at[lng.Symbol]) >= readingEvery
		s.glossWarm.mu.Unlock()
		if !due {
			continue
		}
		s.svc.HTTP.Go("glossary "+lng.Symbol, func(ctx context.Context) {
			if g, err := s.svc.Glossary(ctx, lng); err == nil && !g.Partial {
				s.glossWarm.mu.Lock()
				defer s.glossWarm.mu.Unlock()
				if s.glossWarm.at == nil {
					s.glossWarm.at = map[string]time.Time{}
				}
				s.glossWarm.at[lng.Symbol] = time.Now()
			}
		})
	}
}
