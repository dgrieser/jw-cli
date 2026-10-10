package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/subtitles"
)

// ErrNoBookDoc is a book document the library does not list for that book in
// that edition: Genesis has no media gallery, Rbi8 no introductions.
var ErrNoBookDoc = errors.New("the library lists no such document for this book")

// BookLink is the document of one kind a book's page lists below its chapters.
func (s *Service) BookLink(ctx context.Context, lng model.Language, edition string, book int, kinds ...string) (wol.BookLink, error) {
	nav, err := s.BookNav(ctx, lng, edition, book)
	if err != nil {
		return wol.BookLink{}, err
	}
	for _, kind := range kinds {
		for _, l := range nav.Links {
			if l.Kind == kind {
				return l, nil
			}
		}
	}
	return wol.BookLink{}, fmt.Errorf("%s (%s): no %s: %w",
		s.BookTable(ctx, lng).Name(book), edition, strings.Join(kinds, " or "), ErrNoBookDoc)
}

// BookIntro is a bible book's introduction: the video, the facts about the
// book's writing, and the noteworthy facts. The facts listed under the video
// are completed from the table of the books of the Bible, which every book has
// a row in, so a book whose introduction is the video alone still says who
// wrote it, where and when. The noteworthy facts are the text written under
// the video; where there is none, the video's own words stand in for it.
func (s *Service) BookIntro(ctx context.Context, lng model.Language, edition string, book int) (model.BookIntro, error) {
	link, err := s.BookLink(ctx, lng, edition, book, wol.BookIntroduction)
	if err != nil {
		return model.BookIntro{}, err
	}
	intro, err := s.WOL.Introduction(ctx, link.URL)
	if err != nil {
		return model.BookIntro{}, err
	}
	intro.Book = book
	if intro.Title == "" {
		intro.Title = link.Title
	}
	// best effort: the introduction stands without the table
	if cfg, err := s.WOLConfig(ctx, lng); err == nil {
		if table, err := s.WOL.BookFacts(ctx, cfg); err == nil {
			intro.Facts = MergeFacts(intro.Facts, table[book])
		}
	}
	if intro.NotesHTML == "" && intro.Video != nil {
		for _, f := range intro.Video.Files {
			if f.SubtitlesURL == "" {
				continue
			}
			if vtt, err := s.HTTP.GetText(ctx, f.SubtitlesURL, nil); err == nil {
				intro.Transcript = subtitles.ParseVTT(vtt)
			}
			break
		}
	}
	return intro, nil
}

// MergeFacts completes the facts an introduction lists with those of the
// table it does not: the introduction's own wording first, then each column of
// the table that names something else.
func MergeFacts(listed, table []model.Fact) []model.Fact {
	out := append([]model.Fact(nil), listed...)
	have := map[string]bool{}
	for _, f := range listed {
		have[wol.FactKey(f.Label)] = true
	}
	for _, f := range table {
		if key := wol.FactKey(f.Label); !have[key] {
			have[key] = true
			out = append(out, f)
		}
	}
	return out
}

// BookGallery is a bible book's media gallery, chapter by chapter.
func (s *Service) BookGallery(ctx context.Context, lng model.Language, edition string, book int) (model.BookGallery, error) {
	link, err := s.BookLink(ctx, lng, edition, book, wol.BookGallery)
	if err != nil {
		return model.BookGallery{}, err
	}
	g, err := s.WOL.Gallery(ctx, link.URL)
	if err != nil {
		return model.BookGallery{}, err
	}
	if g.Title == "" {
		g.Title = link.Title
	}
	return g, nil
}

// GalleryEntry is one item of a book's media gallery: the picture with its
// caption, description and rights line, or the video it plays.
type GalleryEntry struct {
	model.MediaAsset
	Video *model.Video `json:"video,omitempty"`
}

// BookGalleryItem reads one item of a book's media gallery by its id, the last
// segment of its gallery address. The id is looked up under the book's own
// gallery, so only an item of the library can be asked for.
func (s *Service) BookGalleryItem(ctx context.Context, lng model.Language, edition string, book int, id string) (GalleryEntry, error) {
	if _, err := strconv.ParseUint(id, 10, 64); err != nil || id == "" {
		return GalleryEntry{}, fmt.Errorf("invalid gallery item %q", id)
	}
	link, err := s.BookLink(ctx, lng, edition, book, wol.BookGallery)
	if err != nil {
		return GalleryEntry{}, err
	}
	asset, err := s.WOL.GalleryItem(ctx, link.URL+"/"+id)
	if err != nil {
		return GalleryEntry{}, err
	}
	e := GalleryEntry{MediaAsset: asset}
	if asset.VideoSource != "" {
		if v, err := s.WOL.Video(ctx, asset.VideoSource); err == nil {
			e.Video = &v
		}
	}
	return e, nil
}

// ParseBook resolves a bible book named the way a reference names it — "Mt",
// "Matthäus", "matthew" — or by its number.
func (s *Service) ParseBook(ctx context.Context, lng model.Language, input string) (int, error) {
	input = strings.TrimSpace(input)
	if n, err := strconv.Atoi(input); err == nil {
		if n < 1 || n > 66 {
			return 0, fmt.Errorf("invalid book %d (want 1-66)", n)
		}
		return n, nil
	}
	if book, ok := s.BookTable(ctx, lng).Lookup(input); ok {
		return book, nil
	}
	return 0, fmt.Errorf("unknown bible book %q (try a name, an abbreviation, or a number 1-66)", input)
}

// BookOutline is a book's outline of contents — or the overview of the Gospels
// and Acts — under the name the library gives it.
type BookOutline struct {
	Book  int    `json:"book"`
	Title string `json:"title"`
	// Kind is wol.BookOutline, whose verses count by chapter, or
	// wol.BookOverview, whose headings name their chapters themselves.
	Kind  string              `json:"kind"`
	URL   string              `json:"url"`
	Items []model.OutlineItem `json:"items"`
}

// ByChapter reports whether the headings are grouped under their chapters.
func (o BookOutline) ByChapter() bool { return o.Kind == wol.BookOutline }

// BookOutlineDoc reads a book's outline or overview as one document.
func (s *Service) BookOutlineDoc(ctx context.Context, lng model.Language, edition string, book int) (BookOutline, error) {
	link, err := s.BookLink(ctx, lng, edition, book, wol.BookOutline, wol.BookOverview)
	if err != nil {
		return BookOutline{}, err
	}
	cfg, err := s.WOLConfig(ctx, lng)
	if err != nil {
		return BookOutline{}, err
	}
	items, err := s.WOL.Outline(ctx, cfg, edition, book)
	if err != nil {
		return BookOutline{}, err
	}
	if len(items) == 0 {
		return BookOutline{}, fmt.Errorf("no headings in %s at %s (page layout changed?)", link.Title, link.URL)
	}
	return BookOutline{Book: book, Title: link.Title, Kind: link.Kind, URL: link.URL, Items: items}, nil
}
