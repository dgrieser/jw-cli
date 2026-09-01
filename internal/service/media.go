package service

import (
	"fmt"

	"github.com/dgrieser/jw-cli/internal/model"
)

// CategoriesToResults turns mediator categories into listing rows.
func CategoriesToResults(cats []model.Category) []model.Result {
	var out []model.Result
	for _, c := range cats {
		out = append(out, model.Result{
			Kind:        "category",
			Title:       c.Name,
			Snippet:     c.Description,
			Context:     c.Key,
			CategoryKey: c.Key,
		})
	}
	return out
}

// MediaToResults turns mediator media items into listing rows.
func MediaToResults(media []model.MediaItem) []model.Result {
	var out []model.Result
	for _, m := range media {
		out = append(out, model.Result{
			Kind:     m.Type,
			Title:    m.Title,
			LANK:     m.LANK,
			Duration: m.DurationFormatted,
			ImageURL: BestImage(m.Images),
		})
	}
	return out
}

// BestImage picks the most useful rendition out of a mediator image map.
func BestImage(images map[string]map[string]string) string {
	for _, typ := range []string{"lss", "sqr", "wss", "pnr", "cvr"} {
		if sizes, ok := images[typ]; ok {
			for _, size := range []string{"lg", "xl", "md", "sm", "xs"} {
				if u := sizes[size]; u != "" {
					return u
				}
			}
		}
	}
	return ""
}

// PickBest is the largest rendition of a media item: the highest frame, and of
// equal frames the largest file.
func PickBest(mi model.MediaItem) (model.MediaFile, error) {
	if len(mi.Files) == 0 {
		return model.MediaFile{}, fmt.Errorf("no files for %s", mi.Title)
	}
	best := mi.Files[0]
	for _, f := range mi.Files[1:] {
		if f.FrameHeight > best.FrameHeight || (f.FrameHeight == best.FrameHeight && f.Filesize > best.Filesize) {
			best = f
		}
	}
	return best, nil
}
