package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
	"github.com/dgrieser/jw-cli/internal/results"
	"github.com/dgrieser/jw-cli/internal/service"
	"github.com/dgrieser/jw-cli/internal/tui"
)

// tuiActions builds the shared key-binding behavior for all interactive
// listings.
func tuiActions(ctx context.Context, a *app.App, lng model.Language) tui.Actions {
	return tui.Actions{
		NoColor: a.Flags.NoColor,
		Text:    a.Text(),
		Show: func(item model.Result) (tui.Content, error) {
			if mi, ok, err := mediaOf(ctx, a, item); ok {
				if err != nil {
					return tui.Content{}, err
				}
				md, err := renderComposed(a, render.Markdown, mediaInfoHTML(mi, a.Text(), a.Flags.NoURLs, render.Markdown))
				if err != nil {
					return tui.Content{}, err
				}
				return tui.Content{Text: md, Markdown: true}, nil
			}
			switch item.Kind {
			case "file", "image":
				parts := []string{strings.Join(imageDetailLines(item, a.Text()), "\n")}
				if !a.Flags.NoURLs && item.FileURL != "" {
					parts = append(parts, item.FileURL)
				}
				parts = append(parts, a.Text().PressToDownload)
				return tui.Content{Text: strings.Join(parts, "\n\n")}, nil
			}
			target := firstNonEmpty(item.WOLLink, item.JWLink)
			if target == "" && item.DocID != 0 {
				target = fmt.Sprint(item.DocID)
			}
			if target == "" {
				return tui.Content{}, fmt.Errorf("%s has no readable content", item.Title)
			}
			art, err := fetchArticle(ctx, a, target)
			if err != nil {
				return tui.Content{}, err
			}
			format, err := a.Format()
			if err != nil || format == render.JSON {
				format = render.Markdown
			}
			base := a.HTTP().Base.WOL
			body, err := render.Render(art.HTML, format, a.RenderOptions(base))
			if err != nil {
				return tui.Content{}, err
			}
			// -o raw asks for the markdown verbatim, in the pane too
			return tui.Content{Text: headedBody(art, format, body), Markdown: format == render.Markdown}, nil
		},
		Download: func(item model.Result) (string, error) {
			return tuiDownload(ctx, a, item)
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

func tuiDownload(ctx context.Context, a *app.App, item model.Result) (string, error) {
	if item.FileURL != "" {
		return downloadURL(ctx, a, item.FileURL, item.Checksum, item.Filesize, "", "")
	}
	mi, ok, err := mediaOf(ctx, a, item)
	if !ok {
		return "", fmt.Errorf("%s has nothing directly downloadable", item.Title)
	}
	if err != nil {
		return "", err
	}
	file, err := service.PickBest(mi)
	if err != nil {
		return "", err
	}
	return downloadURL(ctx, a, file.URL, file.Checksum, file.Filesize, "", "")
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
			rs := results.ResultSet{Kind: "media-browse", Lang: lng.Symbol, Items: rootCategoryRows(cats, lng.Symbol)}
			_ = results.Save(a.Cache().Dir(), &rs)
			return rs, a.Text().MediaCategories, nil
		}
		cat, err := a.Service().Category(ctx, lng.Symbol, key, pageSize, (page-1)*pageSize)
		if err != nil {
			return results.ResultSet{}, "", err
		}
		items := categoryRows(cat, lng.Symbol)
		if len(items) == 0 && page > 1 {
			return results.ResultSet{}, "", errors.New(a.Text().NoMoreItems)
		}
		rs := results.ResultSet{Kind: "media-browse", Query: key, Lang: lng.Symbol, Page: page, Items: items}
		_ = results.Save(a.Cache().Dir(), &rs)
		header := fmt.Sprintf("%s (%s)", cat.Name, cat.Key)
		if page > 1 {
			header += fmt.Sprintf(a.Text().PageSuffixShort, page)
		}
		return rs, header, nil
	}
}

// searchFetcher pages through search results for either engine.
func searchFetcher(ctx context.Context, a *app.App, lng model.Language, p service.SearchParams) tui.Fetcher {
	// the page each result was first listed on, across the session
	listed := map[string]int{}
	return func(page int) (results.ResultSet, string, error) {
		rs, header, err := runSearch(ctx, a, lng, &p, page)
		if err != nil {
			return results.ResultSet{}, "", err
		}
		if len(rs.Items) == 0 && page > 1 {
			return results.ResultSet{}, "", errors.New(a.Text().NoMoreResults)
		}
		rs.Items = dropRepeats(rs.Items, page, listed)
		_ = results.Save(a.Cache().Dir(), &rs)
		return rs, header, nil
	}
}

// dropRepeats leaves out the results an earlier page already listed. jw.org
// serves the first page of an all-types search grouped by type, sized by
// itself, and the following pages flat from the start of the results — so its
// second page repeats rows of the first. A result keeps its place on the page
// it first came on, so paging back shows that page whole again.
func dropRepeats(items []model.Result, page int, listed map[string]int) []model.Result {
	kept := items[:0:0]
	for _, r := range items {
		key := firstNonEmpty(r.LANK, r.JWLink, r.WOLLink)
		if first, ok := listed[key]; key != "" && ok && first < page {
			continue
		}
		if _, ok := listed[key]; key != "" && !ok {
			listed[key] = page
		}
		kept = append(kept, r)
	}
	return kept
}

// runSearch executes one page of a search on the chosen engine and heads the
// listing the way the terminal prints it.
func runSearch(ctx context.Context, a *app.App, lng model.Language, p *service.SearchParams, page int) (results.ResultSet, string, error) {
	out, err := a.Service().RunSearch(ctx, lng, p, page, excerptProgress(a))
	if err != nil {
		return results.ResultSet{}, "", err
	}
	var header string
	switch out.Kind {
	case "wol-search":
		header = a.Text().WolResults(out.Total, out.Query, out.Page)
	default:
		header = a.Text().Results(out.Total, out.Query)
		switch {
		case len(out.Items) > p.Limit:
			// a grouped first page: jw.org sized it itself, whatever the
			// limit, so the page is all there is to say
			header += fmt.Sprintf(a.Text().PageSuffixShort, out.Page)
		case out.Total > p.Limit:
			header += fmt.Sprintf(a.Text().PageSuffix, out.Page, p.Limit)
		}
	}
	return listingOf(out), header, nil
}

// listingOf is the listing a search prints and saves: one page of results and
// what the engine says about the whole query.
func listingOf(out service.SearchOutcome) results.ResultSet {
	return results.ResultSet{Kind: out.Kind, Query: out.Query, Lang: out.Lang, Page: out.Page,
		Total: out.Total, Items: out.Items}
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
