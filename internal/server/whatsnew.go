package server

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dgrieser/jw-cli/internal/api/jworg"
	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/service"
)

// The page of what is new (/latest) puts together what the site published
// lately (service.LatestItems): jw.org's own page of what is new —
// articles, publications, news — and the media library's latest media, as
// the media start page keeps them (latest.go). The entries go by day, newest
// first, each leading to its page here: a medium to the media page,
// everything else to the reader.

// latestKinds are the filters of the page, by the ?kind= that asks for one.
var latestKinds = []string{"", "media", "pub", "news"}

// latestPage is the page of what is new.
type latestPage struct {
	basePage
	Kind string // the filter shown, "" for everything
	Tabs []pubTab
	Days []latestDay
}

// latestDay is a day's entries under its date. T is there for the listing
// partial, which names an empty list.
type latestDay struct {
	Heading string
	Items   []resultView
	T       *i18n.Messages
}

// whatsNew is jw.org's page of what is new in lang; one that cannot be read
// is no entries and the error, logged, rather than no page.
func (s *Server) whatsNew(ctx context.Context, lang string) ([]model.NewItem, error) {
	items, err := s.svc.WhatsNew(ctx, lang)
	if err != nil {
		s.logf("what's new: %s: %v", lang, err)
	}
	return items, err
}

// latestPubShelf is the row of the latest publications on the
// publications' start page, leading to the page of what is new: jw.org's
// list of what is new, its publications alone, newest first.
func latestPubShelf(items []model.NewItem, heading string, page basePage) *pubShelf {
	pubs := slices.DeleteFunc(slices.Clone(items), func(it model.NewItem) bool { return service.LatestGroup(it) != "pub" })
	if len(pubs) == 0 {
		return nil
	}
	sh := pubShelf{Class: "latest-pubs", Heading: heading, Href: latestPage{basePage: page}.LatestURL("pub")}
	for _, it := range pubs[:min(len(pubs), pubShelfLimit)] {
		sh.Tiles = append(sh.Tiles, pubTile{Title: it.Title, Subtitle: it.Context, Href: newItemHref(it, page.Lang), Image: it.ImageURL})
	}
	return &sh
}

// newItemHref is the page here an entry of what is new is read on.
func newItemHref(it model.NewItem, lang string) string {
	if it.LANK != "" {
		return basePage{Lang: lang}.WithLang("/media/item/" + url.PathEscape(it.LANK))
	}
	return articleHref(it.URL, lang)
}

// newItemKind names what an entry is, in the page's language.
func newItemKind(t *i18n.Messages, kind string) string {
	switch kind {
	case "video":
		return t.UIKindVideo
	case "audio":
		return t.UIKindAudio
	case "publication":
		return t.UIKindPublication
	case "issue":
		return t.UIKindIssue
	case "news":
		return t.UIKindNews
	}
	return t.UIKindArticle
}

// LatestURL addresses the page with one filter, keeping the language.
func (p latestPage) LatestURL(kind string) string {
	q := url.Values{}
	if kind != "" {
		q.Set("kind", kind)
	}
	if p.Lang != "" {
		q.Set("lang", p.Lang)
	}
	if enc := q.Encode(); enc != "" {
		return "/latest?" + enc
	}
	return "/latest"
}

func (s *Server) uiLatest(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	kind := r.FormValue("kind")
	if !slices.Contains(latestKinds, kind) {
		kind = ""
	}
	t := text(lng)
	page := latestPage{basePage: s.base(r, t.UINavLatest).withSite(s.svc.JWOrg.WhatsNewURL(lng.Symbol)), Kind: kind}
	for _, k := range latestKinds {
		page.Tabs = append(page.Tabs, pubTab{Title: latestTabTitle(t, k), Href: page.LatestURL(k), Active: k == kind})
	}

	// the media as the media start page has them: found at the last update
	var (
		news    []model.NewItem
		newsErr error
		pick    model.Category
		recent  []model.MediaItem
		wg      sync.WaitGroup
	)
	wg.Go(func() { news, newsErr = s.whatsNew(r.Context(), lng.Symbol) })
	if kind == "" || kind == "media" {
		wg.Go(func() { recent = s.latest(r.Context(), lng.Symbol) })
		wg.Go(func() {
			pick, _ = s.svc.Mediator.Category(r.Context(), lng.Symbol, mediaLatestCategory, mediaShelfLimit, 0)
		})
	}
	wg.Wait()

	items := service.LatestItems(news, service.MergeRecent(pick.Media, recent))
	if kind != "" {
		items = slices.DeleteFunc(items, func(it model.NewItem) bool { return service.LatestGroup(it) != kind })
	}
	// the media are there without jw.org's list; the page says what is not
	if newsErr != nil && !service.IsStaleFeed(newsErr) && kind != "media" {
		page.Error = t.UILatestUnreadable
	}
	page.Days = groupByDay(s.latestViews(items, t, page.Lang), items, t)
	s.render(w, http.StatusOK, "latest", page)
}

