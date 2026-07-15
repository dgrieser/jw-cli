package cli

import (
	"context"
	"fmt"

	"github.com/dgrieser/jw-cli/internal/api/search"
	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
	"github.com/dgrieser/jw-cli/internal/results"
	"github.com/dgrieser/jw-cli/internal/tui"
)

// tuiActions builds the shared key-binding behavior for all interactive
// listings.
func tuiActions(ctx context.Context, a *app.App, lng model.Language) tui.Actions {
	return tui.Actions{
		Show: func(item model.Result) (string, error) {
			switch item.Kind {
			case "video", "audio":
				mi, err := a.Mediator().MediaItem(ctx, lng.Symbol, item.LANK)
				if err != nil {
					return "", err
				}
				return mediaInfoText(mi), nil
			case "file", "image":
				return fmt.Sprintf("%s\n\n%s\n\nPress d to download.", item.Title, item.FileURL), nil
			}
			target := firstNonEmpty(item.WOLLink, item.JWLink)
			if target == "" && item.DocID != 0 {
				target = fmt.Sprint(item.DocID)
			}
			if target == "" {
				return "", fmt.Errorf("%s has no readable content", item.Title)
			}
			art, err := fetchArticle(ctx, a, target)
			if err != nil {
				return "", err
			}
			format, err := a.Format()
			if err != nil || format == render.JSON {
				format = render.Markdown
			}
			base := a.HTTP().Base.WOL
			body, err := render.Render(art.HTML, format, render.Options{BaseURL: base})
			if err != nil {
				return "", err
			}
			if art.Title != "" {
				body = art.Title + "\n\n" + body
			}
			return body, nil
		},
		Download: func(item model.Result) (string, error) {
			return tuiDownload(ctx, a, lng, item)
		},
		Open: func(item model.Result) error {
			link := preferredLink(item)
			if link == "" {
				return fmt.Errorf("no link available")
			}
			return openInBrowser(link)
		},
		Browse: func(item model.Result) (tui.Fetcher, string, bool) {
			if item.Kind != "category" {
				return nil, "", false
			}
			return categoryFetcher(ctx, a, lng, item.CategoryKey), item.Title, true
		},
	}
}

func tuiDownload(ctx context.Context, a *app.App, lng model.Language, item model.Result) (string, error) {
	switch {
	case item.LANK != "" && (item.Kind == "video" || item.Kind == "audio"):
		mi, err := a.Mediator().MediaItem(ctx, lng.Symbol, item.LANK)
		if err != nil {
			return "", err
		}
		file, err := pickBest(mi)
		if err != nil {
			return "", err
		}
		return downloadURL(ctx, a, file.URL, file.Checksum, file.Filesize, "", "")
	case item.FileURL != "":
		return downloadURL(ctx, a, item.FileURL, item.Checksum, item.Filesize, "", "")
	}
	return "", fmt.Errorf("%s has nothing directly downloadable", item.Title)
}

func pickBest(mi model.MediaItem) (model.MediaFile, error) {
	if len(mi.Files) == 0 {
		return model.MediaFile{}, fmt.Errorf("no files for %s", mi.Title)
	}
	best := mi.Files[0]
	for _, f := range mi.Files[1:] {
		if f.FrameHeight > best.FrameHeight || (f.FrameHeight == best.FrameHeight && f.Filesize > best.Filesize) {
			best = f
		}
	}
	return best, nil
}

// categoryFetcher pages through one mediator category ("" = root list).
func categoryFetcher(ctx context.Context, a *app.App, lng model.Language, key string) tui.Fetcher {
	const pageSize = 50
	return func(page int) (results.ResultSet, string, error) {
		if key == "" {
			cats, err := a.Mediator().RootCategories(ctx, lng.Symbol)
			if err != nil {
				return results.ResultSet{}, "", err
			}
			rs := results.ResultSet{Kind: "media-browse", Lang: lng.Symbol, Items: categoriesToResults(cats)}
			_ = results.Save(a.Cache().Dir(), rs)
			return rs, "Media categories", nil
		}
		cat, err := a.Mediator().Category(ctx, lng.Symbol, key, pageSize, (page-1)*pageSize)
		if err != nil {
			return results.ResultSet{}, "", err
		}
		items := append(categoriesToResults(cat.Subcategories), mediaToResults(cat.Media)...)
		if len(items) == 0 && page > 1 {
			return results.ResultSet{}, "", fmt.Errorf("no more items")
		}
		rs := results.ResultSet{Kind: "media-browse", Query: key, Lang: lng.Symbol, Page: page, Items: items}
		_ = results.Save(a.Cache().Dir(), rs)
		header := fmt.Sprintf("%s (%s)", cat.Name, cat.Key)
		if page > 1 {
			header += fmt.Sprintf(" — page %d", page)
		}
		return rs, header, nil
	}
}

// searchFetcher pages through search results for either engine.
func searchFetcher(ctx context.Context, a *app.App, lng model.Language, engine, query, facet, sortBy, scope string, limit int) tui.Fetcher {
	return func(page int) (results.ResultSet, string, error) {
		rs, header, err := runSearch(ctx, a, lng, engine, query, facet, sortBy, scope, limit, page)
		if err != nil {
			return results.ResultSet{}, "", err
		}
		if len(rs.Items) == 0 && page > 1 {
			return results.ResultSet{}, "", fmt.Errorf("no more results")
		}
		_ = results.Save(a.Cache().Dir(), rs)
		return rs, header, nil
	}
}

// runSearch executes one page of a search on the chosen engine.
func runSearch(ctx context.Context, a *app.App, lng model.Language, engine, query, facet, sortBy, scope string, limit, page int) (results.ResultSet, string, error) {
	switch engine {
	case "jworg", "jw", "":
		sp, err := a.Search().Search(ctx, lng.Symbol, search.Params{
			Query: query, Facet: facet, Sort: sortBy,
			Offset: (page - 1) * limit, Limit: limit,
		})
		if err != nil {
			return results.ResultSet{}, "", err
		}
		header := fmt.Sprintf("%d results for %q", sp.Total, query)
		if sp.Total > limit {
			header += fmt.Sprintf(" (page %d, %d per page)", page, limit)
		}
		rs := results.ResultSet{Kind: "search", Query: query, Lang: lng.Symbol, Page: page, Items: sp.Results}
		return rs, header, nil
	case "wol":
		return searchWOL(ctx, a, lng, query, scope, sortBy, page)
	}
	return results.ResultSet{}, "", fmt.Errorf("invalid engine %q (want jworg or wol)", engine)
}

func runSearchTUI(ctx context.Context, a *app.App, lng model.Language, fetch tui.Fetcher, header string) error {
	return tui.Run(header, fetch, tuiActions(ctx, a, lng))
}

func runBrowseTUI(ctx context.Context, a *app.App, lng model.Language, key string) error {
	header := "Media categories"
	if key != "" {
		header = key
	}
	return tui.Run(header, categoryFetcher(ctx, a, lng, key), tuiActions(ctx, a, lng))
}
