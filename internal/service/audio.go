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
	"github.com/dgrieser/jw-cli/internal/i18n"
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
// numbered from: 516 is 16 described. The mediator lists none of them; a
// category whose publications have some is followed by a category of its own
// for them, keyed as the category with describedKey after it.
const (
	describedTracks = 500
	describedKey    = "-AudioDescriptions"
)

// Category is a mediator category with the audio tracks pub-media knows and
// the mediator does not yet list, in the category and in each subcategory
// that comes with its media. Only a category read whole is completed: a page
// of it has no place for what is missing elsewhere. The tracks with audio
// descriptions of a list's publications make a category of their own: a
// subcategory of the category read, and one after each subcategory.
func (s *Service) Category(ctx context.Context, lang, key string, limit, offset int) (model.Category, error) {
	if base, ok := strings.CutSuffix(key, describedKey); ok && base != "" {
		return s.describedCategory(ctx, lang, base, key)
	}
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
	tracks, locale := s.pubTracks(ctx, lang, pubs)
	n := len(cat.Media)
	described := map[string][]model.MediaItem{}
	for _, l := range lists {
		*l.media, described[l.key] = completeAudio(*l.media, tracks, l.key)
	}
	if cat.Total > 0 {
		cat.Total += len(cat.Media) - n
	}
	name := i18n.TextFor(locale).MediaDescribed
	var subs []model.Category
	for _, sub := range cat.Subcategories {
		subs = append(subs, sub)
		if media := described[sub.Key]; len(media) > 0 {
			subs = append(subs, describedSub(sub.Key, sub.Name, name, media, &model.CategoryRef{Key: cat.Key, Name: cat.Name}))
		}
	}
	if media := described[cat.Key]; len(media) > 0 {
		subs = append(subs, describedSub(cat.Key, cat.Name, name, media, &model.CategoryRef{Key: cat.Key, Name: cat.Name}))
	}
	cat.Subcategories = subs
	return cat, nil
}

// describedSub is the category of the tracks with audio descriptions of the
// category key.
func describedSub(key, catName, format string, media []model.MediaItem, parent *model.CategoryRef) model.Category {
	return model.Category{
		Key:    key + describedKey,
		Name:   fmt.Sprintf(format, catName),
		Type:   "ondemand",
		Media:  media,
		Total:  len(media),
		Parent: parent,
	}
}

// describedCategory is the category of the tracks with audio descriptions of
// the category base, under it.
func (s *Service) describedCategory(ctx context.Context, lang, base, key string) (model.Category, error) {
	cat, err := s.Category(ctx, lang, base, 0, 0)
	if err != nil {
		return cat, err
	}
	for _, sub := range cat.Subcategories {
		if sub.Key == key {
			sub.Parent = &model.CategoryRef{Key: cat.Key, Name: cat.Name}
			return sub, nil
		}
	}
	return model.Category{}, fmt.Errorf("category %q not found", key)
}

// CategoryInfo is a category's name and parent, as the mediator's
// CategoryInfo, for the categories of tracks with audio descriptions too.
func (s *Service) CategoryInfo(ctx context.Context, lang, key string) (model.Category, error) {
	if base, ok := strings.CutSuffix(key, describedKey); ok && base != "" {
		cat, err := s.describedCategory(ctx, lang, base, key)
		cat.Subcategories, cat.Media = nil, nil
		return cat, err
	}
	return s.Mediator.CategoryInfo(ctx, lang, key)
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
				for _, list := range append([]model.Category{cat}, cat.Subcategories...) {
					if i := slices.IndexFunc(list.Media, func(m model.MediaItem) bool { return m.LANK == lank }); i >= 0 {
						out = list.Media[i]
						break
					}
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
// once, and the locale of lang as pub-media names it. One that cannot be read
// is left out: the mediator's list stands.
func (s *Service) pubTracks(ctx context.Context, lang string, pubs []string) (map[string][]model.PubFile, string) {
	out := map[string][]model.PubFile{}
	locale := ""
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
			if l, ok := pm.Languages[lang]; ok && l.Locale != "" {
				locale = l.Locale
			}
			mu.Unlock()
		})
	}
	wg.Wait()
	return out, locale
}

// completeAudio adds to media the tracks of its publications it lacks, each
// next to the track before it as the list runs: after it where the list
// counts up, before it where it counts down. The tracks with audio
// descriptions of a publication the list has none of are not added; they are
// returned on their own, publication by publication as the list has them,
// each counting up.
func completeAudio(media []model.MediaItem, tracks map[string][]model.PubFile, category string) ([]model.MediaItem, []model.MediaItem) {
	type pubList struct {
		have        map[int]bool
		first, last int // the tracks listed first and last
		described   bool
		listed      []model.MediaItem
	}
	pubs := map[string]*pubList{}
	var order []string
	for _, m := range media {
		sym, track, ok := audioTrack(m.LANK)
		if !ok {
			continue
		}
		p := pubs[sym]
		if p == nil {
			p = &pubList{have: map[int]bool{}, first: track}
			pubs[sym] = p
			order = append(order, sym)
		}
		p.have[track] = true
		p.listed = append(p.listed, m)
		p.last = track
		p.described = p.described || track >= describedTracks
	}
	var described []model.MediaItem
	for _, sym := range order {
		p := pubs[sym]
		files := slices.Clone(tracks[sym])
		slices.SortStableFunc(files, func(a, b model.PubFile) int { return a.Track - b.Track })
		down := p.first > p.last
		cover := sharedImages(p.listed)
		for _, f := range files {
			if f.Track <= 0 || p.have[f.Track] {
				continue
			}
			if f.Track >= describedTracks && !p.described {
				p.have[f.Track] = true
				described = append(described, trackItem(sym, f, category+describedKey))
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
	// a described track shows the picture of the song it describes
	for i, d := range described {
		sym, track, _ := audioTrack(d.LANK)
		if d.Images != nil {
			continue
		}
		plain := fmt.Sprintf("pub-%s_%d_AUDIO", sym, track-describedTracks)
		if j := slices.IndexFunc(media, func(m model.MediaItem) bool { return m.LANK == plain }); j >= 0 {
			described[i].Images = media[j].Images
		} else {
			described[i].Images = sharedImages(pubs[sym].listed)
		}
	}
	return media, described
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
