package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/model"
)

// The publications pages browse the library the way wol lays it out: the
// categories, a category's years, a year's editions and months, and at the
// bottom an issue or a publication with its articles and its files. Looking a
// publication up by symbol, document id, book or track stays with the CLI and
// the JSON API (/api/v1/pub).

// pubPage is every page of the publication browser.
type pubPage struct {
	basePage
	Crumbs  []crumb // the pages above this one, top first
	Heading string
	// Publication marks a page that is one publication or one issue: it
	// leads with its cover, what it is, and its files.
	Publication bool
	Cover       string
	Facts       []pubFact
	External    string
	Files       []pubFormat
	// Categories and Shelves make the start page: the categories to browse,
	// then a row of the latest covers of each.
	Categories []pubTile
	Shelves    []pubShelf
	Tabs       []pubTab
	Groups     []pubGroup
}

type pubFact struct{ Label, Value string }

// pubTile is one entry to open: a category, a year, an issue, a publication
// or an article.
type pubTile struct {
	Title, Subtitle, Href, Image string
}

// pubShelf is one row of covers on the start page: a category's latest
// issues or its publications, and where they were found below it.
type pubShelf struct {
	Heading, Context, Href string
	Tiles                  []pubTile
}

type pubTab struct {
	Title, Href string
	Active      bool
}

// pubGroup is one run of entries under a heading. Entries with covers show as
// a grid of tiles, entries without (years, editions) as a compact list of
// links, and articles as a table of contents.
type pubGroup struct {
	Heading string
	Tiles   []pubTile
	Chips   []pubTile
	Docs    []pubTile
}

// pubFormat is one format of a publication's files. A format that comes in
// many files (the audio tracks) folds them away.
type pubFormat struct {
	Format string
	Count  string
	Many   bool
	Files  []pubFileView
}

type pubFileView struct {
	Title, Label, Size, URL string
}

const (
	// pubShelfLimit bounds a start page row, as the media page's rows are.
	pubShelfLimit = 24
	// pubShelfDepth is how far below a category the start page looks for
	// covers: a periodical keeps them under year and edition.
	pubShelfDepth = 3
	// pubShelfTries is how many entries of a level are looked into.
	pubShelfTries = 2
	// pubManyFiles is the number of files a format may list openly.
	pubManyFiles = 3
)

// pubFormatOrder is the order formats are listed in: the written ones first.
var pubFormatOrder = []string{"PDF", "EPUB", "JWPUB", "RTF", "TXT", "BRL", "DAISY", "MP3", "AAC", "MP4", "M4V", "3GP", "ZIP"}

