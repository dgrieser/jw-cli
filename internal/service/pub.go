package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/dgrieser/jw-cli/internal/api/pubmedia"
	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/model"
)

// LibraryLocale is the language whose library paths name a page of the
// publication tree in every language: an address stays the same when the
// language changes.
const LibraryLocale = "en"

// Library reads one page of the library's publication tree in lng: kind is
// wol.LibraryKind or wol.PublicationKind, and an empty library path is the
// list of categories at the top. Library paths, the one asked for and those
// the page leads to, are English (all-publications/books) whatever lng is.
func (s *Service) Library(ctx context.Context, lng model.Language, kind, path string) (wol.LibraryPage, error) {
	cfg, err := s.WOLConfig(ctx, lng)
	if err != nil {
		return wol.LibraryPage{}, err
	}
	canon, err := s.WOL.ConfigFor(ctx, LibraryLocale)
	if err != nil {
		return wol.LibraryPage{}, err
	}
	return s.WOL.CanonicalLibrary(ctx, cfg, canon, kind, path)
}

// LibraryFiles is the downloadable files of the publication a library page
// shows, in every format the publication media API carries. A page that is no
// one publication has none.
func (s *Service) LibraryFiles(ctx context.Context, lng model.Language, page wol.LibraryPage) (model.PubMedia, error) {
	if page.Symbol == "" {
		return model.PubMedia{}, fmt.Errorf("%w: the page is no publication", pubmedia.ErrNotFound)
	}
	return s.PubMedia.Links(ctx, pubmedia.Query{Pub: page.Symbol, Issue: page.Issue, Lang: lng.Symbol})
}

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
				if f.Label != "" && f.Label != "0p" {
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
