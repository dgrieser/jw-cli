package server

import (
	"context"
	"fmt"
	"time"

	"github.com/dgrieser/jw-cli/internal/bibleref"
	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/service"
)

// The week's bible reading is studied ahead: once a week, at night, every
// chapter of it is unfolded a level deep the way jw bible read --unfold 1
// does it — the study notes, footnotes, indexes and marginal references of
// each verse with the verses they cite, who quotes the verse, the other
// translations and the glossary entries of its words — in the languages
// configured, for what that reads to be in the cache when a reader opens the
// reading during the week. It reads thousands of pages, so it reads them
// slowly (studyPace), and only at night (studyFrom to studyUntil, the
// server's local time): a night that does not see it through has the next
// one go on where it stopped, what was read already coming from the cache.
// The content of each chapter — what the "Inspired" book, the highlights,
// the Bible handbook and Insight say on it, which its heading loads once
// opened — is read first, being a few documents only.

// DefaultStudyLangs are the languages the week's reading is studied in when
// Config names none: English and German.
var DefaultStudyLangs = []string{"E", "X"}

// studyFrom and studyUntil are the hours of the night, local time, the
// reading is studied in.
const (
	studyFrom  = 1
	studyUntil = 6
)

// studyPace is how long each request upstream the study makes waits after
// the one before it.
var studyPace = 3 * time.Second

// studyEdition is the bible the reading is studied in: the one the bible
// pages read, with its study material.
const studyEdition = "nwtsty"

// Study studies the week's reading ahead every night it was not yet, until
// ctx is done. It returns at once without background revalidation, or
// without a language to study it in.
func (s *Server) Study(ctx context.Context) {
	if !s.svc.HTTP.Backgrounds() || len(s.studyLangs) == 0 {
		return
	}
	for {
		from, until := nightOf(time.Now())
		if !sleepUntil(ctx, from) {
			return
		}
		night, cancel := context.WithDeadline(ctx, until)
		s.studyWeek(night, time.Now())
		cancel()
		if !sleepUntil(ctx, until) {
			return
		}
	}
}

// nightOf is the night the study runs in next: the one under way at now, or
// else the coming one.
func nightOf(now time.Time) (from, until time.Time) {
	y, m, d := now.Date()
	from = time.Date(y, m, d, studyFrom, 0, 0, 0, now.Location())
	until = time.Date(y, m, d, studyUntil, 0, 0, 0, now.Location())
	if !now.Before(until) {
		from = time.Date(y, m, d+1, studyFrom, 0, 0, 0, now.Location())
		until = time.Date(y, m, d+1, studyUntil, 0, 0, 0, now.Location())
	}
	return from, until
}

// sleepUntil waits until t, and reports false when ctx is done first.
func sleepUntil(ctx context.Context, t time.Time) bool {
	timer := time.NewTimer(time.Until(t))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// studyContent reads the content of each chapter of a week's reading ahead,
// once a week and language; it reports false when the night ended first.
func (s *Server) studyContent(ctx context.Context, lng model.Language, rd service.WeekReading) bool {
	key := fmt.Sprintf("studiedcontent1-%s-%d-%d", lng.Locale, rd.Year, rd.Week)
	var done bool
	if s.svc.Cache.Get(key, &done) && done {
		return true
	}
	read := true
	for _, ch := range rd.Chapters {
		_, err := s.svc.BookContent(ctx, lng, ch.Book, ch.Chapter)
		if ctx.Err() != nil {
			return false
		}
		if err != nil {
			s.logf("study: %s content of %d:%d: %v", lng.Symbol, ch.Book, ch.Chapter, err)
			read = false
		}
	}
	if read {
		s.svc.Cache.Put(key, true)
	}
	return true
}

// studyWeek unfolds the chapters of the reading of the week containing date
// in every language configured that has not had them unfolded yet. A week
// whose reading is not out, or that has none, has nothing to study.
func (s *Server) studyWeek(ctx context.Context, date time.Time) {
	ctx = httpx.Paced(httpx.Background(ctx), studyPace)
	for _, spec := range s.studyLangs {
		lng, err := s.resolveLanguage(ctx, spec)
		if err != nil {
			continue
		}
		rd, err := s.svc.WeekReading(ctx, lng, date)
		if err != nil {
			continue
		}
		if !s.studyContent(ctx, lng, rd) {
			return
		}
		key := fmt.Sprintf("studied1-%s-%d-%d", lng.Locale, rd.Year, rd.Week)
		var done bool
		if s.svc.Cache.Get(key, &done) && done {
			continue
		}
		start := time.Now()
		studied := true
		for _, ch := range rd.Chapters {
			ref := bibleref.Ref{Book: ch.Book, Chapter: ch.Chapter}
			_, err := s.svc.ReadPassages(ctx, lng, service.ReadRequest{
				// the English book names are known in every language
				Refs:    ref.String(),
				Edition: studyEdition,
				Unfold:  service.UnfoldConfig{Depth: 1, Glossary: true, Cited: true},
			}, text(lng))
			if ctx.Err() != nil {
				s.logf("study: %s %s stopped for the night, after %s", lng.Symbol, ref, time.Since(start).Round(time.Second))
				return
			}
			if err != nil {
				s.logf("study: %s %s: %v", lng.Symbol, ref, err)
				studied = false
			}
		}
		if studied {
			s.svc.Cache.Put(key, true)
			s.logf("study: %s week %d of %d studied (%s), in %s", lng.Symbol, rd.Week, rd.Year, rd.Title,
				time.Since(start).Round(time.Second))
		}
	}
}
