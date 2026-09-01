package service

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dgrieser/jw-cli/internal/model"
)

// SplitFormats normalizes a repeatable, comma-separable format flag into
// upper-case format codes.
func SplitFormats(in []string) []string {
	var out []string
	for _, f := range in {
		for part := range strings.SplitSeq(f, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, strings.ToUpper(part))
			}
		}
	}
	return out
}

// PubFilesToResults flattens the lang→format→files map into a stable listing.
func PubFilesToResults(pm model.PubMedia) []model.Result {
	var items []model.Result
	langs := make([]string, 0, len(pm.Files))
	for sym := range pm.Files {
		langs = append(langs, sym)
	}
	sort.Strings(langs)
	for _, sym := range langs {
		formats := make([]string, 0, len(pm.Files[sym]))
		for f := range pm.Files[sym] {
			formats = append(formats, f)
		}
		sort.Strings(formats)
		for _, format := range formats {
			for _, f := range pm.Files[sym][format] {
				title := f.Title
				if title == "" {
					title = pm.PubName
				}
				ctx := format
				if f.Label != "" {
					ctx += " " + f.Label
				}
				if len(langs) > 1 {
					ctx += ", " + sym
				}
				if f.Track > 0 {
					ctx += fmt.Sprintf(", track %d", f.Track)
				}
				items = append(items, model.Result{
					Kind:     "file",
					Title:    title,
					Context:  ctx,
					FileURL:  f.URL,
					Checksum: f.Checksum,
					Filesize: f.Filesize,
					DocID:    f.DocID,
					Pub:      &model.PubKey{Pub: pm.Pub, Issue: pm.Issue, BookNum: f.BookNum, Track: f.Track},
				})
			}
		}
	}
	return items
}
