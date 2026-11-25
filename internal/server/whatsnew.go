package server

import (
	"cmp"
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/service"
)

// The page of what is new (/latest) puts together what the site published
// lately: jw.org's own page of what is new — articles, publications, news —
// and the media library's latest media, which that page mostly leaves out
// (latest.go). The entries go by day, newest first, each leading to its page
// here: a video to the media page, everything else to the reader.

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

// latestEntry is one entry before it is shown: when it came out and its row.
type latestEntry struct {
	at   time.Time
	kind string // media, pub or news
	view resultView
}

// whatsNew is jw.org's page of what is new in lang; one that cannot be read
// is no entries, logged, rather than no page.
func (s *Server) whatsNew(ctx context.Context, lang string) []model.NewItem {
	items, err := s.svc.WhatsNew(ctx, lang)
	if err != nil {
		s.logf("what's new: %s: %v", lang, err)
	}
	return items
}

// newPublications are the publications among what is new: the articles
// and issues of the library, not the news and the videos.
func newPublications(items []model.NewItem) []model.NewItem {
	var out []model.NewItem
	for _, it := range items {
		if latestKindOf(it) == "pub" {
			out = append(out, it)
		}
	}
	return out
}

// latestPubShelf is the row of the latest publications on the
// publications' start page, leading to the page of what is new: jw.org's
// list of what is new, its publications alone, newest first.
func latestPubShelf(items []model.NewItem, heading string, page basePage) *pubShelf {
	pubs := newPublications(items)
	if len(pubs) == 0 {
		return nil
	}
	sh := pubShelf{Class: "latest-pubs", Heading: heading, Href: latestPage{basePage: page}.LatestURL("pub")}
	for _, it := range pubs[:min(len(pubs), pubShelfLimit)] {
		sh.Tiles = append(sh.Tiles, pubTile{Title: it.Title, Subtitle: it.Context, Href: newItemHref(it, page.Lang), Image: it.ImageURL})
	}
	return &sh
}

// latestKindOf is the filter an entry of jw.org's page falls under.
func latestKindOf(it model.NewItem) string {
	switch it.Kind {
	case "video":
		return "media"
	case "news":
		return "news"
	}
	return "pub"
}

// newItemHref is the page here an entry of jw.org's page is read on.
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

	var (
		items  []model.NewItem
		pick   model.Category
		recent []model.MediaItem
		wg     sync.WaitGroup
	)
	wg.Go(func() { items = s.whatsNew(r.Context(), lng.Symbol) })
	wg.Go(func() { recent = s.latest(r.Context(), lng.Symbol) })
	wg.Go(func() {
		pick, _ = s.svc.Mediator.Category(r.Context(), lng.Symbol, mediaLatestCategory, mediaShelfLimit, 0)
	})
	wg.Wait()

	var entries []latestEntry
	media := withRecent(pick.Media, recent)
	listed := map[string]bool{}
	for i, v := range s.resultViews(service.MediaToResults(media), page.Lang) {
		m := media[i]
		listed[service.MediaStem(m.LANK)] = true
		if service.Published(m).IsZero() {
			continue // no day to list it under
		}
		v.Kind = newItemKind(t, m.Type)
		entries = append(entries, latestEntry{at: service.Published(m), kind: "media", view: v})
	}
	for _, it := range items {
		if it.LANK != "" && listed[service.MediaStem(it.LANK)] {
			continue
		}
		at, err := time.ParseInLocation(time.DateOnly, it.Published, latestZone)
		if err != nil {
			continue
		}
		ctxLine := it.Context
		if strings.EqualFold(ctxLine, t.UILatestNews) {
			ctxLine = "" // "NEWS" beside the mark saying it is news
		}
		entries = append(entries, latestEntry{at: at, kind: latestKindOf(it), view: resultView{
			Kind:     newItemKind(t, it.Kind),
			Title:    it.Title,
			Context:  ctxLine,
			Href:     newItemHref(it, page.Lang),
			External: it.URL,
			ImageURL: it.ImageURL,
		}})
	}
	if kind != "" {
		entries = slices.DeleteFunc(entries, func(e latestEntry) bool { return e.kind != kind })
	}
	page.Days = groupByDay(entries, t)
	s.render(w, http.StatusOK, "latest", page)
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

// groupByDay sorts the entries newest first and puts them under their days,
// Berlin time as the updates keep it. jw.org's page dates an entry by the
// day alone; on a day, its entries come before the media of that day, in
// its order.
func groupByDay(entries []latestEntry, t *i18n.Messages) []latestDay {
	day := func(at time.Time) time.Time {
		y, m, d := at.In(latestZone).Date()
		return time.Date(y, m, d, 0, 0, 0, 0, latestZone)
	}
	slices.SortStableFunc(entries, func(a, b latestEntry) int {
		if c := day(b.at).Compare(day(a.at)); c != 0 {
			return c
		}
		// the day's entries of jw.org's page first, then its media, latest first
		am, bm := a.kind == "media", b.kind == "media"
		if am != bm {
			if am {
				return 1
			}
			return -1
		}
		return cmp.Compare(b.at.UnixNano(), a.at.UnixNano())
	})
	var days []latestDay
	var current time.Time
	for _, e := range entries {
		if d := day(e.at); len(days) == 0 || !d.Equal(current) {
			current = d
			days = append(days, latestDay{Heading: t.Date(d), T: t})
		}
		e.view.Index = len(days[len(days)-1].Items) + 1
		days[len(days)-1].Items = append(days[len(days)-1].Items, e.view)
	}
	return days
}