// pubHref is the browser's own page for a library or publication path. The
// top of the tree — a library path of one segment, "all-publications" — is
// /pub itself.
func pubHref(kind, path string, page basePage) string {
	if kind == wol.LibraryKind && !strings.Contains(path, "/") {
		return page.WithLang("/pub")
	}
	segs := strings.Split(path, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return page.WithLang("/pub/" + kind + "/" + strings.Join(segs, "/"))
}

// cardHref is where a card of a library page leads in the browser.
func cardHref(c wol.LibraryCard, page basePage) string {
	switch c.Kind {
	case wol.LibraryKind, wol.PublicationKind:
		return pubHref(c.Kind, c.Path, page)
	case wol.DocumentKind:
		return articleHref(strconv.Itoa(c.DocID), page.Lang)
	}
	return articleHref(c.URL, page.Lang)
}

func cardTile(c wol.LibraryCard, page basePage) pubTile {
	return pubTile{Title: c.Title, Subtitle: c.Subtitle, Href: cardHref(c, page), Image: c.Thumbnail}
}

func (s *Server) uiPubRoot(w http.ResponseWriter, r *http.Request) {
	s.uiPubPage(w, r, wol.LibraryKind, "")
}

func (s *Server) uiPubLibrary(w http.ResponseWriter, r *http.Request) {
	s.uiPubPage(w, r, wol.LibraryKind, r.PathValue("path"))
}

func (s *Server) uiPubPublication(w http.ResponseWriter, r *http.Request) {
	s.uiPubPage(w, r, wol.PublicationKind, r.PathValue("path"))
}

func (s *Server) uiPubPage(w http.ResponseWriter, r *http.Request, kind, path string) {
	lng, err := s.language(r)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	path = strings.Trim(path, "/")
	lib, err := s.svc.Library(r.Context(), lng, kind, path)
	if err != nil {
		// a library path with no counterpart in the language asked for —
		// or one not in English — leads back to the top of the tree
		if status, _ := classify(r.Context(), err); status == http.StatusNotFound && kind == wol.LibraryKind && path != "" {
			http.Redirect(w, r, s.base(r, "").WithLang("/pub"), http.StatusSeeOther)
			return
		}
		s.failUI(w, r, err)
		return
	}
	txt := text(lng)
	page := pubPage{basePage: s.base(r, firstNonEmpty(lib.Title, txt.UINavPublications))}
	// a bible is read as one
	if lib.Bible && lib.Symbol != "" {
		http.Redirect(w, r, page.WithLang("/bible?bible="+url.QueryEscape(lib.Symbol)), http.StatusSeeOther)
		return
	}
	root := kind == wol.LibraryKind && !strings.Contains(path, "/")
	if root {
		page.Heading = txt.UINavPublications
		page.Categories, page.Shelves = s.pubStart(r.Context(), lng, lib, page.basePage)
		s.render(w, http.StatusOK, "pub", page)
		return
	}
	page.Heading = firstNonEmpty(lib.Title, txt.UINavPublications)

	// the trail and the files are separate lookups; neither holds up the other
	var wg sync.WaitGroup
	wg.Go(func() { page.Crumbs = s.pubCrumbs(r.Context(), lng, lib, page.basePage) })
	if lib.Symbol != "" {
		wg.Go(func() {
			if pm, err := s.svc.LibraryFiles(r.Context(), lng, lib); err == nil {
				page.Files = pubFormats(pm, txt.UIFileCount)
			}
		})
	}
	wg.Wait()

	if lib.Symbol != "" || kind == wol.PublicationKind {
		page.Publication = true
		page.Cover = lib.Thumbnail
		page.External = lib.URL
		if lib.Symbol != "" {
			page.Facts = append(page.Facts, pubFact{txt.UISymbol, lib.Symbol})
		}
		if lib.Issue != "" {
			page.Facts = append(page.Facts, pubFact{txt.UIIssue, formatIssue(lib.Issue)})
		} else if lib.Year != "" {
			page.Facts = append(page.Facts, pubFact{txt.UIYear, lib.Year})
		}
	}
	for _, t := range lib.Tabs {
		page.Tabs = append(page.Tabs, pubTab{Title: t.Title, Href: pubHref(kind, t.Path, page.basePage), Active: t.Selected})
	}
	for _, g := range lib.Groups {
		page.Groups = append(page.Groups, pubGroupOf(g, page.basePage))
	}
	s.render(w, http.StatusOK, "pub", page)
}

// pubGroupOf sorts a group's cards into the three ways they are shown.
func pubGroupOf(g wol.LibraryGroup, page basePage) pubGroup {
	out := pubGroup{Heading: g.Title}
	covers := slices.ContainsFunc(g.Cards, func(c wol.LibraryCard) bool {
		return c.Kind != wol.DocumentKind && c.Thumbnail != ""
	})
	for _, c := range latestFirst(g.Cards) {
		t := cardTile(c, page)
		switch {
		case c.Kind == wol.DocumentKind || c.Kind == "link":
			out.Docs = append(out.Docs, t)
		case covers:
			out.Tiles = append(out.Tiles, t)
		default:
			out.Chips = append(out.Chips, t)
		}
	}
	return out
}

// issueCover reads the year and issue off the cover of a periodical's issue:
// .../publication/r1/lp-e/w24/2024/5/thumbnail.
var issueCover = regexp.MustCompile(`/publication/[^/]+/[^/]+/[^/]+/(\d{4})/(\d+)/thumbnail$`)

// latestFirst puts the issues of a periodical newest first, the way they are
// read, where wol lists them from January on. Cards that are not all issues
// keep wol's order.
func latestFirst(cards []wol.LibraryCard) []wol.LibraryCard {
	type dated struct {
		card        wol.LibraryCard
		year, issue int
	}
	var issues []dated
	for _, c := range cards {
		if c.Kind == wol.DocumentKind {
			return cards
		}
		m := issueCover.FindStringSubmatch(c.Thumbnail)
		if m == nil {
			return cards
		}
		year, _ := strconv.Atoi(m[1])
		issue, _ := strconv.Atoi(m[2])
		issues = append(issues, dated{c, year, issue})
	}
	if len(issues) < 2 {
		return cards
	}
	slices.SortStableFunc(issues, func(a, b dated) int {
		if a.year != b.year {
			return b.year - a.year
		}
		return b.issue - a.issue
	})
	out := make([]wol.LibraryCard, len(issues))
	for i, d := range issues {
		out[i] = d.card
	}
	return out
}

// pubStart builds the start page out of the top of the tree: the categories,
// and for each one rows of covers found below it — a periodical's latest
// issues across its years, the publications themselves for books. Each
// category is looked into at once; one that fails or has no covers has no row.
func (s *Server) pubStart(ctx context.Context, lng model.Language, lib wol.LibraryPage, page basePage) ([]pubTile, []pubShelf) {
	var cats []wol.LibraryCard
	for _, c := range lib.Cards() {
		if c.Kind == wol.LibraryKind || c.Kind == wol.PublicationKind {
			cats = append(cats, c)
		}
	}
	tiles := make([]pubTile, len(cats))
	shelves := make([][]pubShelf, len(cats))
	var wg sync.WaitGroup
	for i, c := range cats {
		tiles[i] = cardTile(c, page)
		if c.Kind != wol.LibraryKind {
			continue
		}
		wg.Go(func() { shelves[i] = s.pubShelvesOf(ctx, lng, c, page) })
	}
	wg.Wait()
	return tiles, slices.Concat(shelves...)
}

// pubShelvesOf finds a category's rows. A periodical — a category of years —
// gets a row of its latest issues gathered across the years, one per edition
// where a year has several (the public and the study edition). Any other
// category gets its first page of covers: the category itself when its
// publications have them, else the first entries below it.
func (s *Server) pubShelvesOf(ctx context.Context, lng model.Language, cat wol.LibraryCard, page basePage) []pubShelf {
	lib, err := s.svc.Library(ctx, lng, cat.Kind, cat.Path)
	if err != nil {
		return nil
	}
	if years := yearCards(lib); len(years) > 1 {
		return s.periodicalShelves(ctx, lng, cat, years, page)
	}
	sh := s.pubCovers(ctx, lng, cat, nil, pubShelfDepth, page)
	if sh == nil {
		return nil
	}
	sh.Heading = cat.Title
	return []pubShelf{*sh}
}

// yearCards are the years of a periodical's page, newest first as wol lists
// them; none when the page is not a periodical's.
func yearCards(lib wol.LibraryPage) []wol.LibraryCard {
	var years []wol.LibraryCard
	others := 0
	for _, c := range lib.Cards() {
		if c.Kind == wol.LibraryKind && c.Thumbnail == "" && titleYear.MatchString(c.Title) {
			years = append(years, c)
		} else {
			others++ // "Quellen für das Arbeitsheft" beside the years
		}
	}
	if others > 2 || others >= len(years) {
		return nil
	}
	return years
}

var titleYear = regexp.MustCompile(`(?:^|\D)(?:18|19|20)\d\d(?:\D|$)`)

const (
	// pubShelfYears bounds how far back a periodical's row reaches: Awake!
	// has one issue a year now, a dozen a decade ago.
	pubShelfYears = 12
	// pubYearBatch is how many years are read at once while a row fills.
	pubYearBatch = 4
	// pubMaxEditions is the most editions a year is taken to come in.
	pubMaxEditions = 3
)

// periodicalShelves gathers a periodical's issues year by year, newest first,
// until each of its rows holds pubShelfLimit covers. A year that lists its
// issues fills the one row; a year that lists editions fills a row per
// edition, matched across years by their order (public before study).
func (s *Server) periodicalShelves(ctx context.Context, lng model.Language, cat wol.LibraryCard, years []wol.LibraryCard, page basePage) []pubShelf {
	type row struct {
		title, href string
		cards       []wol.LibraryCard
	}
	var rows []*row
	rowAt := func(i int, title, href string) *row {
		for len(rows) <= i {
			rows = append(rows, &row{})
		}
		if rows[i].title == "" && rows[i].href == "" {
			rows[i].title, rows[i].href = title, href
		}
		return rows[i]
	}
	full := func() bool {
		if len(rows) == 0 {
			return false
		}
		for _, r := range rows {
			if len(r.cards) < pubShelfLimit {
				return false
			}
		}
		return true
	}
	years = years[:min(len(years), pubShelfYears)]
	for start := 0; start < len(years) && !full(); start += pubYearBatch {
		batch := years[start:min(len(years), start+pubYearBatch)]
		// per year: its own covers, or each edition's
		got := make([][][]wol.LibraryCard, len(batch))
		editions := make([][]wol.LibraryCard, len(batch))
		var wg sync.WaitGroup
		for i, y := range batch {
			wg.Go(func() {
				lib, err := s.svc.Library(ctx, lng, y.Kind, y.Path)
				if err != nil {
					return
				}
				if issues := coverCards(lib); len(issues) > 0 {
					got[i] = [][]wol.LibraryCard{issues}
					return
				}
				for _, c := range lib.Cards() {
					if c.Kind == wol.LibraryKind {
						editions[i] = append(editions[i], c)
					}
				}
				// a year of months without covers is no year of editions
				if len(editions[i]) > pubMaxEditions {
					editions[i] = nil
					return
				}
				got[i] = make([][]wol.LibraryCard, len(editions[i]))
				var ew sync.WaitGroup
				for e, ed := range editions[i] {
					ew.Go(func() {
						if lib, err := s.svc.Library(ctx, lng, ed.Kind, ed.Path); err == nil {
							got[i][e] = coverCards(lib)
						}
					})
				}
				ew.Wait()
			})
		}
		wg.Wait()
		for i := range batch {
			for e, cards := range got[i] {
				title, href := "", pubHref(cat.Kind, cat.Path, page)
				if editions[i] != nil {
					title = editions[i][e].Title
				}
				r := rowAt(e, title, href)
				r.cards = append(r.cards, cards...)
			}
		}
	}
	newest := 0
	for _, r := range rows {
		if len(r.cards) > 0 {
			newest = max(newest, issueYear(newestIssuesFirst(r.cards)[0]))
		}
	}
	var out []pubShelf
	for _, r := range rows {
		cards := newestIssuesFirst(r.cards)
		if len(cards) < 2 {
			continue // one cover is no row
		}
		// an edition that ended years ago (the simplified one) is no row
		if newest > 0 && issueYear(cards[0]) < newest-1 {
			continue
		}
		cards = cards[:min(len(cards), pubShelfLimit)]
		sh := pubShelf{Heading: cat.Title, Context: r.title, Href: r.href}
		for _, c := range cards {
			t := cardTile(c, page)
			if m := issueCover.FindStringSubmatch(c.Thumbnail); m != nil && !strings.Contains(c.Title+" "+c.Subtitle, m[1]) {
				t.Title += " " + m[1] // January → January 2025
			}
			sh.Tiles = append(sh.Tiles, t)
		}
		out = append(out, sh)
	}
	return out
}

// isIssues reports whether every card is a periodical's issue.
func isIssues(cards []wol.LibraryCard) bool {
	for _, c := range cards {
		if !issueCover.MatchString(c.Thumbnail) {
			return false
		}
	}
	return true
}

// mepsDocID reads the year out of a book's or brochure's document id:
// 1102021352 is 2021.
var mepsDocID = regexp.MustCompile(`^110((?:19|20)\d\d)\d{3}$`)

// release is when a publication came out, as near as the library tells: the
// year its page records, else the year in the id of its first article; the
// id orders publications of one year.
type release struct{ year, docid int }

// releaseOf reads a publication's release off its own page.
func (s *Server) releaseOf(ctx context.Context, lng model.Language, c wol.LibraryCard) release {
	lib, err := s.svc.Library(ctx, lng, c.Kind, c.Path)
	if err != nil {
		return release{}
	}
	var r release
	r.year, _ = strconv.Atoi(lib.Year)
	for _, d := range lib.Cards() {
		if d.Kind != wol.DocumentKind {
			continue
		}
		r.docid = d.DocID
		if m := mepsDocID.FindStringSubmatch(strconv.Itoa(d.DocID)); m != nil && r.year == 0 {
			r.year, _ = strconv.Atoi(m[1])
		}
		break
	}
	return r
}

// newestReleasesFirst orders books, brochures, tracts and the like by their
// release, newest first; one the library gives no date for goes last, in
// wol's order. Each publication's page is read at once, and kept for a week.
func (s *Server) newestReleasesFirst(ctx context.Context, lng model.Language, cards []wol.LibraryCard) []wol.LibraryCard {
	rel := make([]release, len(cards))
	var wg sync.WaitGroup
	for i, c := range cards {
		wg.Go(func() { rel[i] = s.releaseOf(ctx, lng, c) })
	}
	wg.Wait()
	idx := make([]int, len(cards))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int {
		ra, rb := rel[a], rel[b]
		if ra.year != rb.year {
			return rb.year - ra.year
		}
		return rb.docid - ra.docid
	})
	out := make([]wol.LibraryCard, len(cards))
	for i, j := range idx {
		out[i] = cards[j]
	}
	return out
}

