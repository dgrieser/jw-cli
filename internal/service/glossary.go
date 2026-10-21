package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dgrieser/jw-cli/internal/api/wol"
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
	key := "glossary1-" + lng.Locale
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
