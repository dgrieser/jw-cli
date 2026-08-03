package wol

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
)

// DailyText fetches the "Examining the Scriptures Daily" entry for a date.
// wol expects month/day without zero padding.
func (c *Client) DailyText(ctx context.Context, cfg Config, date time.Time) (model.Article, error) {
	u := c.url(cfg, "dt", fmt.Sprintf("/%d/%d/%d", date.Year(), int(date.Month()), date.Day()))
	doc, err := c.hc.GetHTML(ctx, u)
	if err != nil {
		return model.Article{}, err
	}
	// the page carries no title of its own; write one in the library's language
	txt := i18n.TextFor(cfg.Locale)
	art := model.Article{URL: u, Title: fmt.Sprintf(txt.DailyTextTitle, txt.Date(date))}
	// The dt page bundles today's material: the daily text (pub-es) plus the
	// meeting publications, which `jw meetings` already covers. Prefer pub-es.
	items := doc.Find(".todayItem.pub-es")
	if items.Length() == 0 {
		items = doc.Find(".todayItem")
	}
	if items.Length() == 0 {
		items = doc.Find("#article, article").First()
	}
	var b, text strings.Builder
	items.Each(func(_ int, s *goquery.Selection) {
		// drop navigation and modal chrome: the #article fallback wraps it,
		// and a chrome-only match must not read as content
		pruned := s.Clone()
		pruned.Find(todayChrome).Remove()
		if html, err := goquery.OuterHtml(pruned); err == nil {
			b.WriteString(html)
		}
		text.WriteString(pruned.Text())
	})
	art.HTML = strings.TrimSpace(b.String())
	if strings.TrimSpace(text.String()) == "" {
		return art, fmt.Errorf("no daily text found at %s (this language may not publish it for that date; try --lang E)", u)
	}
	return art, nil
}

// todayChrome lists the non-content elements wol nests inside the dt/meetings
// article container. Deliberately no bare "nav": the meetings page keeps its
// material lists inside <nav> elements.
const todayChrome = "#todayNav, .forwardBackNavControls, #galleryModalContainer, script, style"

// Meetings fetches the meeting-material page (midweek workbook + Watchtower
// study) for the ISO week containing date.
func (c *Client) Meetings(ctx context.Context, cfg Config, date time.Time) (model.Article, error) {
	year, week := date.ISOWeek()
	u := c.url(cfg, "meetings", fmt.Sprintf("/%d/%d", year, week))
	doc, err := c.hc.GetHTML(ctx, u)
	if err != nil {
		return model.Article{}, err
	}
	art := parseDocument(doc, c.hc.Base.WOL)
	art.URL = u
	if art.Title == "" {
		art.Title = fmt.Sprintf(i18n.TextFor(cfg.Locale).MeetingsTitle, week, year)
	}
	if art.HTML == "" {
		return art, fmt.Errorf("no meeting content found at %s", u)
	}
	return art, nil
}
