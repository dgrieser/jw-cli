package wol

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/htmlx"
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
		// this page is assembled by hand rather than by parseDocument, so the
		// verse and image indexes have to be collected here to keep --refs and
		// --images working
		art.Images = append(art.Images, htmlx.Images(pruned, c.hc.Base.WOL)...)
		art.ScriptureRefs = append(art.ScriptureRefs, htmlx.ScriptureRefs(pruned, c.hc.Base.WOL)...)
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

// meetingsURL is the material page for the ISO week containing date.
func (c *Client) meetingsURL(cfg Config, date time.Time) (u string, week, year int) {
	year, week = date.ISOWeek()
	return c.url(cfg, "meetings", fmt.Sprintf("/%d/%d", year, week)), week, year
}

// MeetingParts are the two documents a meeting-material page links to.
type MeetingParts struct {
	Week int    `json:"week"`
	Year int    `json:"year"`
	URL  string `json:"url"` // the material page the links came from
	// Midweek and Weekend are wol document URLs, empty when the week does not
	// list that meeting.
	Midweek string `json:"midweek,omitempty"`
	Weekend string `json:"weekend,omitempty"`
}

// Publication symbols wol tags each meeting's link card with. Unlike the section
// headings ("Leben und Dienst", "Watchtower Study") these do not change with the
// language, so they are what the two meetings are recognized by.
const (
	pubWorkbook   = "pub-mwb" // Life and Ministry Meeting Workbook
	pubWatchtower = "pub-w"   // Watchtower, study edition
)

// MeetingParts locates the midweek and weekend documents for the ISO week
// containing date. The page opens with one link card per meeting and repeats
// both further down among the publications used at the meetings; only the
// document links (/wol/d/) are of interest, the repeats point at library pages.
func (c *Client) MeetingParts(ctx context.Context, cfg Config, date time.Time) (MeetingParts, error) {
	u, week, year := c.meetingsURL(cfg, date)
	doc, err := c.hc.GetHTML(ctx, u)
	if err != nil {
		return MeetingParts{}, err
	}
	parts := MeetingParts{Week: week, Year: year, URL: u}
	doc.Find("a.cardContainer").EachWithBreak(func(_ int, card *goquery.Selection) bool {
		href, _ := card.Attr("href")
		if href == "" || !strings.Contains(href, "/wol/d/") {
			return true
		}
		switch {
		case parts.Midweek == "" && card.HasClass(pubWorkbook):
			parts.Midweek = absURL(c.hc.Base.WOL, href)
		case parts.Weekend == "" && card.HasClass(pubWatchtower):
			parts.Weekend = absURL(c.hc.Base.WOL, href)
		}
		return parts.Midweek == "" || parts.Weekend == ""
	})
	if parts.Midweek == "" && parts.Weekend == "" {
		return parts, fmt.Errorf("no meeting material found at %s", u)
	}
	return parts, nil
}

// Meetings fetches the meeting-material page (midweek workbook + Watchtower
// study) for the ISO week containing date.
func (c *Client) Meetings(ctx context.Context, cfg Config, date time.Time) (model.Article, error) {
	u, week, year := c.meetingsURL(cfg, date)
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
