package wol

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// The library names its pages in the words of each language
// (all-publications/books, alle-publikationen/bücher) and orders them by
// their titles, so neither a path nor a position carries over from one
// language to another, and wol has no link between the two. A page is told
// apart from its siblings by what is the same in every language instead: the
// publication its cover shows (w24/2024/5) or its title ends with ("(si)"),
// else the kind of publication it
// holds and the year in its title (w 2024), and among siblings alike in both,
// their order (the public before the study edition).

var (
	titleYear   = regexp.MustCompile(`(?:^|\D)((?:18|19|20)\d\d)(?:\D|$)`)
	titleSymbol = regexp.MustCompile(`\(([^()\s]+)\)\s*$`)
)

// cardKey is a card's language-independent name among its siblings, without
// the position that tells apart siblings alike in it. A page that is one
// publication in one language may be a shelf of its editions in another
// (a tract), so both go by the publication's symbol.
func cardKey(c LibraryCard) string {
	if m := libraryHref.FindStringSubmatch(c.Thumbnail); m != nil && m[1] == "publication" {
		return "sym:" + strings.TrimSuffix(strings.Trim(m[2], "/"), "/thumbnail")
	}
	if c.Kind == PublicationKind {
		return "sym:" + c.Path
	}
	// a shelf of one publication ends its title with the symbol:
	// "Enjoy Family Life (T-21)"; a year in brackets is no symbol
	if m := titleSymbol.FindStringSubmatch(c.Title); m != nil && !allDigits(m[1]) {
		return "sym:" + m[1]
	}
	year := ""
	if m := titleYear.FindStringSubmatch(c.Title); m != nil {
		year = m[1]
	}
	return c.Kind + ":" + c.Icon + ":" + year
}

func allDigits(s string) bool {
	return strings.Trim(s, "0123456789") == ""
}

// libraryKeys names the library and publication cards of a page: path to
// key, and key to card.
func libraryKeys(page LibraryPage) (byPath map[string]string, byKey map[string]LibraryCard) {
	byPath, byKey = map[string]string{}, map[string]LibraryCard{}
	seen := map[string]int{}
	for _, c := range page.Cards() {
		if c.Kind != LibraryKind && c.Kind != PublicationKind {
			continue
		}
		base := cardKey(c)
		key := fmt.Sprintf("%s#%d", base, seen[base])
		seen[base]++
		byPath[c.Path] = key
		byKey[key] = c
	}
	return byPath, byKey
}

// TranslateLibraryPath finds the page of the library in to that a library
// path of from names, by walking both trees down side by side.
func (c *Client) TranslateLibraryPath(ctx context.Context, from, to Config, path string) (string, error) {
	kind, p, err := c.translateLibrary(ctx, from, to, path)
	if err == nil && kind != LibraryKind {
		err = fmt.Errorf("%w: library path %q is a publication there", ErrNoTranslation, path)
	}
	return p, err
}

// translateLibrary is TranslateLibraryPath for a page that may be a
// publication in the other language: it says which kind of page it found.
func (c *Client) translateLibrary(ctx context.Context, from, to Config, path string) (kind, out string, err error) {
	path = strings.Trim(path, "/")
	if path == "" || from == to {
		return LibraryKind, path, nil
	}
	none := fmt.Errorf("%w: library path %q", ErrNoTranslation, path)
	segs := strings.Split(path, "/")
	src, err := c.Library(ctx, from, LibraryKind, "")
	if err != nil {
		return "", "", err
	}
	dst, err := c.Library(ctx, to, LibraryKind, "")
	if err != nil {
		return "", "", err
	}
	if rootSlug(src) != segs[0] || rootSlug(dst) == "" {
		return "", "", none
	}
	srcPath, at := segs[0], LibraryCard{Kind: LibraryKind, Path: rootSlug(dst)}
	for i, seg := range segs[1:] {
		if at.Kind != LibraryKind {
			return "", "", none // nothing below a publication
		}
		if i > 0 {
			if src, err = c.Library(ctx, from, LibraryKind, srcPath); err != nil {
				return "", "", err
			}
			if dst, err = c.Library(ctx, to, LibraryKind, at.Path); err != nil {
				return "", "", err
			}
		}
		want := srcPath + "/" + seg
		srcKeys, _ := libraryKeys(src)
		_, dstCards := libraryKeys(dst)
		key, ok := srcKeys[want]
		if !ok {
			return "", "", none
		}
		if at, ok = dstCards[key]; !ok {
			return "", "", none
		}
		srcPath = want
	}
	return at.Kind, at.Path, nil
}

// ErrNoTranslation: a library page of one language has no counterpart in
// another.
var ErrNoTranslation = fmt.Errorf("no counterpart in the other language")

// rootSlug is the first segment of the paths the top of the tree leads to:
// "all-publications".
func rootSlug(root LibraryPage) string {
	for _, c := range root.Cards() {
		if c.Kind == LibraryKind {
			first, _, _ := strings.Cut(c.Path, "/")
			return first
		}
	}
	return ""
}

// CanonicalLibrary reads a page of the library in cfg while naming it, and
// every library page it leads to, by the paths of canon — the English tree —
// so an address is the same in every language. path is canonical; a path of
// cfg's own language is read as it is when it names no page of canon. A page
// without a counterpart in canon keeps the path of its own language.
func (c *Client) CanonicalLibrary(ctx context.Context, cfg, canon Config, kind, path string) (LibraryPage, error) {
	path = strings.Trim(path, "/")
	if cfg == canon {
		return c.Library(ctx, cfg, kind, path)
	}
	if kind != LibraryKind {
		// a publication is named by its symbol everywhere; only the page
		// above it is named in words
		page, err := c.Library(ctx, cfg, kind, path)
		if err != nil {
			return LibraryPage{}, err
		}
		if page.Parent != nil && page.Parent.Kind == LibraryKind {
			if p, err := c.TranslateLibraryPath(ctx, cfg, canon, page.Parent.Path); err == nil {
				parent := *page.Parent
				parent.Path = p
				page.Parent = &parent
			}
		}
		return page, nil
	}

	local, localKind, canonPath, paired := path, LibraryKind, path, true
	if k, p, err := c.translateLibrary(ctx, canon, cfg, path); err == nil {
		local, localKind = p, k
	} else if p, err := c.TranslateLibraryPath(ctx, cfg, canon, path); err == nil {
		canonPath = p // a path in the page's own language
	} else {
		paired = false
	}
	if localKind != LibraryKind {
		// a shelf in English, one publication here
		return c.CanonicalLibrary(ctx, cfg, canon, localKind, local)
	}
	page, err := c.Library(ctx, cfg, LibraryKind, local)
	if err != nil || !paired {
		return page, err
	}
	canonPage, err := c.Library(ctx, canon, LibraryKind, canonPath)
	if err != nil {
		return page, nil
	}
	page.Path = canonPage.Path
	if canonPage.Parent != nil {
		parent := *canonPage.Parent
		page.Parent = &parent
	}
	localKeys, _ := libraryKeys(page)
	_, canonCards := libraryKeys(canonPage)
	groups := make([]LibraryGroup, len(page.Groups))
	for gi, g := range page.Groups {
		cards := make([]LibraryCard, len(g.Cards))
		for ci, card := range g.Cards {
			if card.Kind == LibraryKind {
				if cc, ok := canonCards[localKeys[card.Path]]; ok {
					card.Kind, card.Path = cc.Kind, cc.Path
				}
			}
			cards[ci] = card
		}
		groups[gi] = LibraryGroup{Title: g.Title, Cards: cards}
	}
	page.Groups = groups
	return page, nil
}
