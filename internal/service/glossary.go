package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"html"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
)

// A glossary is a publication whose documents each explain one word: the
// study bible's glossary, Insight on the Scriptures, the Research Guide. Their
// tables of contents, read once a week like the week's bible reading, are an
// index of the words they explain, which the pages mark wherever the words
// come up (static/app.js).

// GlossaryBook is a publication read as a glossary, by its canonical library
// path (see Library).
type GlossaryBook struct {
	ID    string `json:"id"`
	Kind  string `json:"-"` // wol.PublicationKind or wol.LibraryKind
	Path  string `json:"-"`
	Title string `json:"title"`
}

// GlossaryBooks are the glossaries indexed, in the order a word explained in
// several is looked up in: the short entry before the long one.
var GlossaryBooks = []GlossaryBook{
	{ID: "nwtstg", Kind: wol.PublicationKind, Path: "nwtstg"},
	{ID: "it", Kind: wol.LibraryKind, Path: "all-publications/insight"},
	{ID: "rsg", Kind: wol.PublicationKind, Path: "rsg"},
}

// Glossary is the index of the words the glossaries of a language explain.
type Glossary struct {
	Lang  string         `json:"lang"`
	Books []GlossaryBook `json:"books"`
	// DocURL is the address of a document on wol.jw.org without its docid,
	// which a term's DocID completes.
	DocURL string `json:"docURL,omitempty"`
	// CaseSensitive and Suffixes say how a term is found in a text of the
	// language: whether its case must be the same (German writes every noun
	// capitalized, so "Arm" is no "arm"), and the endings an inflected word
	// may add to it ("Altars", "Gottes").
	CaseSensitive bool     `json:"caseSensitive,omitempty"`
	Suffixes      []string `json:"suffixes,omitempty"`
	// Terms are sorted by term, then by book.
	Terms []GlossaryTerm `json:"terms"`
	// Partial marks an index some of whose pages could not be read: it is
	// kept for a while only, and read again after.
	Partial bool `json:"partial,omitempty"`
	// Common are the entries left out as words of every day (see
	// glossaryCommon), by docid.
	Common []int `json:"common,omitempty"`
}

// GlossaryTerm is one word a glossary explains, and the document explaining it.
type GlossaryTerm struct {
	Term  string `json:"t"`
	Book  int    `json:"b"` // index into Glossary.Books
	DocID int    `json:"d"`
	// Title is the entry's own title, when it is more than the term: "Arab,
	// II", "Abijah (King of Judah)".
	Title string `json:"n,omitempty"`
}

// glossaryKeep is how long a glossary's index is kept before its pages are
// read again: as long as the week's bible reading, and as rarely changed.
// One read only in part is read again after glossaryRetry.
const (
	glossaryKeep  = readingKeep
	glossaryRetry = time.Hour
)

// glossaryKey is where the cache keeps the index of lng.
func glossaryKey(lng model.Language) string { return "glossary3-" + lng.Locale }

// keptGlossary is an index as the cache keeps it, with when it was read.
type keptGlossary struct {
	At       int64    `json:"at"`
	Glossary Glossary `json:"glossary"`
}

// glossaryCall is a reading of an index under way, which every caller asking
// for it meanwhile waits on.
type glossaryCall struct {
	done chan struct{}
	g    Glossary
	err  error
}

