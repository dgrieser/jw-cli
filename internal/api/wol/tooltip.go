package wol

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/dgrieser/jw-cli/internal/model"
)

// docIDClass is the class wol marks an element holding a document with:
// "todayItem today html5 pub-es docId-1102026200 pub-es26 …".
var docIDClass = regexp.MustCompile(`(?:^|\s)docId-(\d+)(?:\s|$)`)

// DocIDOfClasses is the document an element's classes say it holds, zero when
// they name none. It is what names a document that has no link of its own: a
// daily text on its page, a footnote's answer.
func DocIDOfClasses(classes string) int {
	if m := docIDClass.FindStringSubmatch(classes); m != nil {
		id, _ := strconv.Atoi(m[1])
		return id
	}
	return 0
}

// tooltipItem is one passage of a citation endpoint's answer. A citation can
// name several — a marginal reference points at every verse it lists, each an
// item of its own — and every one of them belongs to the answer.
type tooltipItem struct {
	Title            string `json:"title"`
	Caption          string `json:"caption"`
	Content          string `json:"content"`
	URL              string `json:"url"`
	ImageURL         string `json:"imageUrl"`
	PublicationTitle string `json:"publicationTitle"`
	// ArticleClasses are the classes of the document the passage is from,
	// its id among them ("document html5 docId-1001070147 pub-nwtsty bible
	// bibleBook …"): what names the document of a footnote, which comes
	// without a URL.
	ArticleClasses string `json:"articleClasses"`
}

// tooltip turns the items of an answer into the tooltip holding them, the
// first as the tooltip itself and the others as its More. locale is the one
// the citation was asked with: asked without a locale, the endpoint answers
// without one too — in the passage's own links as much as in its URL.
func (c *Client) tooltip(items []tooltipItem, locale string) model.Tooltip {
	tips := make([]model.Tooltip, len(items))
	for i, it := range items {
		tips[i] = model.Tooltip{
			Title:            it.Title,
			Caption:          it.Caption,
			ContentHTML:      relocalize(it.Content, locale),
			URL:              absURL(c.hc.Base.WOL, relocalizeURL(it.URL, locale)),
			ImageURL:         absURL(c.hc.Base.WOL, relocalizeURL(it.ImageURL, locale)),
			PublicationTitle: it.PublicationTitle,
			DocID:            DocIDOfClasses(it.ArticleClasses),
			Bible:            slices.Contains(strings.Fields(it.ArticleClasses), "bible"),
		}
	}
	tip := tips[0]
	if len(tips) > 1 {
		tip.More = tips[1:]
	}
	return tip
}