// latestViews are the rows of what is new, one for each entry.
func (s *Server) latestViews(items []model.NewItem, t *i18n.Messages, lang string) []resultView {
	views := make([]resultView, len(items))
	for i, it := range items {
		v := resultView{
			Kind:      it.Kind,
			KindLabel: newItemKind(t, it.Kind),
			Title:     it.Title,
			Context:   it.Context,
			Href:      newItemHref(it, lang),
			External:  it.URL,
			ImageURL:  it.ImageURL,
			Duration:  it.Duration,
		}
		if strings.EqualFold(v.Context, t.UILatestNews) {
			v.Context = "" // "NEWS" beside the mark saying it is news
		}
		views[i] = v
	}
	return views
}

// latestTabTitle names a filter of the page.
func latestTabTitle(t *i18n.Messages, kind string) string {
	switch kind {
	case "media":
		return t.UINavMedia
	case "pub":
		return t.UINavPublications
	case "news":
		return t.UILatestNews
	}
	return t.UILatestAll
}

// groupByDay puts the rows of what is new, in their order, under the days
// their entries came out on.
func groupByDay(views []resultView, items []model.NewItem, t *i18n.Messages) []latestDay {
	var days []latestDay
	current := ""
	for i, v := range views {
		if d := items[i].Published; len(days) == 0 || d != current {
			current = d
			heading := d
			if at, err := time.ParseInLocation(time.DateOnly, d, service.LatestZone); err == nil {
				heading = t.Date(at)
			}
			days = append(days, latestDay{Heading: heading, T: t})
		}
		v.Index = len(days[len(days)-1].Items) + 1
		days[len(days)-1].Items = append(days[len(days)-1].Items, v)
	}
	return days
}

// apiLatest is GET /api/v1/latest: what is new, as jw new -o json lists it
// — newest first, kind=media|pub|news for one kind, days for how far back
// the media are looked for (the server's own list of the latest media
// covers the last 30 days).
func (s *Server) apiLatest(w http.ResponseWriter, r *http.Request) {
	kind := r.FormValue("kind")
	if kind != "" && !slices.Contains(latestKinds, kind) {
		badRequest(w, "unknown kind %q (want media, pub or news)", kind)
		return
	}
	days := latestDays
	if v := r.FormValue("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			badRequest(w, "days must be a whole number of at least 1, not %q", v)
			return
		}
		days = n
	}
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	var (
		news    []model.NewItem
		pick    model.Category
		recent  []model.MediaItem
		newsErr error
		wg      sync.WaitGroup
	)
	wg.Go(func() { news, newsErr = s.whatsNew(r.Context(), lng.Symbol) })
	if kind == "" || kind == "media" {
		wg.Go(func() {
			if days == latestDays {
				recent = s.latest(r.Context(), lng.Symbol) // as the media page has them
				return
			}
			recent, _ = s.svc.RecentMedia(r.Context(), lng.Symbol, time.Now().AddDate(0, 0, -days))
		})
		wg.Go(func() {
			pick, _ = s.svc.Mediator.Category(r.Context(), lng.Symbol, mediaLatestCategory, mediaShelfLimit, 0)
		})
	}
	wg.Wait()
	items := service.LatestItems(news, service.MergeRecent(pick.Media, recent))
	if kind != "" {
		items = slices.DeleteFunc(items, func(it model.NewItem) bool { return service.LatestGroup(it) != kind })
	}
	if len(items) == 0 && newsErr != nil {
		failJSON(w, r, newsErr)
		return
	}
	if items == nil {
		items = []model.NewItem{}
	}
	answer := map[string]any{"lang": lng.Symbol, "items": items}
	switch {
	case service.IsStaleFeed(newsErr):
		// the last feed posted, past its hold: what it is, not a failure
		answer["stale"] = newsErr.Error()
	case newsErr != nil:
		// a partial answer says it is partial: the media without jw.org's list
		answer["error"] = newsErr.Error()
	}
	writeJSON(w, http.StatusOK, answer)
}

