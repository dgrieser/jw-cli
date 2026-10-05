package service

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/dgrieser/jw-cli/internal/api/pubmedia"
	"github.com/dgrieser/jw-cli/internal/model"
)

// The mediator's audio categories list the tracks of publications — songs,
// albums, recordings — but it is not always told of a track as soon as
// jw.org's own "Play & Download" list is, which reads the publication's files
// from pub-media. A category and a media item are read from the mediator and
// completed from pub-media: a track it has that the mediator does not list
// takes its place among the publication's other tracks.

// audioLANK is the natural key of a publication's audio track:
// pub-imc_3_AUDIO, pub-iam-4_18_AUDIO.
var audioLANK = regexp.MustCompile(`^pub-([a-z0-9-]+)_([0-9]+)_AUDIO$`)

// describedTracks is where a publication's tracks with audio descriptions are
// numbered from: 516 is 16 described. The mediator lists them in a category of
// their own, if at all, so they are left out of a category that has none.
const describedTracks = 500

// Category is a mediator category with the audio tracks pub-media knows and
// the mediator does not yet list, in the category and in each subcategory
// that comes with its media. Only a category read whole is completed: a page
// of it has no place for what is missing elsewhere.
func (s *Service) Category(ctx context.Context, lang, key string, limit, offset int) (model.Category, error) {
	cat, err := s.Mediator.Category(ctx, lang, key, limit, offset)
	if err != nil {
		return cat, err
	}
	type list struct {
		media *[]model.MediaItem
		key   string
	}
	var lists []list
	if offset == 0 && (cat.Total == 0 || len(cat.Media) >= cat.Total) {
		lists = append(lists, list{&cat.Media, cat.Key})
	}
	for i := range cat.Subcategories {
		if sub := &cat.Subcategories[i]; len(sub.Media) > 0 {
			lists = append(lists, list{&sub.Media, sub.Key})
		}
	}
	var pubs []string
	for _, l := range lists {
		for _, m := range *l.media {
			if sym, _, ok := audioTrack(m.LANK); ok && !slices.Contains(pubs, sym) {
				pubs = append(pubs, sym)
			}
		}
	}
	if len(pubs) == 0 {
		return cat, nil
	}
	tracks := s.pubTracks(ctx, lang, pubs)
	n := len(cat.Media)
	for _, l := range lists {
		*l.media = completeAudio(*l.media, tracks, l.key)
	}
	if cat.Total > 0 {
		cat.Total += len(cat.Media) - n
	}
	return cat, nil
}

// MediaItem is a mediator media item, or — for an audio track the mediator
// does not know yet — the track as pub-media describes it.
func (s *Service) MediaItem(ctx context.Context, lang, lank string) (model.MediaItem, error) {
	item, err := s.Mediator.MediaItem(ctx, lang, lank)
	if err == nil {
		return item, nil
	}
	sym, track, ok := audioTrack(lank)
	if !ok {
		return item, err
	}
	pm, perr := s.PubMedia.Links(ctx, pubmedia.Query{Pub: sym, Track: track, Formats: []string{"MP3"}, Lang: lang})
	if perr != nil {
		return item, err
	}
	for _, f := range pm.Files[lang]["MP3"] {
		if f.Track != track {
			continue
		}
		out := trackItem(sym, f, "")
		// a listed track of the same publication names the category, which
		// lists this one as Category completes it: next to its neighbours,
		// with the cover they share
		for i, t := range []int{1, track - 1} {
			if t <= 0 || t == track || (i > 0 && t == 1) {
				continue
			}
			known, err := s.Mediator.MediaItem(ctx, lang, fmt.Sprintf("pub-%s_%d_AUDIO", sym, t))
			if err != nil || known.PrimaryCategory == "" {
				continue
			}
			out.PrimaryCategory = known.PrimaryCategory
			if cat, err := s.Category(ctx, lang, known.PrimaryCategory, 0, 0); err == nil {
				if i := slices.IndexFunc(cat.Media, func(m model.MediaItem) bool { return m.LANK == lank }); i >= 0 {
					out = cat.Media[i]
				}
			}
			break
		}
		return out, nil
	}
	return item, err
}

// sharedImages is the picture every one of media shows, when there are at
// least two of them and it is one: the cover of an album rather than the art
// of one song.
func sharedImages(media []model.MediaItem) map[string]map[string]string {
	if len(media) < 2 || len(media[0].Images) == 0 {
		return nil
	}
	for _, m := range media[1:] {
		if !maps.EqualFunc(m.Images, media[0].Images, maps.Equal) {
			return nil
		}
	}
	return media[0].Images
}