// newestIssuesFirst orders a row gathered across years by the year and issue
// on each cover, newest first; an entry without them goes last.
func newestIssuesFirst(cards []wol.LibraryCard) []wol.LibraryCard {
	out := slices.Clone(cards)
	key := func(c wol.LibraryCard) (int, int) {
		m := issueCover.FindStringSubmatch(c.Thumbnail)
		if m == nil {
			return 0, 0
		}
		year, _ := strconv.Atoi(m[1])
		issue, _ := strconv.Atoi(m[2])
		return year, issue
	}
	slices.SortStableFunc(out, func(a, b wol.LibraryCard) int {
		ay, ai := key(a)
		by, bi := key(b)
		if ay != by {
			return by - ay
		}
		return bi - ai
	})
	return out
}

// issueYear is the year on an issue's cover, zero for none.
func issueYear(c wol.LibraryCard) int {
	if m := issueCover.FindStringSubmatch(c.Thumbnail); m != nil {
		y, _ := strconv.Atoi(m[1])
		return y
	}
	return 0
}

// coverCards are a page's entries that show a cover.
func coverCards(lib wol.LibraryPage) []wol.LibraryCard {
	var out []wol.LibraryCard
	for _, c := range lib.Cards() {
		if (c.Kind == wol.LibraryKind || c.Kind == wol.PublicationKind) && c.Thumbnail != "" {
			out = append(out, c)
		}
	}
	return out
}

