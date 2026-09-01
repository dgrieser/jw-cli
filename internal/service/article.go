package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/dgrieser/jw-cli/internal/model"
)

// Article resolves a docid or URL to a parsed article.
func (s *Service) Article(ctx context.Context, lng model.Language, target string) (model.Article, error) {
	if docid, err := strconv.Atoi(target); err == nil {
		cfg, err := s.WOLConfig(ctx, lng)
		if err != nil {
			return model.Article{}, err
		}
		return s.WOL.Document(ctx, cfg, docid)
	}
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		return model.Article{}, fmt.Errorf("expected a document id or URL, got %q", target)
	}
	if strings.Contains(target, "wol.jw.org") || strings.Contains(target, "/wol/") {
		return s.WOL.DocumentByURL(ctx, target)
	}
	return s.JWOrg.ArticleByURL(ctx, target)
}

// ImagesToResults turns an article's illustrations into listing rows.
func ImagesToResults(art model.Article) []model.Result {
	items := make([]model.Result, 0, len(art.Images))
	for _, img := range art.Images {
		items = append(items, ImageResult(img, ""))
	}
	return items
}

// ImageResult turns an illustration into a listing row: its own words as the
// title, everything else it says as metadata. The URL is never the title —
// under --no-urls the row would be the one place a target still shows up, and a
// picture with nothing to say falls back to its index instead.
func ImageResult(img model.MediaAsset, context string) model.Result {
	meta := img.Meta()
	title := ""
	if meta != nil {
		title = meta.Label()
	}
	return model.Result{
		Kind: "image", Title: title, Context: context, Image: meta,
		FileURL: img.URL, ImageURL: img.URL,
	}
}