// audioTrack reads the publication and track out of an audio track's key.
func audioTrack(lank string) (string, int, bool) {
	m := audioLANK.FindStringSubmatch(lank)
	if m == nil {
		return "", 0, false
	}
	track, err := strconv.Atoi(m[2])
	if err != nil || track <= 0 {
		return "", 0, false
	}
	return m[1], track, true
}

// pubTracks reads the MP3 tracks of each publication from pub-media, all at
// once. One that cannot be read is left out: the mediator's list stands.
func (s *Service) pubTracks(ctx context.Context, lang string, pubs []string) map[string][]model.PubFile {
	out := map[string][]model.PubFile{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, sym := range pubs {
		wg.Go(func() {
			pm, err := s.PubMedia.Links(ctx, pubmedia.Query{Pub: sym, Formats: []string{"MP3"}, Lang: lang})
			if err != nil {
				return
			}
			mu.Lock()
			out[sym] = pm.Files[lang]["MP3"]
			mu.Unlock()
		})
	}
	wg.Wait()
	return out
}

// completeAudio adds to media the tracks of its publications it lacks, each
// next to the track before it as the list runs: after it where the list
// counts up, before it where it counts down.
func completeAudio(media []model.MediaItem, tracks map[string][]model.PubFile, category string) []model.MediaItem {
	type pubList struct {
		have        map[int]bool
		first, last int // the tracks listed first and last
		described   bool
		listed      []model.MediaItem
	}
	pubs := map[string]*pubList{}
	for _, m := range media {
		sym, track, ok := audioTrack(m.LANK)
		if !ok {
			continue
		}
		p := pubs[sym]
		if p == nil {
			p = &pubList{have: map[int]bool{}, first: track}
			pubs[sym] = p
		}
		p.have[track] = true
		p.listed = append(p.listed, m)
		p.last = track
		p.described = p.described || track >= describedTracks
	}
	for sym, p := range pubs {
		files := slices.Clone(tracks[sym])
		slices.SortStableFunc(files, func(a, b model.PubFile) int { return a.Track - b.Track })
		down := p.first > p.last
		cover := sharedImages(p.listed)
		for _, f := range files {
			if f.Track <= 0 || p.have[f.Track] || (f.Track >= describedTracks && !p.described) {
				continue
			}
			p.have[f.Track] = true
			item := trackItem(sym, f, category)
			if item.Images == nil {
				item.Images = cover
			}
			media = insertTrack(media, sym, f.Track, down, item)
		}
	}
	return media
}

// insertTrack places item, track number track of sym, next to the listed
// track of sym closest below it, or at the end of sym's tracks when none is
// below it.
func insertTrack(media []model.MediaItem, sym string, track int, down bool, item model.MediaItem) []model.MediaItem {
	below, belowAt, firstAt, lastAt := 0, -1, -1, -1
	for i, m := range media {
		s, t, ok := audioTrack(m.LANK)
		if !ok || s != sym {
			continue
		}
		if firstAt < 0 {
			firstAt = i
		}
		lastAt = i
		if t < track && t > below {
			below, belowAt = t, i
		}
	}
	at := len(media)
	switch {
	case belowAt >= 0 && down:
		at = belowAt
	case belowAt >= 0:
		at = belowAt + 1
	case down && lastAt >= 0:
		at = lastAt + 1
	case firstAt >= 0:
		at = firstAt
	}
	return slices.Insert(media, at, item)
}

// trackItem is a pub-media track as a media item.
func trackItem(sym string, f model.PubFile, category string) model.MediaItem {
	item := model.MediaItem{
		LANK:              fmt.Sprintf("pub-%s_%d_AUDIO", sym, f.Track),
		Type:              "audio",
		Title:             f.Title,
		DurationSec:       f.Duration,
		DurationFormatted: minSec(f.Duration),
		FirstPublished:    published(f.Modified),
		PrimaryCategory:   category,
		Files: []model.MediaFile{{
			URL: f.URL, MimeType: f.MimeType, Checksum: f.Checksum, Filesize: f.Filesize, Duration: f.Duration,
		}},
	}
	if f.ImageURL != "" {
		item.Images = map[string]map[string]string{"sqr": {"lg": f.ImageURL}}
	}
	return item
}

// minSec writes a duration as the mediator does: 2m 57s, 1h 2m 3s.
func minSec(sec float64) string {
	if sec <= 0 {
		return ""
	}
	t := int(sec + 0.5)
	if t >= 3600 {
		return fmt.Sprintf("%dh %dm %ds", t/3600, t/60%60, t%60)
	}
	return fmt.Sprintf("%dm %ds", t/60, t%60)
}

// published writes pub-media's "2026-10-01 20:46:48" as the mediator writes
// a date.
func published(modified string) string {
	if modified == "" {
		return ""
	}
	return strings.Replace(modified, " ", "T", 1) + "Z"
}
