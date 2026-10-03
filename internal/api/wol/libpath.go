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
// else the kind of publication it holds and the year in its title (w 2024),
// and among siblings alike in both, their order (the public before the study
// edition).
//
// Addresses are English paths. A page that the English library does not
// carry — an older book kept in German and French only — is addressed below
// its nearest English page by that same name: its symbol
// (all-publications/books/fm), or its kind and year (…/dx-1945).

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

// keySegment writes a card's key as a path segment, for a page with no
// English counterpart: "sym:fm#0" is fm, "library:dx:1945#0" dx-1945, the
// second of two alike -2.
func keySegment(key string) string {
	base, n, _ := strings.Cut(key, "#")
	var seg string
	if sym, ok := strings.CutPrefix(base, "sym:"); ok {
		seg = strings.ReplaceAll(sym, "/", "-")
	} else {
		var parts []string
		for _, p := range strings.Split(base, ":")[1:] {
			if p != "" {
				parts = append(parts, p)
			}
		}
		seg = strings.Join(parts, "-")
	}
	if n != "" && n != "0" {
		seg += "-" + n
	}
	if seg == "" {
		seg = "page"
	}
	return seg
}

// resolveLibrary finds the page of cfg's library that a canonical path names,
// walking both trees down side by side: an English segment by its English
// counterpart, and below the last page the English library carries, a key
// segment among cfg's own pages. It says which kind of page it found — a shelf
// in English may be one publication in cfg — and whether the English library
// carries the page.
func (c *Client) resolveLibrary(ctx context.Context, canon, cfg Config, path string) (kind, local string, inCanon bool, err error) {
	path = strings.Trim(path, "/")
	if path == "" {
		return LibraryKind, "", true, nil
	}
	none := fmt.Errorf("%w: library path %q", ErrNoTranslation, path)
	segs := strings.Split(path, "/")
	src, err := c.Library(ctx, canon, LibraryKind, "")
	if err != nil {
		return "", "", false, err
	}
	dst, err := c.Library(ctx, cfg, LibraryKind, "")
	if err != nil {
		return "", "", false, err
	}
	if rootSlug(src) != segs[0] || rootSlug(dst) == "" {
		return "", "", false, none
	}
	srcPath, at, inCanon := segs[0], LibraryCard{Kind: LibraryKind, Path: rootSlug(dst)}, true
	for i, seg := range segs[1:] {
		if at.Kind != LibraryKind {
			return "", "", false, none // nothing below a publication
		}
		if i > 0 {
			if inCanon {
				if src, err = c.Library(ctx, canon, LibraryKind, srcPath); err != nil {
					return "", "", false, err
				}
			}
			if dst, err = c.Library(ctx, cfg, LibraryKind, at.Path); err != nil {
				return "", "", false, err
			}
		}
		want := srcPath + "/" + seg
		dstKeys, dstCards := libraryKeys(dst)
		var key string
		if inCanon {
			srcKeys, _ := libraryKeys(src)
			if k, ok := srcKeys[want]; ok {
				key = k
			} else {
				inCanon = false
			}
		}
		if !inCanon {
			for _, k := range dstKeys {
				if keySegment(k) == seg {
					key = k
					break
				}
			}
		}
		next, ok := dstCards[key]
		if key == "" || !ok {
			return "", "", false, none
		}
		at, srcPath = next, want
	}
	return at.Kind, at.Path, inCanon, nil
}

