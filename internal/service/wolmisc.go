package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/model"
)

// DailyText fetches the daily text (Examining the Scriptures Daily) for date.
func (s *Service) DailyText(ctx context.Context, lng model.Language, date time.Time) (model.Article, error) {
	cfg, err := s.WOLConfig(ctx, lng)
	if err != nil {
		return model.Article{}, err
	}
	art, err := s.WOL.DailyText(ctx, cfg, date)
	if err == nil && art.DocID == 0 {
		// the page is no document of its own: the text is a day of the
		// month's issue of the year's book, which the element holding it
		// names — and which is never listed as quoting the verses it cites
		art.DocID = heldDocID(art.HTML)
	}
	return art, err
}

// heldDocID is the document the outermost element of a fragment marked with
// one says it holds; zero when none does. A link card names the document it
// leads to the same way, so links are not asked.
func heldDocID(fragment string) int {
	if !strings.Contains(fragment, "docId-") {
		return 0
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return 0
	}
	id := 0
	doc.Find(`body [class*="docId-"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if goquery.NodeName(s) != "a" {
			id = wol.DocIDOfClasses(s.AttrOr("class", ""))
		}
		return id == 0
	})
	return id
}

// Meetings fetches the overview of the week's meeting material.
func (s *Service) Meetings(ctx context.Context, lng model.Language, date time.Time) (model.Article, error) {
	cfg, err := s.WOLConfig(ctx, lng)
	if err != nil {
		return model.Article{}, err
	}
	return s.WOL.Meetings(ctx, cfg, date)
}

// MeetingPart resolves one meeting's document on the week's material page and
// reads it. which is "midweek" or "weekend".
func (s *Service) MeetingPart(ctx context.Context, lng model.Language, date time.Time, which string) (model.Article, error) {
	cfg, err := s.WOLConfig(ctx, lng)
	if err != nil {
		return model.Article{}, err
	}
	parts, err := s.WOL.MeetingParts(ctx, cfg, date)
	if err != nil {
		return model.Article{}, err
	}
	var target string
	switch which {
	case "midweek":
		target = parts.Midweek
	case "weekend":
		target = parts.Weekend
	default:
		return model.Article{}, fmt.Errorf("unknown meeting %q (want midweek or weekend)", which)
	}
	if target == "" {
		return model.Article{}, fmt.Errorf("no %s material listed for week %d/%d at %s",
			which, parts.Week, parts.Year, parts.URL)
	}
	return s.WOL.DocumentByURL(ctx, target)
}

// MeetingParts exposes the week's meeting-part links themselves.
func (s *Service) MeetingParts(ctx context.Context, lng model.Language, date time.Time) (wol.MeetingParts, error) {
	cfg, err := s.WOLConfig(ctx, lng)
	if err != nil {
		return wol.MeetingParts{}, err
	}
	return s.WOL.MeetingParts(ctx, cfg, date)
}

// WeekReading is the bible reading of a week's midweek meeting: the chapters
// the workbook heads the week with.
type WeekReading struct {
	Year  int    `json:"year"`
	Week  int    `json:"week"`
	Start string `json:"start"` // the week's Monday, YYYY-MM-DD
	// Title is the passage as the library names it: "Jeremia 45:1-46:28".
	Title string `json:"title,omitempty"`
	// Chapters are the chapters read, in order; none when the week has no
	// reading, as at an assembly or the Memorial.
	Chapters []ReadingChapter `json:"chapters"`
}

// ReadingChapter is one chapter of a week's reading.
type ReadingChapter struct {
	Book    int `json:"book"`
	Chapter int `json:"chapter"`
}

// WeekReading reads the bible reading of the ISO week containing date off its
// midweek workbook. A week whose material lists no workbook has no reading;
// one whose material is not out yet is an error.
func (s *Service) WeekReading(ctx context.Context, lng model.Language, date time.Time) (WeekReading, error) {
	year, week := date.ISOWeek()
	monday := date.AddDate(0, 0, -((int(date.Weekday()) + 6) % 7))
	out := WeekReading{Year: year, Week: week, Start: monday.Format(time.DateOnly), Chapters: []ReadingChapter{}}
	cfg, err := s.WOLConfig(ctx, lng)
	if err != nil {
		return out, err
	}
	parts, err := s.WOL.MeetingParts(ctx, cfg, date)
	if err != nil {
		return out, err
	}
	if parts.Midweek == "" {
		return out, nil
	}
	cite, err := s.WOL.ReadingCitation(ctx, parts.Midweek)
	if err != nil || cite == "" {
		return out, err
	}
	tip, err := s.WOL.Tooltip(ctx, cite)
	if err != nil {
		return out, err
	}
	out.Title = tip.Title
	for _, id := range wol.ReadingChapters(tip.URL) {
		out.Chapters = append(out.Chapters, ReadingChapter{Book: id / 1000, Chapter: id % 1000})
	}
	return out, nil
}
