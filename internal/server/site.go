package server

import (
	"context"
	"fmt"
	"net/url"

	"github.com/dgrieser/jw-cli/internal/model"
)

// Where a page of this server is read on jw.org or wol.jw.org, which the
// settings offer to open (basePage.SiteURL) — the only way a page leads out
// to the sites, besides the hold menu of a link.

// wolPage is the address of a page of wol.jw.org in the language, built the
// way the library's own client builds it: /{locale}/wol/{command}/{rsconf}/
// {lp}{rest}. Empty when the language's library cannot be found.
func (s *Server) wolPage(ctx context.Context, lng model.Language, command, rest string) string {
	cfg, err := s.svc.WOLConfig(ctx, lng)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s/%s/wol/%s/%s/%s%s", s.svc.HTTP.Base.WOL, cfg.Locale, command, cfg.Rsconf, cfg.Lp, rest)
}

// wolSearchPage is the Online Library's own search for what a search of it
// asked: the words, the scope, the order and the publications covered.
func (s *Server) wolSearchPage(ctx context.Context, lng model.Language, query, scope, sort string, cats []string) string {
	if sort == "" || sort == "rel" {
		sort = "occ" // wol's name for its default ranking
	}
	v := url.Values{"q": {query}, "p": {firstNonEmpty(scope, "par")}, "r": {sort}}
	for _, c := range cats {
		v.Add("fc[]", c)
	}
	return s.wolPage(ctx, lng, "s", "?"+v.Encode())
}

// jwSearchPage is jw.org's own search for a query: its finder goes on to the
// search page of the language, whose address is in the language's own words
// (/de/suche/).
func jwSearchPage(query string, lng model.Language) string {
	return "https://www.jw.org/finder?" + url.Values{"q": {query}, "wtlocale": {lng.Symbol}}.Encode()
}

// videoLibraryDocID is jw.org's video library, where tv.jw.org leads.
const videoLibraryDocID = "1011214"

// videoLibraryPage is jw.org's video library in the language, at one of its
// categories when key names one: the finder goes on to the library's page in
// the language's own words, keeping what follows the #, which is how the
// library addresses a category (#de/categories/VODChildren).
func videoLibraryPage(lng model.Language, key string) string {
	u := "https://www.jw.org/finder?" + url.Values{
		"docid": {videoLibraryDocID}, "prefer": {"content"}, "wtlocale": {lng.Symbol},
	}.Encode()
	if key != "" && lng.Locale != "" {
		u += "#" + lng.Locale + "/categories/" + url.PathEscape(key)
	}
	return u
}

// verseFragment opens a chapter of wol at a verse, by its id (book·1e6 +
// chapter·1e3 + verse), as wol's own links to a verse do.
func verseFragment(vid int) string {
	return fmt.Sprintf("#study=discover&v=%d:%d:%d", vid/1_000_000, vid/1_000%1_000, vid%1_000)
}
