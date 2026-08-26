package wol

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/model"
)

// The gallery item page of the study bible. A verse's media entry links to it
// and it is the only place the picture's metadata is written down: the study
// pane itself carries a thumbnail and a one-line title, while the gallery page
// adds the full-size rendition, the explanatory caption and the rights line.
const (
	selGalleryItem    = ".gallerySelectedItem"
	selGalleryTitle   = ".mediaTitle h2"
	selGalleryImg     = ".mediaContent img"
	selGalleryCaption = ".mediaCaption .captionWrapper"
	selGalleryCredit  = ".imgCredit"
	// the caption wrapper also holds the credit and the related-scripture list;
	// the description is the div that is neither
	selGalleryNonCaption = selGalleryCredit + ", .relatedScriptures"
)

// galleryTTL: an item's caption and credit are as stable as the publication.
const galleryTTL = 30 * 24 * time.Hour

// GalleryItem fetches the metadata of one study-bible media item from its
// gallery page: the full-size image, its long caption and its rights line.
// Cached for a month. Best effort by design — a caller that only wants the
// thumbnail can ignore the error.
func (c *Client) GalleryItem(ctx context.Context, itemURL string) (model.MediaAsset, error) {
	if itemURL == "" {
		return model.MediaAsset{}, fmt.Errorf("no gallery item URL")
	}
	// the study pane appends the chapter the item was reached from
	// ("#chapter=2"); the page itself is the same either way
	fetchURL := itemURL
	if i := strings.IndexByte(fetchURL, '#'); i >= 0 {
		fetchURL = fetchURL[:i]
	}
	key := "gallery1-" + fetchURL
	var cached model.MediaAsset
	if c.cache.Get(key, galleryTTL, &cached) && cached.URL != "" {
		cached.SourceURL = itemURL
		return cached, nil
	}
	doc, err := c.hc.GetHTML(ctx, fetchURL)
	if err != nil {
		return model.MediaAsset{}, err
	}
	asset, err := parseGalleryItem(doc.Selection, c.hc.Base.WOL)
	if err != nil {
		return model.MediaAsset{}, fmt.Errorf("read gallery item %s: %w", itemURL, err)
	}
	asset.SourceURL = itemURL
	c.cache.Put(key, asset)
	return asset, nil
}

func parseGalleryItem(sel *goquery.Selection, base string) (model.MediaAsset, error) {
	item := sel.Find(selGalleryItem).First()
	if item.Length() == 0 {
		return model.MediaAsset{}, fmt.Errorf("no gallery item on the page (layout changed?)")
	}
	asset := model.MediaAsset{Caption: cleanSpace(item.Find(selGalleryTitle).First().Text())}
	if img := item.Find(selGalleryImg).First(); img.Length() > 0 {
		src, _ := img.Attr("src")
		asset.URL = absURL(base, src)
		asset.Alt = cleanSpace(img.AttrOr("alt", ""))
	}
	wrapper := item.Find(selGalleryCaption).First()
	asset.Credit = cleanSpace(wrapper.Find(selGalleryCredit).First().Text())
	wrapper.Children().EachWithBreak(func(_ int, div *goquery.Selection) bool {
		if div.Is(selGalleryNonCaption) {
			return true
		}
		if txt := cleanSpace(div.Text()); txt != "" {
			asset.Description = txt
			return false
		}
		return true
	})
	if asset.URL == "" && asset.Caption == "" && asset.Description == "" {
		return model.MediaAsset{}, fmt.Errorf("gallery item carries no image and no caption")
	}
	return asset, nil
}