func (s *Server) pubCovers(ctx context.Context, lng model.Language, at wol.LibraryCard, trail []string, depth int, page basePage) *pubShelf {
	lib, err := s.svc.Library(ctx, lng, at.Kind, at.Path)
	if err != nil {
		return nil
	}
	var covers, below []wol.LibraryCard
	for _, c := range lib.Cards() {
		if c.Kind != wol.LibraryKind && c.Kind != wol.PublicationKind {
			continue
		}
		if c.Thumbnail != "" {
			covers = append(covers, c)
		}
		if c.Kind == wol.LibraryKind {
			below = append(below, c)
		}
	}
	// issues by their date, everything else by its release
	if isIssues(covers) {
		covers = latestFirst(covers)
	} else if len(covers) > 1 {
		covers = s.newestReleasesFirst(ctx, lng, covers)
	}
	var tiles []pubTile
	for _, c := range covers {
		tiles = append(tiles, cardTile(c, page))
	}
	// one cover is no row
	if len(tiles) > 1 {
		return &pubShelf{
			Context: strings.Join(trail, " › "),
			Href:    pubHref(at.Kind, at.Path, page),
			Tiles:   tiles[:min(len(tiles), pubShelfLimit)],
		}
	}
	if depth <= 1 {
		return nil
	}
	for _, next := range below[:min(len(below), pubShelfTries)] {
		if sh := s.pubCovers(ctx, lng, next, append(slices.Clip(trail), next.Title), depth-1, page); sh != nil {
			return sh
		}
	}
	return nil
}