// Glossary is the index of the glossaries of lng, read off their tables of
// contents at most once a week. While they cannot be read, the index read
// before stands.
func (s *Service) Glossary(ctx context.Context, lng model.Language) (Glossary, error) {
	key := glossaryKey(lng)
	var kept keptGlossary
	found := s.Cache.Get(key, &kept)
	keep := glossaryKeep
	if kept.Glossary.Partial {
		keep = glossaryRetry
	}
	if found && time.Since(time.Unix(kept.At, 0)) < keep {
		return kept.Glossary, nil
	}
	// a hundred pages are read for it: once, however many ask meanwhile,
	// and to the end when the first to ask goes
	call := &glossaryCall{done: make(chan struct{})}
	if c, busy := s.glossaries.LoadOrStore(key, call); busy {
		call = c.(*glossaryCall)
	} else {
		go func() {
			defer s.glossaries.Delete(key)
			defer close(call.done)
			g, err := s.glossary(context.WithoutCancel(ctx), lng)
			// an index read in part is no better than a whole one read before
			worse := found && g.Partial && len(g.Terms) < len(kept.Glossary.Terms)
			switch {
			case err == nil && !worse:
				s.Cache.Put(key, keptGlossary{At: time.Now().Unix(), Glossary: g})
			case found:
				// what was read before stands while the library cannot be
				// read, and it is asked again in a while
				g, err = kept.Glossary, nil
				g.Partial = true
				s.Cache.Put(key, keptGlossary{At: time.Now().Unix(), Glossary: g})
			}
			call.g, call.err = g, err
		}()
	}
	select {
	case <-ctx.Done():
		return Glossary{}, ctx.Err()
	case <-call.done:
		return call.g, call.err
	}
}

// glossary reads the index off the glossaries' tables of contents. A page that
// cannot be read is left out, and the index marked partial; none read at all
// is an error.
func (s *Service) glossary(ctx context.Context, lng model.Language) (Glossary, error) {
	out := Glossary{Lang: lng.Symbol, Terms: []GlossaryTerm{}}
	out.CaseSensitive, out.Suffixes = glossaryMatch(lng)
	var firstErr error
	fail := func(err error) {
		out.Partial = true
		if firstErr == nil {
			firstErr = err
		}
	}
	for _, book := range GlossaryBooks {
		pages, title, err := s.glossaryPages(ctx, lng, book)
		if err != nil {
			fail(err)
			continue
		}
		bi := len(out.Books)
		book.Title = title
		out.Books = append(out.Books, book)
		results := make([][]wol.LibraryCard, len(pages))
		errs := make([]error, len(pages))
		var wg sync.WaitGroup
		sem := make(chan struct{}, 4)
		for i, p := range pages {
			wg.Go(func() {
				sem <- struct{}{}
				defer func() { <-sem }()
				page, err := s.Library(ctx, lng, p.Kind, p.Path)
				results[i], errs[i] = page.Cards(), err
			})
		}
		wg.Wait()
		for i, cards := range results {
			if errs[i] != nil {
				fail(errs[i])
				continue
			}
			for _, c := range cards {
				if c.Kind != wol.DocumentKind || c.DocID == 0 {
					continue
				}
				if out.DocURL == "" {
					out.DocURL = strings.TrimSuffix(c.URL, strconv.Itoa(c.DocID))
				}
				terms := GlossaryTerms(c.Title)
				for _, t := range terms {
					gt := GlossaryTerm{Term: t, Book: bi, DocID: c.DocID}
					if len(terms) > 1 || t != c.Title {
						gt.Title = c.Title
					}
					out.Terms = append(out.Terms, gt)
				}
			}
		}
	}
	if len(out.Terms) == 0 && firstErr != nil {
		return out, firstErr
	}
	if err := s.dropCommon(ctx, lng, &out); err != nil {
		fail(err)
	}
	slices.SortStableFunc(out.Terms, func(a, b GlossaryTerm) int {
		if c := strings.Compare(a.Term, b.Term); c != 0 {
			return c
		}
		return a.Book - b.Book
	})
	return out, nil
}

// glossaryPages are the pages of a glossary's table of contents that list its
// entries, a letter each, and its title. A publication is split into tabs
// (A, B, …, besides an introduction and an index of scriptures, which are no
// letters); a shelf has a card for each letter.
func (s *Service) glossaryPages(ctx context.Context, lng model.Language, book GlossaryBook) ([]wol.LibraryCard, string, error) {
	root, err := s.Library(ctx, lng, book.Kind, book.Path)
	if err != nil {
		return nil, "", err
	}
	title := strings.TrimSpace(root.Title)
	var pages []wol.LibraryCard
	if book.Kind == wol.PublicationKind {
		for _, t := range root.Tabs {
			if letterTab(t.Title) {
				pages = append(pages, wol.LibraryCard{Kind: wol.PublicationKind, Path: t.Path})
			}
		}
		if len(root.Tabs) == 0 {
			pages = append(pages, wol.LibraryCard{Kind: wol.PublicationKind, Path: book.Path})
		}
	} else {
		for _, c := range root.Cards() {
			if c.Kind == wol.LibraryKind || c.Kind == wol.PublicationKind {
				pages = append(pages, c)
			}
		}
	}
	if len(pages) == 0 {
		return nil, title, fmt.Errorf("%s: %w", book.Path, errNoGlossary)
	}
	return pages, title, nil
}