// maxFeed bounds a posted feed: jw.org's runs to some 20 KB.
const maxFeed = 2 << 20

// feedTypes are the types a feed is posted as. None of them is a type a
// form of another site can post without the browser asking first.
var feedTypes = []string{"application/rss+xml", "application/xml", "text/xml"}

// apiPostFeed is POST /api/v1/latest/feed: the RSS feed of jw.org's page of
// what is new (…/rss/WhatsNewWebArticles/feed.xml), as a machine jw.org
// lets through read it, for a server it turns away. The feed's own link
// names its language; ?lang=, when given, must be the same. Admins only,
// when the server has accounts. The feed stands in for jw.org's page for
// service.FeedHold, during which www.jw.org is not asked for it at all.
func (s *Server) apiPostFeed(w http.ResponseWriter, r *http.Request) {
	if s.authOn() {
		if u := userOf(r); u == nil || !u.IsAdmin() {
			writeError(w, http.StatusForbidden, "forbidden", "posting the feed of what is new takes an admin's API key")
			return
		}
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || !slices.Contains(feedTypes, mt) {
		writeError(w, http.StatusUnsupportedMediaType, "bad_request", "want the RSS feed as "+strings.Join(feedTypes, ", "))
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxFeed))
	if err != nil {
		badRequest(w, "unreadable feed: %v", err)
		return
	}
	items, locale, err := jworg.ParseWhatsNewFeed(body)
	if err != nil {
		badRequest(w, "%s", err)
		return
	}
	lng, err := s.resolveLanguage(r.Context(), locale)
	if err != nil {
		badRequest(w, "the feed's language %q: %v", locale, err)
		return
	}
	if spec := r.FormValue("lang"); spec != "" {
		asked, err := s.resolveLanguage(r.Context(), spec)
		if err != nil {
			failJSON(w, r, err)
			return
		}
		if asked.Symbol != lng.Symbol {
			badRequest(w, "the feed is in %s (%s), not in %s", lng.Symbol, locale, asked.Symbol)
			return
		}
	}
	if s.svc.Feeds == nil {
		writeError(w, http.StatusNotFound, "not_found", "this server keeps no posted feeds")
		return
	}
	feed, err := s.svc.Feeds.Put(lng.Symbol, items, time.Now())
	if err != nil {
		failJSON(w, r, err)
		return
	}
	s.logf("what's new: %s: feed posted, %d entries, holds until %s", lng.Symbol, len(items), feed.Until().Format(time.RFC3339))
	writeJSON(w, http.StatusOK, feedStatus(feed, true, time.Now()))
}

// apiFeed is GET /api/v1/latest/feed?lang=: the feed last posted in a
// language and whether it still holds; 404 for none.
func (s *Server) apiFeed(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	feed, ok := s.svc.Feeds.Get(lng.Symbol)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no feed of what is new was posted in "+lng.Symbol)
		return
	}
	writeJSON(w, http.StatusOK, feedStatus(feed, false, time.Now()))
}

// feedStatus is what the feed routes answer about a posted feed.
func feedStatus(feed service.PostedFeed, withItems bool, now time.Time) map[string]any {
	out := map[string]any{
		"lang":     feed.Lang,
		"received": feed.Received,
		"until":    feed.Until(),
		"holds":    feed.Holds(now),
		"count":    len(feed.Items),
	}
	if withItems {
		out["items"] = feed.Items
	}
	return out
}