// pubCrumbs is the trail from the start page down to the page above lib. A
// page names only its parent, so each level is a lookup of its own — cached,
// and on the way down already read; one that fails cuts the trail short.
func (s *Server) pubCrumbs(ctx context.Context, lng model.Language, lib wol.LibraryPage, page basePage) []crumb {
	var trail []crumb
	for p := lib.Parent; p != nil && len(trail) < maxCrumbs; {
		if p.Kind == wol.LibraryKind && !strings.Contains(p.Path, "/") {
			break // the start page, added below
		}
		up, err := s.svc.Library(ctx, lng, p.Kind, p.Path)
		if err != nil || up.Title == "" {
			break
		}
		trail = append(trail, crumb{Label: up.Title, Href: pubHref(p.Kind, p.Path, page)})
		p = up.Parent
	}
	trail = append(trail, crumb{Label: text(lng).UINavPublications, Href: page.WithLang("/pub")})
	slices.Reverse(trail)
	return trail
}

// pubFormats groups a publication's files by format, the written formats
// first.
func pubFormats(pm model.PubMedia, countFormat string) []pubFormat {
	byFormat := map[string][]model.PubFile{}
	for _, formats := range pm.Files {
		for format, files := range formats {
			byFormat[format] = append(byFormat[format], files...)
		}
	}
	formats := make([]string, 0, len(byFormat))
	for f := range byFormat {
		formats = append(formats, f)
	}
	rank := func(f string) int {
		if i := slices.Index(pubFormatOrder, f); i >= 0 {
			return i
		}
		return len(pubFormatOrder)
	}
	slices.SortFunc(formats, func(a, b string) int {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra - rb
		}
		return strings.Compare(a, b)
	})
	var out []pubFormat
	for _, format := range formats {
		files := byFormat[format]
		pf := pubFormat{Format: format, Many: len(files) > pubManyFiles, Count: fmt.Sprintf(countFormat, len(files))}
		for _, f := range files {
			v := pubFileView{Title: firstNonEmpty(f.Title, pm.PubName), URL: f.URL}
			if f.Label != "" && f.Label != "0p" {
				v.Label = f.Label
			}
			if f.Filesize > 0 {
				v.Size = humanSize(f.Filesize)
			}
			pf.Files = append(pf.Files, v)
		}
		out = append(out, pf)
	}
	return out
}

// formatIssue writes an issue date the way a reader reads it: 202405 as
// 2024-05, a dated issue 20240515 as 2024-05-15.
func formatIssue(issue string) string {
	switch len(issue) {
	case 6:
		return issue[:4] + "-" + issue[4:]
	case 8:
		return issue[:4] + "-" + issue[4:6] + "-" + issue[6:]
	}
	return issue
}

// --- JSON API --------------------------------------------------------------

// apiPubLibrary is one page of the publication tree as JSON: the categories
// with no path, a category, year or issue below them with one.
func (s *Server) apiPubLibrary(w http.ResponseWriter, r *http.Request) {
	s.apiPubPage(w, r, wol.LibraryKind)
}

// apiPubPublication is the table of contents of one publication.
func (s *Server) apiPubPublication(w http.ResponseWriter, r *http.Request) {
	s.apiPubPage(w, r, wol.PublicationKind)
}

func (s *Server) apiPubPage(w http.ResponseWriter, r *http.Request, kind string) {
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	path := strings.Trim(r.PathValue("path"), "/")
	if kind == wol.PublicationKind && path == "" {
		badRequest(w, "missing publication path")
		return
	}
	lib, err := s.svc.Library(r.Context(), lng, kind, path)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, lib)
}
