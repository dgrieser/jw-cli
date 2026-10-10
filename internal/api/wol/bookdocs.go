package wol

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
)

// The documents a bible book's page links to below its chapters, and the table
// of the books of the Bible the study edition carries. Selectors that mirror
// the live markup are grouped here so layout drift is cheap to fix.
const (
	selIntroVideo = "video[data-json-src]"           // the introduction's video
	selIntroFacts = ".openingContent ul li p"        // "Writer: Matthew", under the video
	selIntroNotes = ".bodyTxt"                       // the noteworthy facts, where written
	selVideoSrc   = "[data-json-src]"                // a gallery item's video
	selTableRow   = "table tr"                       // one book of the table
	selGalleryRow = "ul.grid > li"                   // a chapter heading or a tile
	selGalleryHd  = ".chapterTitle"                  // "Matthew 2"
	selGalleryA   = "a.galleryItem"                  // a tile's gallery page
	selGalleryCap = ".title"                         // a tile's title
	classHasVideo = "hasVideo"                       // a tile that is a video
	classChapter  = "chapterItem"                    // a row that heads a chapter
	selDocTitle   = "#article .scalableui header h1" // a document's own heading
)

// BookTableDocID is the study edition's table of the books of the Bible:
// writer, place, date and time covered of every book, one row each in book
// order. A document keeps its id in every language.
const BookTableDocID = 1001070071

// Introduction reads a bible book's introduction page (one of BookNav's Links):
// its video, the facts listed under it and the noteworthy facts written below
// them. Most books have the video only.
func (c *Client) Introduction(ctx context.Context, pageURL string) (model.BookIntro, error) {
	return httpx.Memo(ctx, c.cache, "intro1-"+pageURL, func(ctx context.Context) (model.BookIntro, error) {
		doc, err := c.hc.GetHTML(ctx, pageURL)
		if err != nil {
			return model.BookIntro{}, err
		}
		intro := parseIntroduction(doc, c.hc.Base.WOL)
		intro.URL = pageURL
		if v := doc.Find(selIntroVideo).First(); v.Length() > 0 {
			video, err := c.Video(ctx, v.AttrOr("data-json-src", ""))
			if err == nil {
				if video.Poster == "" {
					video.Poster = absURL(c.hc.Base.WOL, v.AttrOr("data-img-src", ""))
				}
				intro.Video = &video
			}
		}
		if intro.Video == nil && intro.Facts == nil && intro.NotesHTML == "" {
			return model.BookIntro{}, fmt.Errorf("no introduction at %s (page layout changed?)", pageURL)
		}
		return intro, nil
	})
}

func parseIntroduction(doc *goquery.Document, base string) model.BookIntro {
	intro := model.BookIntro{Title: cleanSpace(doc.Find(selDocTitle).First().Text())}
	doc.Find(selIntroFacts).Each(func(_ int, p *goquery.Selection) {
		label := cleanSpace(p.Find("strong").First().Text())
		value := cleanSpace(strings.TrimPrefix(cleanSpace(p.Text()), label))
		label = strings.TrimSpace(strings.TrimSuffix(label, ":"))
		if label != "" && value != "" {
			intro.Facts = append(intro.Facts, model.Fact{Label: label, Value: value})
		}
	})
	if notes := doc.Find(selIntroNotes).First(); notes.Length() > 0 && cleanSpace(notes.Text()) != "" {
		if html, err := goquery.OuterHtml(notes); err == nil {
			intro.NotesHTML = html
		}
	}
	return intro
}

// Video resolves a video a library page embeds (its data-json-src, a /wol/vidlink
// address) to its renditions and subtitles.
func (c *Client) Video(ctx context.Context, src string) (model.Video, error) {
	if src == "" {
		return model.Video{}, fmt.Errorf("no video link")
	}
	type wireFile struct {
		Title string `json:"title"`
		File  struct {
			URL string `json:"url"`
		} `json:"file"`
		Label       string  `json:"label"`
		MimeType    string  `json:"mimetype"`
		FrameWidth  int     `json:"frameWidth"`
		FrameHeight int     `json:"frameHeight"`
		FrameRate   float64 `json:"frameRate"`
		Duration    float64 `json:"duration"`
		Filesize    int64   `json:"filesize"`
		Subtitled   bool    `json:"subtitled"`
		Subtitles   *struct {
			URL string `json:"url"`
		} `json:"subtitles"`
	}
	var resp struct {
		PubImage struct {
			URL string `json:"url"`
		} `json:"pubImage"`
		Files map[string]map[string][]wireFile `json:"files"`
	}
	u := absURL(c.hc.Base.WOL, src)
	if err := c.hc.GetJSON(ctx, u, xhrHeaders(), &resp); err != nil {
		return model.Video{}, err
	}
	var v model.Video
	for _, formats := range resp.Files {
		for _, f := range formats["MP4"] {
			if f.File.URL == "" {
				continue
			}
			if v.Title == "" {
				v.Title = f.Title
			}
			mf := model.MediaFile{
				URL: f.File.URL, Label: f.Label, MimeType: f.MimeType,
				FrameWidth: f.FrameWidth, FrameHeight: f.FrameHeight, FrameRate: f.FrameRate,
				Duration: f.Duration, Filesize: f.Filesize, Subtitled: f.Subtitled,
			}
			if f.Subtitles != nil {
				mf.SubtitlesURL = f.Subtitles.URL
			}
			v.Files = append(v.Files, mf)
		}
		if len(v.Files) > 0 {
			break // the page's own language is the only one listed
		}
	}
	if len(v.Files) == 0 {
		return model.Video{}, fmt.Errorf("no video files at %s", u)
	}
	return v, nil
}

