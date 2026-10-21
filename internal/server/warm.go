package server

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
)

// A page is quick when what it is built from is in the cache and current. The
// pages a reader starts from — the start page, the publications, the media,
// the bible, today's text, this week's meetings — draw on hundreds of pages
// of jw.org each, and what they show changes by the day: so the server reads
// them ahead, in the background and in the languages its readers read, when
// it starts and every warmEvery after, and a reader finds them ready. What a
// reader is likely to ask for next — the chapter after the one on screen — is
// read ahead the same way. Reading ahead gives way to the readers' own
// requests (see httpx.Background), and is done only with background
// revalidation on, as jw serve has it.

// warmEvery is how often the start pages are read ahead. Most of the time
// nothing they draw on is due to be revalidated, and reading them costs a
// few milliseconds from the cache.
const warmEvery = time.Hour

// warmPages are the pages a reader starts from.
var warmPages = []string{"/", "/pub", "/media", "/bible", "/dailytext", "/meetings"}

// The weeks' bible reading every bible picker marks is read ahead a quarter
// of a year, once a week: the workbooks are out months ahead, and what the
// service read of them it keeps for a week (service.WeekReading). The index
// of the glossaries' words is read ahead as often (warmGlossaries).
const (
	readingPage  = "/api/v1/bible/reading?weeks=13"
	readingEvery = 7 * 24 * time.Hour
)

// warmLangs is how many of the languages readers read lately the start
// pages are kept ready in, besides the server's default.
const warmLangs = 3

// warmer remembers the languages readers read lately, latest first.
type warmer struct {
	mu    sync.Mutex
	langs []string
}

// read notes a language a reader read in, by its symbol.
func (w *warmer) read(symbol string) {
	if symbol == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if i := slices.Index(w.langs, symbol); i == 0 {
		return
	} else if i > 0 {
		w.langs = slices.Delete(w.langs, i, i+1)
	}
	w.langs = slices.Insert(w.langs, 0, symbol)
	if len(w.langs) > warmLangs {
		w.langs = w.langs[:warmLangs]
	}
}

func (w *warmer) recent() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.langs)
}

// Warm reads the start pages ahead now and every warmEvery after, until ctx
// is done. It returns at once without background revalidation.
func (s *Server) Warm(ctx context.Context) {
	if !s.svc.HTTP.Backgrounds() {
		return
	}
	if s.routes == nil {
		s.Handler()
	}
	t := time.NewTicker(warmEvery)
	defer t.Stop()
	// when the reading was last read ahead, by language
	readAt := map[string]time.Time{}
	for {
		// the server's default language, and those readers read lately
		def, _ := s.resolveLanguage(httpx.Background(ctx), s.defaultLang)
		for _, lang := range append([]string{""}, s.warm.recent()...) {
			if lang != "" && lang == def.Symbol {
				continue
			}
			pages := warmPages
			if time.Since(readAt[lang]) >= readingEvery {
				readAt[lang] = time.Now()
				pages = append(slices.Clip(pages), readingPage)
			}
			for _, p := range pages {
				if lang != "" {
					sep := "?"
					if strings.Contains(p, "?") {
						sep = "&"
					}
					p += sep + url.Values{"lang": {lang}}.Encode()
				}
				s.readAhead(p)
			}
		}
		s.warmGlossaries(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// prefetch reads a page of the site ahead, in the background, in the
// language of the page that leads to it. A page read ahead leads nowhere
// further.
func (s *Server) prefetch(ctx context.Context, lng model.Language, href string) {
	if href == "" || httpx.InBackground(ctx) {
		return
	}
	u, err := url.Parse(href)
	if err != nil || u.IsAbs() {
		return
	}
	if q := u.Query(); q.Get("lang") == "" && lng.Symbol != "" {
		q.Set("lang", lng.Symbol)
		u.RawQuery = q.Encode()
	}
	s.readAhead(u.String())
}

// readAhead builds the page at target in the background, for what it reads
// to be in the cache when a reader asks for it.
func (s *Server) readAhead(target string) {
	if s.routes == nil {
		return
	}
	s.svc.HTTP.Go("page "+target, func(ctx context.Context) {
		r, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return
		}
		s.routes.ServeHTTP(discard{header: http.Header{}}, r)
	})
}

// busy notes every request but for the page's own files as a reader's, which
// background work gives way to while it is under way.
func (s *Server) busy(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !httpx.InBackground(r.Context()) && r.URL.Path != "/sw.js" && !strings.HasPrefix(r.URL.Path, "/static/") {
			defer s.svc.HTTP.Busy()()
		}
		next.ServeHTTP(w, r)
	})
}

// discard is where a page read ahead is written: nowhere.
type discard struct{ header http.Header }

func (d discard) Header() http.Header         { return d.header }
func (d discard) Write(p []byte) (int, error) { return len(p), nil }
func (d discard) WriteHeader(int)             {}
