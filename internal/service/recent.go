package service

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dgrieser/jw-cli/internal/api/pubmedia"
	"github.com/dgrieser/jw-cli/internal/model"
)

// The mediator's category of the latest videos is jw.org's pick of them: it
// leaves out much of what came out lately — the audio, the talks of the
// morning worship, the songs. What the media library published since a day
// is found by walking its whole category tree instead.

// mediaLibraries are the roots of the category tree the media are in.
var mediaLibraries = []string{"VideoOnDemand", "Audio"}

// describedLibrary is the category of the videos with audio descriptions:
// the library's videos again, described, which a list of what is new would
// have twice.
const describedLibrary = "VODAudioDescriptions"

// maxCategoryDepth bounds the walk down the category tree, which is a few
// levels deep.
const maxCategoryDepth = 4

// RecentMedia lists the media of every category published since since,
// newest first, each once. A category that cannot be read leaves its media
// out, and the error says so; it is an error only when no category could be
// read.
func (s *Service) RecentMedia(ctx context.Context, lang string, since time.Time) ([]model.MediaItem, error) {
	var (
		mu    sync.Mutex
		found = map[string]model.MediaItem{}
		errs  []error
		wg    sync.WaitGroup
		read  bool
	)
	var walk func(key string, depth int)
	walk = func(key string, depth int) {
		cat, err := s.Mediator.Category(ctx, lang, key, 0, 0)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			// a category the tree names but the mediator has not (yet) is
			// no failure
			if !errors.Is(err, pubmedia.ErrNotFound) {
				errs = append(errs, err)
			}
			return
		}
		read = true
		for _, c := range append([]model.Category{cat}, cat.Subcategories...) {
			for _, m := range c.Media {
				if _, ok := found[m.LANK]; ok || m.LANK == "" || !Published(m).After(since) {
					continue
				}
				found[m.LANK] = m
			}
		}
		if depth >= maxCategoryDepth {
			return
		}
		for _, sub := range cat.Subcategories {
			if sub.Type == "container" && sub.Key != describedLibrary {
				wg.Go(func() { walk(sub.Key, depth+1) })
			}
		}
	}
	for _, key := range mediaLibraries {
		wg.Go(func() { walk(key, 0) })
	}
	wg.Wait()
	if !read && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	// in an order of their own first, the walk's being whichever category
	// answered first
	media := slices.SortedFunc(maps.Values(found), func(a, b model.MediaItem) int {
		return cmp.Compare(a.LANK, b.LANK)
	})
	NewestFirst(media)
	return media, errors.Join(errs...)
}

// Published is when a media item was first published; the zero time when
// the mediator does not say.
func Published(m model.MediaItem) time.Time {
	t, err := time.Parse(time.RFC3339, m.FirstPublished)
	if err != nil {
		return time.Time{}
	}
	return t
}

// NewestFirst sorts media by when they were first published, newest first;
// those published at once, and those without a date, keep their order.
func NewestFirst(media []model.MediaItem) {
	slices.SortStableFunc(media, func(a, b model.MediaItem) int {
		return Published(b).Compare(Published(a))
	})
}

// MediaStem is a media item's natural key without saying whether it is the
// video or the recording: the video of a talk and its audio share it
// (pub-jwb-142_1_VIDEO, pub-jwb-142_1_AUDIO).
func MediaStem(lank string) string {
	for _, suffix := range []string{"_VIDEO", "_AUDIO"} {
		if stem, ok := strings.CutSuffix(lank, suffix); ok {
			return stem
		}
	}
	return lank
}
