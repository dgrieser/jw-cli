package service

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/unfold"
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

// citable is the document an expansion of art reads its citations in. wol
// writes a verse a document cites as a citation link (/wol/bc/), which the
// expansion follows; jw.org writes the same verse as a link into its study
// bible (a.jsBibleLink), which is no citation. A jw.org article the library
// also carries — the same document, by the same id — is unfolded from the
// library's copy; one it does not carry is left as it is, with nothing to
// unfold.
func (s *Service) citable(ctx context.Context, lng model.Language, art model.Article) model.Article {
	if art.DocID == 0 || !onJWOrg(art.URL) || len(unfold.Refs(art.HTML)) > 0 {
		return art
	}
	cfg, err := s.WOLConfig(ctx, lng)
	if err != nil {
		return art
	}
	lib, err := s.WOL.Document(ctx, cfg, art.DocID)
	if err != nil || len(unfold.Refs(lib.HTML)) == 0 {
		return art
	}
	return lib
}

// onJWOrg reports whether a link leads to www.jw.org rather than the library.
func onJWOrg(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "jw.org" || strings.HasSuffix(host, ".jw.org") && host != "wol.jw.org"
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
