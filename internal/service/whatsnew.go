package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // the site's days are Berlin's wherever this runs

	"github.com/dgrieser/jw-cli/internal/api/pubmedia"
	"github.com/dgrieser/jw-cli/internal/model"
)

// What is new on the site comes from two places: jw.org's page of what is
// new ("Neu auf jw.org") lists the articles, publications and news, and
// hardly any media; the media library's latest are jw.org's pick of the
// latest videos (LatestVideos) and everything else it published lately
// (RecentMedia). Latest puts them together.

// LatestVideosCategory is jw.org's pick of the latest videos.
const LatestVideosCategory = "LatestVideos"

// LatestZone is the time the site's days are counted in: jw.org's page of
// what is new dates an entry by its day alone.
var LatestZone = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return time.UTC
	}
	return loc
}()

// WhatsNew is what jw.org's page of what is new lists in a language: its
// articles, publications, news and videos, newest first.
func (s *Service) WhatsNew(ctx context.Context, lang string) ([]model.NewItem, error) {
	return s.JWOrg.WhatsNew(ctx, lang)
}

// ErrWhatsNew marks the failure to read jw.org's page of what is new among
// what Latest could not read: the media come without the articles,
// publications and news.
var ErrWhatsNew = errors.New("jw.org's list of what is new could not be read")

// LatestFilter says what Latest reads.
type LatestFilter struct {
	// Since bounds the media walked for; the zero time takes 30 days.
	Since time.Time
	// NoMedia leaves the media library out: jw.org's page alone, without
	// the walk of the media library's whole category tree.
	NoMedia bool
}

// Latest is what is new on the site in lang, newest first: jw.org's page of
// what is new with the media library's latest. A part that cannot be read
// is left out and the error says so; it is an error alone when nothing
// could be read.
func (s *Service) Latest(ctx context.Context, lang string, f LatestFilter) ([]model.NewItem, error) {
	if f.Since.IsZero() {
		f.Since = time.Now().AddDate(0, 0, -30)
	}
	var (
		news      []model.NewItem
		pick      model.Category
		recent    []model.MediaItem
		newsErr   error
		pickErr   error
		recentErr error
		wg        sync.WaitGroup
	)
	wg.Go(func() { news, newsErr = s.WhatsNew(ctx, lang) })
	if !f.NoMedia {
		wg.Go(func() { pick, pickErr = s.Mediator.Category(ctx, lang, LatestVideosCategory, 24, 0) })
		wg.Go(func() { recent, recentErr = s.RecentMedia(ctx, lang, f.Since) })
	}
	wg.Wait()
	if newsErr != nil {
		newsErr = fmt.Errorf("%w: %w", ErrWhatsNew, newsErr)
	}
	if errors.Is(pickErr, pubmedia.ErrNotFound) {
		pickErr = nil
	}
	err := errors.Join(newsErr, pickErr, recentErr)
	items := LatestItems(news, MergeRecent(pick.Media, recent))
	if len(items) == 0 && err != nil {
		return nil, err
	}
	return items, err
}

// MergeRecent is jw.org's pick of the latest media with the recent media it
// does not have, newest first. A recording comes once: the video of a talk
// or a song is listed again as audio, and a song in each album it is on,
// under the same key or title; the pick's comes first, then a video.
func MergeRecent(pick, recent []model.MediaItem) []model.MediaItem {
	var videos, others []model.MediaItem
	for _, m := range recent {
		if m.Type == "video" {
			videos = append(videos, m)
		} else {
			others = append(others, m)
		}
	}
	var media []model.MediaItem
	stems, titles := map[string]bool{}, map[string]bool{}
	for _, m := range slices.Concat(pick, videos, others) {
		stem, title := MediaStem(m.LANK), strings.ToLower(strings.TrimSpace(m.Title))
		if stems[stem] || title != "" && titles[title] {
			continue
		}
		stems[stem], titles[title] = true, true
		media = append(media, m)
	}
	NewestFirst(media)
	return media
}

// LatestItems puts jw.org's page of what is new and the latest media
// together, newest day first. On a day, the page's entries come first in its
// order, then the media of the day, latest first. A video the page lists
// that is among the media comes once, as the media have it; a medium without
// a date is left out.
func LatestItems(news []model.NewItem, media []model.MediaItem) []model.NewItem {
	type entry struct {
		at    time.Time
		media bool
		item  model.NewItem
	}
	var entries []entry
	listed := map[string]bool{}
	for _, m := range media {
		at := Published(m)
		if at.IsZero() {
			continue
		}
		listed[MediaStem(m.LANK)] = true
		entries = append(entries, entry{at: at, media: true, item: model.NewItem{
			Kind:      m.Type,
			Title:     m.Title,
			Published: at.In(LatestZone).Format(time.DateOnly),
			ImageURL:  BestImage(m.Images),
			LANK:      m.LANK,
			Duration:  m.DurationFormatted,
		}})
	}
	for _, it := range news {
		if it.LANK != "" && listed[MediaStem(it.LANK)] {
			continue
		}
		at, err := time.ParseInLocation(time.DateOnly, it.Published, LatestZone)
		if err != nil {
			continue
		}
		entries = append(entries, entry{at: at, item: it})
	}
	day := func(e entry) string { return e.item.Published }
	slices.SortStableFunc(entries, func(a, b entry) int {
		if c := cmp.Compare(day(b), day(a)); c != 0 {
			return c
		}
		if a.media != b.media {
			if a.media {
				return 1
			}
			return -1
		}
		return b.at.Compare(a.at)
	})
	out := make([]model.NewItem, len(entries))
	for i, e := range entries {
		out[i] = e.item
	}
	return out
}

// LatestGroup is the filter an entry of what is new falls under: media,
// pub (articles, publications, issues) or news.
func LatestGroup(it model.NewItem) string {
	switch it.Kind {
	case "video", "audio":
		return "media"
	case "news":
		return "news"
	}
	return "pub"
}

// NewItemsToResults turns what is new into listing rows: a medium by its
// key, which jw show, jw media info and jw download take; anything else by
// its page on jw.org, and for jw download by what pub-media knows it by —
// an issue by its publication and issue, an article or a publication by
// its document. News are no publication's documents. The day leads the
// context.
func NewItemsToResults(items []model.NewItem) []model.Result {
	out := make([]model.Result, 0, len(items))
	for _, it := range items {
		r := model.Result{
			Kind:     it.Kind,
			Title:    it.Title,
			Context:  it.Published,
			LANK:     it.LANK,
			Duration: it.Duration,
			ImageURL: it.ImageURL,
		}
		if it.Context != "" {
			r.Context += " · " + it.Context
		}
		if it.LANK == "" {
			r.JWLink = it.URL
		}
		switch {
		case it.LANK != "" || it.Kind == "news":
		case it.Kind == "issue" && it.Symbol != "" && it.Issue != "":
			r.Pub = &model.PubKey{Pub: it.Symbol, Issue: it.Issue}
		default:
			r.DocID = it.DocID
		}
		out = append(out, r)
	}
	return out
}