// canonicalLibraryPath names a page of cfg's library by its canonical path,
// the way resolveLibrary reads it back.
func (c *Client) canonicalLibraryPath(ctx context.Context, cfg, canon Config, local string) (string, error) {
	local = strings.Trim(local, "/")
	if local == "" {
		return "", nil
	}
	none := fmt.Errorf("%w: library path %q", ErrNoTranslation, local)
	segs := strings.Split(local, "/")
	src, err := c.Library(ctx, cfg, LibraryKind, "")
	if err != nil {
		return "", err
	}
	dst, err := c.Library(ctx, canon, LibraryKind, "")
	if err != nil {
		return "", err
	}
	if rootSlug(src) != segs[0] || rootSlug(dst) == "" {
		return "", none
	}
	srcPath, out, inCanon := segs[0], rootSlug(dst), true
	for i, seg := range segs[1:] {
		if i > 0 {
			if src, err = c.Library(ctx, cfg, LibraryKind, srcPath); err != nil {
				return "", err
			}
			if inCanon {
				if dst, err = c.Library(ctx, canon, LibraryKind, out); err != nil {
					return "", err
				}
			}
		}
		want := srcPath + "/" + seg
		srcKeys, _ := libraryKeys(src)
		key, ok := srcKeys[want]
		if !ok {
			return "", none
		}
		if inCanon {
			_, dstCards := libraryKeys(dst)
			if cc, ok := dstCards[key]; ok && cc.Kind == LibraryKind {
				out, srcPath = cc.Path, want
				continue
			}
			inCanon = false
		}
		out, srcPath = out+"/"+keySegment(key), want
	}
	return out, nil
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

// CanonicalRoot is the first segment of every canonical library path: the
// top of the tree, which wol calls all-publications in English.
const CanonicalRoot = "publications"

// CanonicalLibrary reads a page of the library in cfg while naming it, and
// every library page it leads to, by its canonical path — the path of canon,
// the English tree, or below the last page that tree carries, the key
// segments of cfg's own pages — so an address is the same in every language.
// Canonical paths begin with CanonicalRoot (publications/books/fm). A path
// that names no page of cfg's tree is ErrNoTranslation.
func (c *Client) CanonicalLibrary(ctx context.Context, cfg, canon Config, kind, path string) (LibraryPage, error) {
	path = strings.Trim(path, "/")
	if kind == LibraryKind && path != "" {
		rest, ok := strings.CutPrefix(path+"/", CanonicalRoot+"/")
		if !ok {
			return LibraryPage{}, fmt.Errorf("%w: library path %q", ErrNoTranslation, path)
		}
		root, err := c.Library(ctx, canon, LibraryKind, "")
		if err != nil {
			return LibraryPage{}, err
		}
		path = strings.TrimSuffix(rootSlug(root)+"/"+rest, "/")
	}
	page, err := c.canonicalLibrary(ctx, cfg, canon, kind, path)
	if err != nil {
		return LibraryPage{}, err
	}
	root, err := c.Library(ctx, canon, LibraryKind, "")
	if err != nil {
		return LibraryPage{}, err
	}
	rename := func(p string) string {
		if p == rootSlug(root) {
			return CanonicalRoot
		}
		if rest, ok := strings.CutPrefix(p, rootSlug(root)+"/"); ok {
			return CanonicalRoot + "/" + rest
		}
		return p
	}
	if page.Kind == LibraryKind {
		page.Path = rename(page.Path)
	}
	if page.Parent != nil && page.Parent.Kind == LibraryKind {
		parent := *page.Parent
		parent.Path = rename(parent.Path)
		page.Parent = &parent
	}
	groups := make([]LibraryGroup, len(page.Groups))
	for gi, g := range page.Groups {
		cards := make([]LibraryCard, len(g.Cards))
		for ci, card := range g.Cards {
			if card.Kind == LibraryKind {
				card.Path = rename(card.Path)
			}
			cards[ci] = card
		}
		groups[gi] = LibraryGroup{Title: g.Title, Cards: cards}
	}
	page.Groups = groups
	return page, nil
}

func (c *Client) canonicalLibrary(ctx context.Context, cfg, canon Config, kind, path string) (LibraryPage, error) {
	path = strings.Trim(path, "/")
	if cfg == canon {
		// wol answers a library path it does not know with a page above
		// it rather than an error; walking the tree tells
		if kind == LibraryKind {
			if _, _, _, err := c.resolveLibrary(ctx, canon, cfg, path); err != nil {
				return LibraryPage{}, err
			}
		}
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
			parent := *page.Parent
			if p, err := c.canonicalLibraryPath(ctx, cfg, canon, parent.Path); err == nil {
				parent.Path = p
				page.Parent = &parent
			} else {
				page.Parent = nil
			}
		}
		return page, nil
	}

	localKind, local, inCanon, err := c.resolveLibrary(ctx, canon, cfg, path)
	if err != nil {
		return LibraryPage{}, err
	}
	if localKind != LibraryKind {
		// a shelf in English, one publication here
		return c.canonicalLibrary(ctx, cfg, canon, localKind, local)
	}
	page, err := c.Library(ctx, cfg, LibraryKind, local)
	if err != nil {
		return LibraryPage{}, err
	}
	var canonCards map[string]LibraryCard
	page.Path, page.Parent = path, nil
	below := path // where a page without an English counterpart is named
	if inCanon {
		canonPage, err := c.Library(ctx, canon, LibraryKind, path)
		if err != nil {
			return LibraryPage{}, err
		}
		page.Path = canonPage.Path
		if canonPage.Parent != nil {
			parent := *canonPage.Parent
			page.Parent = &parent
		}
		_, canonCards = libraryKeys(canonPage)
		if below == "" {
			below = rootSlug(canonPage)
		}
	} else if up, _, ok := cutLast(path); ok {
		page.Parent = &LibraryCard{Kind: LibraryKind, Path: up}
	}
	localKeys, _ := libraryKeys(page)
	groups := make([]LibraryGroup, len(page.Groups))
	for gi, g := range page.Groups {
		cards := make([]LibraryCard, len(g.Cards))
		for ci, card := range g.Cards {
			if card.Kind == LibraryKind {
				key := localKeys[card.Path]
				if cc, ok := canonCards[key]; ok {
					card.Kind, card.Path = cc.Kind, cc.Path
				} else {
					card.Path = below + "/" + keySegment(key)
				}
			}
			cards[ci] = card
		}
		groups[gi] = LibraryGroup{Title: g.Title, Cards: cards}
	}
	page.Groups = groups
	return page, nil
}

// cutLast splits the last segment off a path.
func cutLast(path string) (up, last string, ok bool) {
	i := strings.LastIndexByte(path, '/')
	if i < 0 {
		return "", "", false
	}
	return path[:i], path[i+1:], true
}