// BookFacts reads the table of the books of the Bible: per book number, its
// writer, place written, writing completed and time covered, labelled by the
// table's column headings. Kept until the page changes upstream.
func (c *Client) BookFacts(ctx context.Context, cfg Config) (map[int][]model.Fact, error) {
	u := c.url(cfg, "d", fmt.Sprintf("/%d", BookTableDocID))
	return httpx.Memo(ctx, c.cache, "bookfacts1-"+u, func(ctx context.Context) (map[int][]model.Fact, error) {
		doc, err := c.hc.GetHTML(ctx, u)
		if err != nil {
			return nil, err
		}
		facts := parseBookTable(doc)
		if len(facts) < 66 {
			return nil, fmt.Errorf("table of the books at %s lists %d books, want 66 (page layout changed?)", u, len(facts))
		}
		return facts, nil
	})
}

// parseBookTable reads the table's rows in order — book 1 first — each under
// the column headings of the table it is in. The first column is the book's
// name, which the reader already knows.
func parseBookTable(doc *goquery.Document) map[int][]model.Fact {
	out := map[int][]model.Fact{}
	book := 0
	doc.Find("table").Each(func(_ int, table *goquery.Selection) {
		var heads []string
		table.Find("th").Each(func(_ int, th *goquery.Selection) {
			heads = append(heads, factLabel(cleanSpace(th.Text())))
		})
		table.Find(selTableRow).Each(func(_ int, tr *goquery.Selection) {
			cells := tr.Find("td")
			if cells.Length() == 0 {
				return
			}
			book++
			var facts []model.Fact
			cells.Each(func(i int, td *goquery.Selection) {
				if i == 0 || i >= len(heads) {
					return
				}
				if v := cleanSpace(td.Text()); v != "" && heads[i] != "" {
					facts = append(facts, model.Fact{Label: heads[i], Value: v})
				}
			})
			out[book] = facts
		})
	})
	return out
}

// factLabel turns a column heading set in capitals into a label: "WRITING
// COMPLETED (B.C.E.)" reads "Writing completed (B.C.E.)". An era in parentheses
// keeps its capitals; any other parenthesis follows the word ("Writer(s)").
func factLabel(head string) string {
	main, rest := head, ""
	if i := strings.IndexByte(head, '('); i >= 0 {
		main, rest = head[:i], head[i:]
	}
	main = strings.ToLower(main)
	if r, size := utf8.DecodeRuneInString(main); size > 0 {
		main = string(unicode.ToUpper(r)) + main[size:]
	}
	if !strings.Contains(rest, ".") {
		rest = strings.ToLower(rest)
	}
	return main + rest
}

// FactKey is what two labels of the same fact have in common, the table's
// heading and the introduction's label alike: "Writing completed (B.C.E.)" and
// "Writing Completed" are both "writing completed".
func FactKey(label string) string {
	if i := strings.IndexByte(label, '('); i >= 0 {
		label = label[:i]
	}
	return strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(label), ":")))
}

// Gallery reads a bible book's media gallery (one of BookNav's Links): its
// pictures and videos, chapter by chapter, as the library lists them.
func (c *Client) Gallery(ctx context.Context, pageURL string) (model.BookGallery, error) {
	return httpx.Memo(ctx, c.cache, "bookgallery1-"+pageURL, func(ctx context.Context) (model.BookGallery, error) {
		doc, err := c.hc.GetHTML(ctx, pageURL)
		if err != nil {
			return model.BookGallery{}, err
		}
		g := parseGallery(doc, c.hc.Base.WOL)
		if len(g.Groups) == 0 {
			return model.BookGallery{}, fmt.Errorf("no gallery items at %s (page layout changed?)", pageURL)
		}
		g.URL = pageURL
		return g, nil
	})
}

func parseGallery(doc *goquery.Document, base string) model.BookGallery {
	g := model.BookGallery{Title: cleanSpace(doc.Find(".gallery h1").First().Text())}
	doc.Find(selGalleryRow).Each(func(_ int, li *goquery.Selection) {
		if li.HasClass(classChapter) {
			g.Groups = append(g.Groups, model.GalleryGroup{Heading: cleanSpace(li.Find(selGalleryHd).Text())})
			return
		}
		a := li.Find(selGalleryA).First()
		if a.Length() == 0 {
			return
		}
		img := a.Find("img").First()
		tile := model.GalleryTile{
			Title:     cleanSpace(li.Find(selGalleryCap).First().Text()),
			URL:       absURL(base, a.AttrOr("href", "")),
			Thumbnail: absURL(base, img.AttrOr("src", "")),
			Image:     absURL(base, img.AttrOr("data-img-large-src", "")),
			Video:     li.HasClass(classHasVideo),
		}
		if len(g.Groups) == 0 {
			g.Groups = append(g.Groups, model.GalleryGroup{})
		}
		last := &g.Groups[len(g.Groups)-1]
		last.Items = append(last.Items, tile)
	})
	return g
}
