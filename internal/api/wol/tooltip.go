package wol

import "github.com/dgrieser/jw-cli/internal/model"

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
		}
	}
	tip := tips[0]
	if len(tips) > 1 {
		tip.More = tips[1:]
	}
	return tip
}
