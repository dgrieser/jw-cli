// Package mediator is a client for the jw.org mediator API
// (https://b.jw-cdn.org/apis/mediator/v1), which backs JW Broadcasting:
// languages, video/audio category trees, and media items.
package mediator

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
)

type Client struct {
	hc *httpx.Client
}

func New(hc *httpx.Client) *Client { return &Client{hc: hc} }

func (c *Client) base() string { return c.hc.Base.CDN + "/apis/mediator/v1" }

type wireLanguage struct {
	Code           string `json:"code"`
	Locale         string `json:"locale"`
	Name           string `json:"name"`
	Vernacular     string `json:"vernacular"`
	Script         string `json:"script"`
	IsRTL          bool   `json:"isRTL"`
	IsSignLanguage bool   `json:"isSignLanguage"`
}

// Languages lists all languages with web content.
func (c *Client) Languages(ctx context.Context) ([]model.Language, error) {
	var resp struct {
		Languages []wireLanguage `json:"languages"`
	}
	if err := c.hc.GetJSON(ctx, c.base()+"/languages/E/web", nil, &resp); err != nil {
		return nil, err
	}
	langs := make([]model.Language, 0, len(resp.Languages))
	for _, l := range resp.Languages {
		langs = append(langs, model.Language{
			Symbol:         l.Code,
			Locale:         l.Locale,
			Name:           l.Name,
			Vernacular:     l.Vernacular,
			Script:         l.Script,
			RTL:            l.IsRTL,
			IsSignLanguage: l.IsSignLanguage,
		})
	}
	if len(langs) == 0 {
		return nil, fmt.Errorf("mediator returned no languages")
	}
	return langs, nil
}

type wireFile struct {
	ProgressiveDownloadURL string  `json:"progressiveDownloadURL"`
	Checksum               string  `json:"checksum"`
	Filesize               int64   `json:"filesize"`
	MimeType               string  `json:"mimetype"`
	Label                  string  `json:"label"`
	FrameHeight            int     `json:"frameHeight"`
	Duration               float64 `json:"duration"`
	Subtitles              *struct {
		URL string `json:"url"`
	} `json:"subtitles"`
}

type wireMediaItem struct {
	LANK                    string                       `json:"languageAgnosticNaturalKey"`
	Type                    string                       `json:"type"`
	Title                   string                       `json:"title"`
	Description             string                       `json:"description"`
	Duration                float64                      `json:"duration"`
	DurationFormattedMinSec string                       `json:"durationFormattedMinSec"`
	FirstPublished          string                       `json:"firstPublished"`
	PrimaryCategory         string                       `json:"primaryCategory"`
	AvailableLanguages      []string                     `json:"availableLanguages"`
	Files                   []wireFile                   `json:"files"`
	Images                  map[string]map[string]string `json:"images"`
}

func (w wireMediaItem) toModel() model.MediaItem {
	m := model.MediaItem{
		LANK:               w.LANK,
		Type:               w.Type,
		Title:              w.Title,
		Description:        w.Description,
		DurationSec:        w.Duration,
		DurationFormatted:  w.DurationFormattedMinSec,
		FirstPublished:     w.FirstPublished,
		PrimaryCategory:    w.PrimaryCategory,
		AvailableLanguages: w.AvailableLanguages,
		Images:             w.Images,
	}
	for _, f := range w.Files {
		mf := model.MediaFile{
			URL:         f.ProgressiveDownloadURL,
			Label:       f.Label,
			MimeType:    f.MimeType,
			Checksum:    f.Checksum,
			FrameHeight: f.FrameHeight,
			Filesize:    f.Filesize,
			Duration:    f.Duration,
		}
		if f.Subtitles != nil {
			mf.SubtitlesURL = f.Subtitles.URL
		}
		m.Files = append(m.Files, mf)
	}
	return m
}

type wireCategory struct {
	Key           string          `json:"key"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Type          string          `json:"type"`
	Subcategories []wireCategory  `json:"subcategories"`
	Media         []wireMediaItem `json:"media"`
}

func (w wireCategory) toModel() model.Category {
	c := model.Category{
		Key:         w.Key,
		Name:        w.Name,
		Description: w.Description,
		Type:        w.Type,
	}
	for _, s := range w.Subcategories {
		c.Subcategories = append(c.Subcategories, s.toModel())
	}
	for _, m := range w.Media {
		c.Media = append(c.Media, m.toModel())
	}
	return c
}

// RootCategories lists the top-level category tree for a language symbol.
func (c *Client) RootCategories(ctx context.Context, lang string) ([]model.Category, error) {
	u := fmt.Sprintf("%s/categories/%s?detailed=1&clientType=www", c.base(), url.PathEscape(lang))
	var resp struct {
		Categories []wireCategory `json:"categories"`
	}
	if err := c.hc.GetJSON(ctx, u, nil, &resp); err != nil {
		return nil, err
	}
	cats := make([]model.Category, 0, len(resp.Categories))
	for _, w := range resp.Categories {
		cats = append(cats, w.toModel())
	}
	return cats, nil
}

// Category fetches one category (with subcategories and media) by key.
func (c *Client) Category(ctx context.Context, lang, key string, limit, offset int) (model.Category, error) {
	u := fmt.Sprintf("%s/categories/%s/%s?detailed=1&clientType=www", c.base(), url.PathEscape(lang), url.PathEscape(key))
	if limit > 0 {
		u += "&limit=" + strconv.Itoa(limit)
	}
	if offset > 0 {
		u += "&offset=" + strconv.Itoa(offset)
	}
	var resp struct {
		Category   wireCategory `json:"category"`
		Pagination *struct {
			TotalCount int `json:"totalCount"`
		} `json:"pagination"`
	}
	if err := c.hc.GetJSON(ctx, u, nil, &resp); err != nil {
		return model.Category{}, err
	}
	cat := resp.Category.toModel()
	if resp.Pagination != nil {
		cat.Total = resp.Pagination.TotalCount
	}
	if cat.Key == "" {
		return cat, fmt.Errorf("category %q not found", key)
	}
	return cat, nil
}

// MediaItem fetches one media item by its language-agnostic natural key.
func (c *Client) MediaItem(ctx context.Context, lang, lank string) (model.MediaItem, error) {
	u := fmt.Sprintf("%s/media-items/%s/%s?clientType=www", c.base(), url.PathEscape(lang), url.PathEscape(lank))
	var resp struct {
		Media []wireMediaItem `json:"media"`
	}
	if err := c.hc.GetJSON(ctx, u, nil, &resp); err != nil {
		return model.MediaItem{}, err
	}
	if len(resp.Media) == 0 {
		return model.MediaItem{}, fmt.Errorf("media item %q not found in language %s", lank, lang)
	}
	return resp.Media[0].toModel(), nil
}
