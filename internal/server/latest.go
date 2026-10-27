package server

import (
	"context"
	"sync"
	"time"
	_ "time/tzdata" // the updates of the latest media keep Berlin time wherever the server runs

	"golang.org/x/sync/singleflight"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
)

// The media start page's row of the latest videos holds jw.org's pick of
// them and, after it, everything else the media library published in the
// last latestDays (service.RecentMedia). Finding that walks the whole
// category tree — a few dozen requests and some megabytes in each language —
// so what was found is kept, and found anew every latestEvery during the day,
// from latestFrom to latestUntil, Berlin time: at 6, 9, 12, 15, 18 and 21
// o'clock, and not at night. In each language readers read it in since the
// update before; another one's is found anew when a reader next asks.

const (
	latestDays  = 30
	latestFrom  = 6
	latestUntil = 22
	latestEvery = 3 * time.Hour
)

// latestZone is the time the updates keep.
var latestZone = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return time.UTC
	}
	return loc
}()

// latestUpdates are the times of the updates on the day of t, in order.
func latestUpdates(t time.Time) []time.Time {
	y, m, d := t.In(latestZone).Date()
	var at []time.Time
	for h := latestFrom; h < latestUntil; h += int(latestEvery / time.Hour) {
		at = append(at, time.Date(y, m, d, h, 0, 0, 0, latestZone))
	}
	return at
}

// lastUpdate is the time of the latest update at or before now.
func lastUpdate(now time.Time) time.Time {
	for _, day := range []time.Time{now, now.In(latestZone).AddDate(0, 0, -1)} {
		at := latestUpdates(day)
		for i := len(at) - 1; i >= 0; i-- {
			if !at[i].After(now) {
				return at[i]
			}
		}
	}
	return time.Time{}
}

// nextUpdate is the time of the first update after now.
func nextUpdate(now time.Time) time.Time {
	for _, day := range []time.Time{now, now.In(latestZone).AddDate(0, 0, 1)} {
		for _, at := range latestUpdates(day) {
			if at.After(now) {
				return at
			}
		}
	}
	return time.Time{}
}

// latestMedia keeps what the media library published lately, by language.
type latestMedia struct {
	mu     sync.Mutex
	byLang map[string]latestList
	flight singleflight.Group
}

type latestList struct {
	media []model.MediaItem
	// found is when the media were found, read when a reader last asked
	found, read time.Time
}

// latest is what the media library published in lang in the last
// latestDays, newest first: as found at the last update, or else found now.
// A list found before the last update is used while it is found anew in the
// background, when the server does background work.
func (s *Server) latest(ctx context.Context, lang string) []model.MediaItem {
	l, ok := s.latestMedia.get(lang, time.Now())
	if ok && !httpx.Refreshing(ctx) {
		if !l.found.Before(lastUpdate(time.Now())) {
			return l.media
		}
		if s.svc.HTTP.Go("latest "+lang, func(ctx context.Context) {
			s.findLatest(httpx.WithRefresh(ctx), lang)
		}) {
			return l.media
		}
	}
	media, err := s.findLatest(ctx, lang)
	if err != nil && ok {
		return l.media
	}
	return media
}

// findLatest finds what the media library published in lang lately, and
// keeps it. Finds under way at once share one walk, which goes on when the
// reader who asked goes away.
func (s *Server) findLatest(ctx context.Context, lang string) ([]model.MediaItem, error) {
	ch := s.latestMedia.flight.DoChan(lang, func() (any, error) {
		ctx := context.WithoutCancel(ctx)
		media, err := s.svc.RecentMedia(ctx, lang, time.Now().AddDate(0, 0, -latestDays))
		if err != nil {
			s.logf("latest media: %s: %v", lang, err)
		}
		if media == nil && err != nil {
			return nil, err
		}
		// a category that failed misses until the next update rather than
		// having every page ask for it again
		s.latestMedia.put(lang, media, time.Now())
		return media, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.Err != nil {
			return nil, r.Err
		}
		return r.Val.([]model.MediaItem), nil
	}
}

func (l *latestMedia) get(lang string, now time.Time) (latestList, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	list, ok := l.byLang[lang]
	if ok {
		list.read = now
		l.byLang[lang] = list
	}
	return list, ok
}

func (l *latestMedia) put(lang string, media []model.MediaItem, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.byLang == nil {
		l.byLang = map[string]latestList{}
	}
	list := l.byLang[lang]
	list.media, list.found = media, now
	if list.read.IsZero() {
		list.read = now // found for a reader
	}
	l.byLang[lang] = list
}

// readSince lists the languages a reader asked for the latest media in
// since t.
func (l *latestMedia) readSince(t time.Time) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var langs []string
	for lang, list := range l.byLang {
		if !list.read.Before(t) {
			langs = append(langs, lang)
		}
	}
	return langs
}

// Latest finds the latest media anew at every update, in the languages
// readers read them in since the update before, until ctx is done. It
// returns at once without background revalidation.
func (s *Server) Latest(ctx context.Context) {
	if !s.svc.HTTP.Backgrounds() {
		return
	}
	for {
		prev := lastUpdate(time.Now())
		if !sleepUntil(ctx, nextUpdate(time.Now())) {
			return
		}
		for _, lang := range s.latestMedia.readSince(prev) {
			s.svc.HTTP.Go("latest "+lang, func(ctx context.Context) {
				s.findLatest(httpx.WithRefresh(ctx), lang)
			})
		}
	}
}
