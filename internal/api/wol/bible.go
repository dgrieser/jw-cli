package wol

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/model"
)

// Bible editions available on wol (publication symbols for the /b/ command).
var BibleEditions = []string{"nwtsty", "nwt", "bi12", "bi10", "bi22", "by", "int", "rh"}

// ChapterDoc wraps a fetched bible chapter page. All selectors that mirror
// the live wol markup are grouped in the sel* constants below so layout
// drift is cheap to fix.
type ChapterDoc struct {
	doc     *goquery.Document
	base    string
	Book    int
	Chapter int
	URL     string
}

const (
	selVerse         = "span.v"                       // verse text segments, id=v{b}-{c}-{v}-{seg}
	selStudySection  = "#studyDiscover div.section"   // per-verse study material, data-key={b}-{c}-{v}
	selStudyNote     = ".studyNoteGroup li.item p"    // one study note paragraph
	selMarginalItem  = ".group.marginal li.item"      // one cross-reference group
	selMarginalCite  = ".marginal.title"              // inline citation list
	selMediaItem     = ".group.media li.item"         // one media entry
	selMediaImg      = "img.studyItemMedia"           // its thumbnail
	selMediaLink     = "a.directLinkItem"             // finder deep link
	selResearchItem  = ".group.index li.item"         // research guide entry
	selFootnoteItem  = ".group.footnote li.item" // footnotes (best effort)
	selSectionTitle  = "h3.title"
)

// Chapter fetches one bible chapter page (with the study pane inlined).
func (c *Client) Chapter(ctx context.Context, cfg Config, edition string, book, chapter int) (*ChapterDoc, error) {
	if edition == "" {
		edition = "nwtsty"
	}
	u := c.url(cfg, "b", fmt.Sprintf("/%s/%d/%d", edition, book, chapter))
	doc, err := c.hc.GetHTML(ctx, u)
	if err != nil {
		return nil, err
	}
	cd := &ChapterDoc{doc: doc, base: c.hc.Base.WOL, Book: book, Chapter: chapter, URL: u}
	if cd.doc.Find(selVerse).Length() == 0 {
		return nil, fmt.Errorf("no verses found at %s (chapter missing in %s, or page layout changed)", u, edition)
	}
	return cd, nil
}

var verseID = regexp.MustCompile(`^v(\d+)-(\d+)-(\d+)-\d+$`)

// Verses returns verses from..to (inclusive); 0,0 means the whole chapter.
// Multi-segment verses are concatenated.
func (d *ChapterDoc) Verses(from, to int) ([]model.Verse, error) {
	if from == 0 {
		from, to = 1, 999
	}
	if to == 0 {
		to = from
	}
	order := []int{}
	byVerse := map[int]*strings.Builder{}
	d.doc.Find(selVerse).Each(func(_ int, s *goquery.Selection) {
		id, ok := s.Attr("id")
		if !ok {
			return
		}
		m := verseID.FindStringSubmatch(id)
		if m == nil {
			return
		}
		book, _ := strconv.Atoi(m[1])
		ch, _ := strconv.Atoi(m[2])
		v, _ := strconv.Atoi(m[3])
		if book != d.Book || ch != d.Chapter || v < from || v > to {
			return
		}
		html, err := s.Html()
		if err != nil {
			return
		}
		b, seen := byVerse[v]
		if !seen {
			b = &strings.Builder{}
			byVerse[v] = b
			order = append(order, v)
		}
		b.WriteString(html)
	})
	if len(order) == 0 {
		return nil, fmt.Errorf("verses %d-%d not found in chapter %d", from, to, d.Chapter)
	}
	verses := make([]model.Verse, 0, len(order))
	for _, v := range order {
		verses = append(verses, model.Verse{
			ID:   d.Book*1_000_000 + d.Chapter*1_000 + v,
			HTML: byVerse[v].String(),
		})
	}
	return verses, nil
}

