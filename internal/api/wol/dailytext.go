package wol

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

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
	art := model.Article{URL: u, Title: "Daily text, " + date.Format("Monday, January 2, 2006")}
	items := doc.Find(".todayItem")
	if items.Length() == 0 {
		items = doc.Find("#article, article").First()
	}
	var b strings.Builder
	items.Each(func(_ int, s *goquery.Selection) {
		if html, err := goquery.OuterHtml(s); err == nil {
			b.WriteString(html)
		}
	})
	art.HTML = strings.TrimSpace(b.String())
	if art.HTML == "" {
		return art, fmt.Errorf("no daily text found at %s", u)
	}
	return art, nil
}

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
		art.Title = fmt.Sprintf("Meetings, week %d/%d", week, year)
	}
	if art.HTML == "" {
		return art, fmt.Errorf("no meeting content found at %s", u)
	}
	return art, nil
}