var errNoGlossary = errors.New("no entries listed")

// letterTab tells a tab of a letter's entries ("A", "Ä", "Ch") from the
// introduction or the index of scriptures.
func letterTab(title string) bool {
	title = strings.TrimSpace(title)
	return title != "" && utf8.RuneCountInString(title) <= 3
}

// glossaryMatch is how the terms of lng are found in its texts (see Glossary).
func glossaryMatch(lng model.Language) (caseSensitive bool, suffixes []string) {
	base, _, _ := strings.Cut(lng.Locale, "-")
	switch base {
	case "de":
		return true, []string{"s", "es", "n", "en", "e", "er", "ern"}
	case "en":
		return false, []string{"s", "es", "'s", "’s"}
	}
	return true, nil
}

var (
	// a bracket closing a title, or any inside it: what it says is no part
	// of the word ("Abijah (King of Judah)", "Canon (Bible canon)")
	glossaryBracket = regexp.MustCompile(`\s*\([^()]*\)`)
	// an ending in brackets right after the word is one it may take:
	// "Scripture(s)", "System(s) of things"
	glossaryEnding = regexp.MustCompile(`(\p{L})\((\p{L}{1,3})\)`)
	glossarySpace  = regexp.MustCompile(`\s+`)
)

// GlossaryTerms are the words an entry's title names. Alternatives are split
// apart ("Arabia; Arabian"); a word after a comma is one only when it is a
// form of the first ("Bank, Banker", "Burial, Burial Places"), not one that
// places it ("Amos, Book of", "Arab, II", "Hort, Fenton John Anthony"). What
// is said in brackets is left out; an ending in brackets gives the word with
// and without it ("Scripture(s)").
func GlossaryTerms(title string) []string {
	var out []string
	add := func(t string) {
		t = strings.Trim(glossarySpace.ReplaceAllString(t, " "), " “”\"„‚‘’«»")
		if utf8.RuneCountInString(t) >= 2 && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	for alt := range strings.SplitSeq(title, ";") {
		forms := []string{alt}
		if glossaryEnding.MatchString(alt) {
			forms = []string{glossaryEnding.ReplaceAllString(alt, "$1"), glossaryEnding.ReplaceAllString(alt, "$1$2")}
		}
		for _, f := range forms {
			parts := strings.Split(glossaryBracket.ReplaceAllString(f, ""), ",")
			for i, p := range parts {
				if i == 0 || sharedPrefix(parts[0], p) >= 3 {
					add(p)
				}
			}
		}
	}
	return out
}

// sharedPrefix is how many letters a and b begin with alike, case aside.
func sharedPrefix(a, b string) int {
	ra := []rune(strings.ToLower(strings.TrimSpace(a)))
	rb := []rune(strings.ToLower(strings.TrimSpace(b)))
	n := 0
	for n < len(ra) && n < len(rb) && ra[n] == rb[n] {
		n++
	}
	return n
}

// glossaryCommon are words of every day that a glossary explains too — a
// night, water, a mother — and that are not marked: they come up in every
// paragraph, and their entries are rarely what a reader is after. They are
// named in English, and the entries they title left out in every language:
// an entry is the same document in all of them (wol's /dsync/ of Insight's
// "Night", 1200003253, is "Nacht", 1200003253), so the English index names
// the documents. Words a glossary gives a meaning of its own — love, faith,
// soul, spirit, heart, world, word, earth, light of the bible's own sense
// aside — are not among them, and neither are names, units or the topics
// of the Research Guide.
var glossaryCommon = strings.Fields(`
	Time Times Day Night Week Weeks Month Year Hour Seasons Calendar Age Generation
	Four Six Seven Ten Twelve Number
	Mother Father Son Daughter Brother Sister Wife Husband Child Children Boy Kid
	Woman Man Friend Uncle Lady Guest Neighbor Neighbors Family Stranger Name Names
	Hand Head Face Eye Ear Mouth Nose Neck Hair Skin Foot Leg Arm Finger Thumb Toe
	Knee Heel Lip Teeth Tongue Throat Shoulder Breast Belly Stomach Bones Liver
	Kidneys Body Hips
	Black White Red Blue Green Greens Brown Purple Colors
	Water Light Fire Rain Snow Ice Wind Sun Moon Star Sky Cloud Smoke Dust Sand
	Stone Rock Ground Grass Trees Forest River Sea Island Isle Mountain Hill Valley
	Desert Field Garden Thunder Lightning Rainbow Shadow Spring Vegetation Weeds
	City Town Village House Homes Road Roads Street Highway Gate Gateway Walls Tower
	Castle Palace Window Building Park Pool Camp Inn Theater Marketplace Province
	District South North East West
	Book Paper Ink Pen Glass Bottle Cup Bowl Pot Basket Bag Bed Lamp Key Lock Ring
	Belt Dress Clothing Cloth Fabric Needle Nail Hammer Knife Cord Wheel Wagon
	Ladder Mirror Oven Seat Purse Luggage Jar Vessels Pipe Hook Bell Chains Cage
	Necklace Bracelet Beads Perfume Cosmetics Toys Maps Instrument Signal Saw
	Food Bread Milk Meat Cheese Butter Honey Cake Salt Oil Flour Garlic Onion Fish
	Wheat Meal Cooking Spice Apple
	Dog Horse Fox Wolf Lion Lions Bear Cow Pig Bull Goat Bee Ant Bat Frog Spider
	Worm Eagle Camel Snake Birds Insects Animals Cricket Nest
	Gold Silver Iron Copper Brass Bronze Tin Wool Cotton Silk Clay Brick Bricks
	Marble Wax Coal Coals Tar Lime Metals Diamond Crystal Pearl Ruby Coral Sapphire
	Emerald Jade Ivory Ebony
	Work Business Music Song Singing Singers Dancing Reading Writing Teaching
	Education School Language Speech Voice Hearing Sleep Dream Dreams Journey Kiss
	Kissing Greeting Sewing Farming Fishing Agriculture Architecture Mining Boxing
	Sports Games Money Debt Loan Purchase Wages Hire Taxes Taxation Interest Bank
	Measure Load Labor Mind Pain Fever Mistakes Accident End Well Prison
	Teacher Judge Leader Secretary Nurse Nurses Physician Doctors Merchant
	Messenger Builder Baker Soldier Army Guard Police Governor Commander Deputy
	Recorder Courier Runners Mason Translator Herald King Queen Prince Princes Duke
	Lawyers
	News Internet Television Technology Shopping Movies Gambling Soccer Lounge Yoga
`)

// commonEntry reports whether an entry of the English index is one of the
// words of every day: titled by one of them alone, or with its other forms
// ("Bank, Banker") or a numeral ("Pen, II"), but not with what makes it
// another thing ("Thunder, Sons of") or a topic of its own ("Work
// (Employment)").
func commonEntry(title string, common map[string]bool) bool {
	terms := GlossaryTerms(title)
	// an ending in brackets is a form of the word ("Time(s)")
	title = glossaryEnding.ReplaceAllString(title, "$1")
	if strings.Contains(title, "(") {
		return false
	}
	if len(terms) == 0 || !common[strings.ToLower(terms[0])] {
		return false
	}
	for alt := range strings.SplitSeq(title, ";") {
		for p := range strings.SplitSeq(alt, ",") {
			p = strings.TrimSpace(p)
			if !slices.Contains(terms, p) && !glossaryNumeral.MatchString(p) {
				return false
			}
		}
	}
	return true
}

var glossaryNumeral = regexp.MustCompile(`^[IVX]+$`)

// dropCommon leaves the words of every day out of an index, the entries the
// English index names; reading that index is what can fail.
func (s *Service) dropCommon(ctx context.Context, lng model.Language, g *Glossary) error {
	common := map[string]bool{}
	for _, w := range glossaryCommon {
		common[strings.ToLower(w)] = true
	}
	var ids []int
	if base, _, _ := strings.Cut(lng.Locale, "-"); base == LibraryLocale {
		for _, t := range g.Terms {
			title := cmp.Or(t.Title, t.Term)
			if commonEntry(title, common) && !slices.Contains(ids, t.DocID) {
				ids = append(ids, t.DocID)
			}
		}
	} else {
		en, err := s.Language(ctx, LibraryLocale)
		if err != nil {
			return err
		}
		eg, err := s.Glossary(ctx, en)
		if err != nil {
			return err
		}
		ids = slices.Clone(eg.Common)
		// what it misses is left out once it is read whole
		g.Partial = g.Partial || eg.Partial
	}
	slices.Sort(ids)
	g.Common = ids
	g.Terms = slices.DeleteFunc(g.Terms, func(t GlossaryTerm) bool {
		_, found := slices.BinarySearch(ids, t.DocID)
		return found
	})
	return nil
}

// ErrNotGlossary: a document asked for as a glossary's entry is none of the
// index.
var ErrNotGlossary = errors.New("not an entry of the glossaries")

// GlossaryEntries are the entries of lng's glossary index among docs, in the
// order of the glossaries, and the index itself; none is ErrNotGlossary.
func (s *Service) GlossaryEntries(ctx context.Context, lng model.Language, docs []int) (Glossary, []GlossaryTerm, error) {
	g, err := s.Glossary(ctx, lng)
	if err != nil {
		return g, nil, err
	}
	var entries []GlossaryTerm
	for _, t := range g.Terms {
		if slices.Contains(docs, t.DocID) && !slices.ContainsFunc(entries, func(e GlossaryTerm) bool { return e.DocID == t.DocID }) {
			entries = append(entries, t)
		}
	}
	if len(entries) == 0 {
		return g, nil, fmt.Errorf("%w: %v", ErrNotGlossary, docs)
	}
	slices.SortStableFunc(entries, func(a, b GlossaryTerm) int {
		if a.Book != b.Book {
			return a.Book - b.Book
		}
		return slices.Index(docs, a.DocID) - slices.Index(docs, b.DocID)
	})
	return g, entries, nil
}

// StreamGlossary streams the entries of the glossaries explaining a marked
// word, one section each, in the order of the glossaries — the study bible's
// glossary, Insight, the Research Guide — whichever of them explain it: docs
// are the entries a link names. Each section is headed by its glossary, and
// by the entry too where a glossary has several for the word; the first one
// comes opened, the others as headings. An entry that cannot be read is left
// out; none read at all is the first failure.
func (s *Service) StreamGlossary(ctx context.Context, lng model.Language, docs []int, txt *i18n.Messages,
	out UnfoldStream) (int, error) {
	g, entries, err := s.GlossaryEntries(ctx, lng, docs)
	if err != nil {
		return 0, err
	}
	sections := make([]*UnfoldSection, len(entries))
	errs := make([]error, len(entries))
	var wg sync.WaitGroup
	for i, e := range entries {
		wg.Go(func() {
			sec, err := s.ArticleSection(ctx, lng, g.DocURL+strconv.Itoa(e.DocID), UnfoldConfig{})
			if err != nil {
				errs[i] = err
				return
			}
			title := ""
			if e.Book >= 0 && e.Book < len(g.Books) {
				title = g.Books[e.Book].Title
			}
			several := 0
			for _, o := range entries {
				if o.Book == e.Book {
					several++
				}
			}
			if title == "" || several > 1 {
				title = strings.TrimPrefix(title+" – "+cmp.Or(e.Title, e.Term), " – ")
			}
			sec.Title, sec.Order = html.EscapeString(title), i
			sections[i] = &sec
		})
	}
	wg.Wait()
	sent := 0
	for _, sec := range sections {
		if sec == nil {
			continue
		}
		sec.Open = sent == 0
		out.Section(*sec)
		sent++
	}
	if sent == 0 {
		return len(entries), errors.Join(errs...)
	}
	return len(entries), nil
}