// StudySection extracts everything the study pane attaches to one verse.
func (d *ChapterDoc) StudySection(verse int) (model.StudySection, bool) {
	key := fmt.Sprintf("%d-%d-%d", d.Book, d.Chapter, verse)
	sec := d.doc.Find(selStudySection).FilterFunction(func(_ int, s *goquery.Selection) bool {
		k, _ := s.Attr("data-key")
		return k == key
	}).First()
	if sec.Length() == 0 {
		return model.StudySection{}, false
	}
	out := model.StudySection{
		Verse: cleanSpace(sec.Find(selSectionTitle).First().Text()),
	}

	sec.Find(selStudyNote).Each(func(_ int, p *goquery.Selection) {
		html, err := p.Html()
		if err != nil || strings.TrimSpace(p.Text()) == "" {
			return
		}
		out.Notes = append(out.Notes, model.StudyNote{
			Lemma: cleanSpace(p.Find("strong").First().Text()),
			HTML:  html,
		})
	})

	sec.Find(selMarginalItem).Each(func(_ int, li *goquery.Selection) {
		cite := cleanSpace(li.Find(selMarginalCite).First().Text())
		if cite == "" {
			cite = cleanSpace(li.Find(".header").First().Text())
		}
		src, _ := li.Attr("data-src")
		if cite == "" && src == "" {
			return
		}
		out.XRefs = append(out.XRefs, model.CrossRef{Citation: cite, SourcePath: absURL(d.base, src)})
	})

	sec.Find(selMediaItem).Each(func(_ int, li *goquery.Selection) {
		asset := model.MediaAsset{Caption: cleanSpace(li.Find(".caption").First().Text())}
		if img := li.Find(selMediaImg).First(); img.Length() > 0 {
			src, _ := img.Attr("src")
			asset.URL = absURL(d.base, src)
			asset.Alt, _ = img.Attr("alt")
		}
		if a := li.Find(selMediaLink).First(); a.Length() > 0 {
			href, _ := a.Attr("href")
			asset.FinderLink = absURL(d.base, href)
		}
		if asset.URL == "" && asset.FinderLink == "" {
			return
		}
		out.Media = append(out.Media, asset)
	})

	sec.Find(selResearchItem).Each(func(_ int, li *goquery.Selection) {
		// media entries live in their own group; skip anything without a link
		a := li.Find("a").First()
		if a.Length() == 0 {
			return
		}
		href, _ := a.Attr("href")
		item := model.ResearchItem{
			Title:  cleanSpace(a.Text()),
			Source: cleanSpace(li.Find(".subtitle").First().Text()),
		}
		if strings.Contains(href, "/pc/") {
			item.PCPath = absURL(d.base, href)
		} else {
			item.ArticleURL = absURL(d.base, href)
		}
		if item.Title == "" && item.PCPath == "" && item.ArticleURL == "" {
			return
		}
		out.Research = append(out.Research, item)
	})

	sec.Find(selFootnoteItem).Each(func(_ int, li *goquery.Selection) {
		if txt := cleanSpace(li.Text()); txt != "" {
			out.Footnotes = append(out.Footnotes, txt)
		}
	})

	return out, true
}

// MarginalReference fetches the full text of one cross-reference group (the
// lazy-loaded data-src of a marginal item).
func (c *Client) MarginalReference(ctx context.Context, srcURL string) (string, error) {
	doc, err := c.hc.GetHTML(ctx, srcURL)
	if err != nil {
		return "", err
	}
	html, err := doc.Find("body").Html()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(html), nil
}

// Tooltip fetches a wol bc/pc citation endpoint as JSON (publication
// excerpts, verse popups).
func (c *Client) Tooltip(ctx context.Context, tcURL string) (model.Tooltip, error) {
	var resp struct {
		Items []struct {
			Title            string `json:"title"`
			Caption          string `json:"caption"`
			Content          string `json:"content"`
			URL              string `json:"url"`
			ImageURL         string `json:"imageUrl"`
			PublicationTitle string `json:"publicationTitle"`
		} `json:"items"`
	}
	if err := c.hc.GetJSON(ctx, tcURL, xhrHeaders(), &resp); err != nil {
		return model.Tooltip{}, err
	}
	if len(resp.Items) == 0 {
		return model.Tooltip{}, fmt.Errorf("no citation content at %s", tcURL)
	}
	it := resp.Items[0]
	return model.Tooltip{
		Title:            it.Title,
		Caption:          it.Caption,
		ContentHTML:      it.Content,
		URL:              absURL(c.hc.Base.WOL, it.URL),
		ImageURL:         absURL(c.hc.Base.WOL, it.ImageURL),
		PublicationTitle: it.PublicationTitle,
	}, nil
}

// LocalizedBookNames extracts the localized bible book names from the bible
// navigation page (best effort; cached for 30 days).
func (c *Client) LocalizedBookNames(ctx context.Context, cfg Config) (map[int][]string, error) {
	key := "books-" + cfg.Locale
	var cached map[int][]string
	if c.cache.Get(key, 30*24*time.Hour, &cached) && len(cached) > 0 {
		return cached, nil
	}
	doc, err := c.hc.GetHTML(ctx, c.url(cfg, "binav", ""))
	if err != nil {
		return nil, err
	}
	names := map[int][]string{}
	bookHref := regexp.MustCompile(`/(\d{1,2})$`)
	doc.Find("a").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		if !strings.Contains(href, "/wol/") {
			return
		}
		m := bookHref.FindStringSubmatch(href)
		if m == nil {
			return
		}
		num, _ := strconv.Atoi(m[1])
		if num < 1 || num > 66 {
			return
		}
		name := cleanSpace(a.AttrOr("title", ""))
		if name == "" {
			name = cleanSpace(a.Text())
		}
		if name == "" || len(name) > 40 {
			return
		}
		if len(names[num]) == 0 {
			names[num] = []string{name}
		}
	})
	if len(names) < 60 {
		return nil, fmt.Errorf("could not extract localized book names for %s (found %d)", cfg.Locale, len(names))
	}
	c.cache.Put(key, names)
	return names, nil
}

func xhrHeaders() map[string][]string {
	return map[string][]string{
		"X-Requested-With": {"XMLHttpRequest"},
		"Accept":           {"application/json, text/javascript, */*; q=0.01"},
	}
}

func absURL(base, path string) string {
	if path == "" || strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return base + path
}
