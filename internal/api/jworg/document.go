package jworg

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/model"
)

// DocumentURL is the jw.org page of a document in a language: the finder
// leads any document id to its page, a song of the songbook as an original
// song or an article read aloud.
func (c *Client) DocumentURL(symbol string, docid int) string {
	return c.hc.Base.JWOrg + "/finder?" + url.Values{"wtlocale": {symbol}, "docid": {strconv.Itoa(docid)}}.Encode()
}

// Document reads the text of a jw.org document, line by line: the stanzas of
// a song, or the headings and paragraphs of anything else. Each line keeps
// its paragraph id, which is what a recording's markers time.
func (c *Client) Document(ctx context.Context, symbol string, docid int) (model.MediaDocument, error) {
	u := c.DocumentURL(symbol, docid)
	doc, err := c.hc.GetHTML(ctx, u)
	if err != nil {
		return model.MediaDocument{}, err
	}
	d := parseDocument(doc)
	d.DocID, d.URL = docid, u
	if len(d.Blocks) == 0 {
		return d, fmt.Errorf("no text found in document %d", docid)
	}
	return d, nil
}

func parseDocument(doc *goquery.Document) model.MediaDocument {
	var d model.MediaDocument
	art := doc.Find("article#article").First()
	if art.Length() == 0 {
		art = doc.Find("article").First()
	}
	if art.Length() == 0 {
		return d
	}
	head := art.Find("header").First()
	d.Context = clean(head.Find(".contextTtl").First().Text())
	d.Title = clean(head.Find("h1").First().Text())
	if d.Title == "" {
		d.Title = clean(art.Find("h1").First().Text())
	}
	d.Theme = clean(art.Find(".themeScrp").First().Text())
	d.Closing = clean(art.Find(".closingContent").First().Text())
	if img := art.Find("#docSubImg .jsRespImg").First(); img.Length() > 0 {
		d.PrintedEdition = firstAttr(img, "data-zoom", "data-img-size-lg", "data-img-size-md")
	}
	body := art.Find(".bodyTxt").First()
	if body.Length() == 0 {
		return d
	}
	body.Find("a.jsDownload").Each(func(_ int, a *goquery.Selection) {
		if u := firstAttr(a, "data-jsonurl", "href"); u != "" {
			d.Downloads = append(d.Downloads, model.Link{Label: clean(a.Text()), URL: u})
		}
	})
	if stanzas := body.Find("ol.source > li"); stanzas.Length() > 0 {
		stanzas.Each(func(_ int, li *goquery.Selection) {
			d.Blocks = append(d.Blocks, songBlocks(li)...)
		})
		return d
	}
	if strings.Contains(" "+art.AttrOr("class", "")+" ", " "+songClass+" ") {
		if blocks := numberedStanzas(body); len(blocks) > 0 {
			d.Blocks = blocks
			return d
		}
	}
	body.Find("h2, h3, h4, p[data-pid]").Each(func(_ int, s *goquery.Selection) {
		if skipped(s) {
			return
		}
		line := textLine(s)
		if line.Text == "" {
			return
		}
		kind := model.BlockParagraph
		if goquery.NodeName(s) != "p" {
			kind = model.BlockHeading
		}
		d.Blocks = append(d.Blocks, model.TextBlock{Kind: kind, Lines: []model.TextLine{line}})
	})
	return d
}

// songBlocks is one stanza of a song: its lines, and the choruses (or the
// bridge) sung after it, each a block of its own named by its first line
// when that line is only a name — "(CHORUS)". Lines after a chorus belong to
// the stanza still, in a block of their own so the order holds.
func songBlocks(li *goquery.Selection) []model.TextBlock {
	label := clean(li.Find(".txtSrcBullet").First().Text())
	cur := model.TextBlock{Kind: model.BlockStanza, Label: label}
	var out []model.TextBlock
	flush := func() {
		if len(cur.Lines) > 0 {
			out = append(out, cur)
		}
	}
	li.Children().Each(func(_ int, s *goquery.Selection) {
		if goquery.NodeName(s) == "p" {
			if line := textLine(s); line.Text != "" {
				cur.Lines = append(cur.Lines, line)
			}
			return
		}
		flush()
		chorus := model.TextBlock{Kind: model.BlockChorus}
		s.Find("p").Each(func(i int, p *goquery.Selection) {
			line := textLine(p)
			if line.Text == "" {
				return
			}
			if i == 0 && nameOnly(p) {
				chorus.Label = line.Text
				return
			}
			chorus.Lines = append(chorus.Lines, line)
		})
		if len(chorus.Lines) > 0 {
			out = append(out, chorus)
		}
		cur = model.TextBlock{Kind: model.BlockStanza}
	})
	flush()
	return out
}

// songClass marks the page of a song.
const songClass = "docClass-31"

// stanzaNumber is the number an older song's page sets before the first line
// of a stanza, in the line itself: "2. Your lofty throne, ...".
var stanzaNumber = regexp.MustCompile(`^(\d+[.)])\s+`)

// numberedStanzas reads a song whose stanzas are not a list but runs of
// lines, the first of each numbered; a line that is only a name in bold
// begins a chorus. Nothing when the page has headings or no numbered line.
func numberedStanzas(body *goquery.Selection) []model.TextBlock {
	if body.Find("h2, h3, h4").Length() > 0 {
		return nil
	}
	var out []model.TextBlock
	numbered := false
	body.Find("p[data-pid]").Each(func(_ int, p *goquery.Selection) {
		if skipped(p) {
			return
		}
		line := textLine(p)
		if line.Text == "" {
			return
		}
		switch m := stanzaNumber.FindStringSubmatch(line.Text); {
		case m != nil:
			numbered = true
			line.Text = strings.TrimPrefix(line.Text, m[0])
			out = append(out, model.TextBlock{Kind: model.BlockStanza, Label: m[1]})
		case nameOnly(p):
			out = append(out, model.TextBlock{Kind: model.BlockChorus, Label: line.Text})
			return
		case len(out) == 0:
			out = append(out, model.TextBlock{Kind: model.BlockStanza})
		}
		out[len(out)-1].Lines = append(out[len(out)-1].Lines, line)
	})
	if !numbered {
		return nil
	}
	return out
}

// nameOnly says a paragraph is a name set in bold and nothing else, as
// "(CHORUS)" is.
func nameOnly(p *goquery.Selection) bool {
	strong := clean(p.Find("strong").Text())
	return strong != "" && strong == clean(p.Text())
}

// textLine is a paragraph's words, without the stanza number set before them.
func textLine(s *goquery.Selection) model.TextLine {
	s = s.Clone()
	s.Find(".txtSrcBullet, .pageNum, sup, .footnoteLink").Remove()
	pid, _ := strconv.Atoi(s.AttrOr("data-pid", ""))
	return model.TextLine{PID: pid, Text: clean(s.Text())}
}

// skipped are the paragraphs of a page that are not its text: the list of
// downloads and its caption, the hidden ones, the captions of pictures.
func skipped(s *goquery.Selection) bool {
	if s.HasClass("displayNone") || s.Closest("figure, figcaption, .gallery").Length() > 0 {
		return true
	}
	if s.Find("a.jsDownload").Length() > 0 {
		return true
	}
	next := s.Next()
	return strings.HasSuffix(clean(s.Text()), ":") && next.Find("a.jsDownload").Length() > 0
}

func firstAttr(s *goquery.Selection, names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(s.AttrOr(n, "")); v != "" {
			return v
		}
	}
	return ""
}
