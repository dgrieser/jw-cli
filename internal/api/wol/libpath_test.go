package wol

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// libCard is one card of a made-up library page: path below the language
// part, title, icon, and the publication its cover shows.
type libCard struct{ href, title, icon, thumb string }

func libraryHTML(locale, lp, title, back string, cards ...libCard) http.HandlerFunc {
	var b strings.Builder
	fmt.Fprintf(&b, `<html><body><article id="article"><h1><a class="backNav" href="/%s/wol/library/r1/%s/%s">%s</a></h1><ul class="directory">`,
		locale, lp, back, title)
	for _, c := range cards {
		thumb := fmt.Sprintf(`<span class="cardThumbnailImage icon-%s"></span>`, c.icon)
		if c.thumb != "" {
			thumb = fmt.Sprintf(`<img class="cardThumbnailImage icon-%s" src="/%s/wol/publication/r1/%s/%s/thumbnail"/>`, c.icon, locale, lp, c.thumb)
		}
		cmd, href := "library", c.href
		if sym, ok := strings.CutPrefix(c.href, "pub:"); ok {
			cmd, href = "publication", sym
		}
		fmt.Fprintf(&b, `<li class="row card"><a class="cardContainer" href="/%s/wol/%s/r1/%s/%s">%s<div class="cardLine1">%s</div></a></li>`,
			locale, cmd, lp, href, thumb, c.title)
	}
	b.WriteString(`</ul></article></body></html>`)
	return func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(b.String())) }
}

var cfgEn1 = Config{Locale: "en", Rsconf: "r1", Lp: "lp-e"}
var cfgDe1 = Config{Locale: "de", Rsconf: "r1", Lp: "lp-x"}

// translateClient serves the same small tree in English and German, the
// German one ordered by its own titles.
func translateClient(t *testing.T) *Client {
	mux := http.NewServeMux()
	en := func(path, title, back string, cards ...libCard) {
		mux.HandleFunc("/en/wol/library/r1/lp-e"+path, libraryHTML("en", "lp-e", title, back, cards...))
	}
	de := func(path, title, back string, cards ...libCard) {
		mux.HandleFunc("/de/wol/library/r1/lp-x"+path, libraryHTML("de", "lp-x", title, back, cards...))
	}
	en("", "", "",
		libCard{"all-publications/watchtower", "Watchtower", "w", ""},
		libCard{"all-publications/books", "Books", "bk", ""},
		libCard{"all-publications/tracts", "Tracts", "trct", ""},
		libCard{"all-publications/glossary", "Glossary", "dict", ""})
	de("", "", "",
		libCard{"alle-publikationen/traktate", "Traktate", "trct", ""},
		libCard{"alle-publikationen/b%C3%BCcher", "Bücher", "bk", ""},
		libCard{"alle-publikationen/wachtturm", "Wachtturm", "w", ""})
	// a tract is a shelf of its editions in English, one publication in German
	en("/all-publications/tracts", "Tracts", "all-publications",
		libCard{"all-publications/tracts/enjoy-family-life-t-21", "Enjoy Family Life (T-21)", "trct", ""})
	de("/alle-publikationen/traktate", "Traktate", "alle-publikationen",
		libCard{"pub:T-21", "Am Familienleben Freude finden (T-21)", "trct", ""})
	en("/all-publications/books", "Books", "all-publications",
		libCard{"all-publications/books/all-scripture-si", "“All Scripture” (si)", "bk", ""},
		libCard{"all-publications/books/reasoning-rs", "Reasoning (rs)", "bk", ""})
	de("/alle-publikationen/bücher", "Bücher", "alle-publikationen",
		libCard{"alle-publikationen/b%C3%BCcher/schlussfolgern-rs", "Schlussfolgern (rs)", "bk", ""},
		libCard{"alle-publikationen/b%C3%BCcher/die-ganze-schrift-si", "„Die ganze Schrift“ (si)", "bk", ""},
		libCard{"alle-publikationen/b%C3%BCcher/geheimnis-buch-fm", "„Geheimnis“-Buch (fm)", "bk", ""})
	de("/alle-publikationen/bücher/geheimnis-buch-fm", "„Geheimnis“-Buch", "alle-publikationen/b%C3%BCcher")
	en("/all-publications/watchtower", "Watchtower", "all-publications",
		libCard{"all-publications/watchtower/the-watchtower-2025", "The Watchtower—2025", "w", ""},
		libCard{"all-publications/watchtower/the-watchtower-2024", "The Watchtower—2024", "w", ""})
	de("/alle-publikationen/wachtturm", "Wachtturm", "alle-publikationen",
		libCard{"alle-publikationen/wachtturm/der-wachtturm-2025", "Der Wachtturm 2025", "w", ""},
		libCard{"alle-publikationen/wachtturm/der-wachtturm-2024", "Der Wachtturm 2024", "w", ""})
	en("/all-publications/watchtower/the-watchtower-2024", "The Watchtower—2024", "all-publications/watchtower",
		libCard{"all-publications/watchtower/the-watchtower-2024/public-edition", "Public Edition", "w", ""},
		libCard{"all-publications/watchtower/the-watchtower-2024/study-edition", "Study Edition", "w", ""})
	de("/alle-publikationen/wachtturm/der-wachtturm-2024", "Der Wachtturm 2024", "alle-publikationen/wachtturm",
		libCard{"alle-publikationen/wachtturm/der-wachtturm-2024/%C3%B6ffentlichkeitsausgabe", "Öffentlichkeitsausgabe", "w", ""},
		libCard{"alle-publikationen/wachtturm/der-wachtturm-2024/studienausgabe", "Studienausgabe", "w", ""})
	en("/all-publications/watchtower/the-watchtower-2024/study-edition", "Study Edition", "all-publications/watchtower/the-watchtower-2024",
		libCard{"all-publications/watchtower/the-watchtower-2024/study-edition/march", "March", "w", "w24/2024/3"},
		libCard{"all-publications/watchtower/the-watchtower-2024/study-edition/may", "May", "w", "w24/2024/5"})
	de("/alle-publikationen/wachtturm/der-wachtturm-2024/studienausgabe", "Studienausgabe", "alle-publikationen/wachtturm/der-wachtturm-2024",
		libCard{"alle-publikationen/wachtturm/der-wachtturm-2024/studienausgabe/m%C3%A4rz", "März", "w", "w24/2024/3"},
		libCard{"alle-publikationen/wachtturm/der-wachtturm-2024/studienausgabe/mai", "Mai", "w", "w24/2024/5"})
	en("/all-publications/watchtower/the-watchtower-2024/study-edition/may", "May", "all-publications/watchtower/the-watchtower-2024/study-edition")
	de("/alle-publikationen/wachtturm/der-wachtturm-2024/studienausgabe/mai", "Mai", "alle-publikationen/wachtturm/der-wachtturm-2024/studienausgabe")
	return testClient(t, mux)
}

func TestResolveLibrary(t *testing.T) {
	c := translateClient(t)
	ctx := context.Background()
	for en, de := range map[string]string{
		"all-publications":                                                  "alle-publikationen",
		"all-publications/books":                                            "alle-publikationen/bücher",
		"all-publications/books/all-scripture-si":                           "alle-publikationen/bücher/die-ganze-schrift-si",
		"all-publications/books/reasoning-rs":                               "alle-publikationen/bücher/schlussfolgern-rs",
		"all-publications/watchtower/the-watchtower-2024/public-edition":    "alle-publikationen/wachtturm/der-wachtturm-2024/öffentlichkeitsausgabe",
		"all-publications/watchtower/the-watchtower-2024/study-edition/may": "alle-publikationen/wachtturm/der-wachtturm-2024/studienausgabe/mai",
		// a book the English library does not carry, by its symbol
		"all-publications/books/fm": "alle-publikationen/bücher/geheimnis-buch-fm",
	} {
		kind, got, _, err := c.resolveLibrary(ctx, cfgEn1, cfgDe1, en)
		if err != nil || kind != LibraryKind || got != de {
			t.Errorf("resolve %s = %s %q, %v; want %s", en, kind, got, err, de)
		}
		back, err := c.canonicalLibraryPath(ctx, cfgDe1, cfgEn1, de)
		if err != nil || back != en {
			t.Errorf("canonical %s = %q, %v; want %s", de, back, err, en)
		}
	}
	if kind, p, _, err := c.resolveLibrary(ctx, cfgEn1, cfgDe1, "all-publications/tracts/enjoy-family-life-t-21"); err != nil || kind != PublicationKind || p != "T-21" {
		t.Errorf("tract = %s %q, %v", kind, p, err)
	}
	for _, missing := range []string{"all-publications/glossary", "all-publications/books/xx", "alle-publikationen/bücher"} {
		if _, _, _, err := c.resolveLibrary(ctx, cfgEn1, cfgDe1, missing); !errors.Is(err, ErrNoTranslation) {
			t.Errorf("%s: err = %v", missing, err)
		}
	}
}

func TestCanonicalLibrary(t *testing.T) {
	c := translateClient(t)
	ctx := context.Background()
	page, err := c.CanonicalLibrary(ctx, cfgDe1, cfgEn1, LibraryKind, "publications/watchtower/the-watchtower-2024/study-edition")
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "Studienausgabe" || page.Path != "publications/watchtower/the-watchtower-2024/study-edition" {
		t.Errorf("page = %q at %q", page.Title, page.Path)
	}
	if p := page.Parent; p == nil || p.Path != "publications/watchtower/the-watchtower-2024" {
		t.Errorf("parent = %+v", p)
	}
	cards := page.Cards()
	if len(cards) != 2 || cards[0].Title != "März" || cards[0].Path != "publications/watchtower/the-watchtower-2024/study-edition/march" {
		t.Errorf("cards = %+v", cards)
	}

	// wol's own name for the top is no address
	if _, err := c.CanonicalLibrary(ctx, cfgDe1, cfgEn1, LibraryKind, "all-publications/books"); !errors.Is(err, ErrNoTranslation) {
		t.Errorf("all-publications path: err = %v", err)
	}
	// a path in the page's own language names no page
	if _, err := c.CanonicalLibrary(ctx, cfgDe1, cfgEn1, LibraryKind, "alle-publikationen/bücher"); !errors.Is(err, ErrNoTranslation) {
		t.Errorf("own-language path: err = %v", err)
	}

	// a book only the German library carries, under the English shelf
	books, err := c.CanonicalLibrary(ctx, cfgDe1, cfgEn1, LibraryKind, "publications/books")
	if err != nil {
		t.Fatal(err)
	}
	if last := books.Cards()[len(books.Cards())-1]; last.Title != "„Geheimnis“-Buch (fm)" || last.Path != "publications/books/fm" {
		t.Errorf("German-only book = %+v", last)
	}
	fm, err := c.CanonicalLibrary(ctx, cfgDe1, cfgEn1, LibraryKind, "publications/books/fm")
	if err != nil {
		t.Fatal(err)
	}
	if fm.Title != "„Geheimnis“-Buch" || fm.Path != "publications/books/fm" || fm.Parent == nil || fm.Parent.Path != "publications/books" {
		t.Errorf("fm page = %q at %q, parent %+v", fm.Title, fm.Path, fm.Parent)
	}
	if _, err := c.CanonicalLibrary(ctx, cfgEn1, cfgEn1, LibraryKind, "publications/books/fm"); err == nil {
		t.Error("the English library read a book it does not carry")
	}

	// the root keeps English paths for its categories
	root, err := c.CanonicalLibrary(ctx, cfgDe1, cfgEn1, LibraryKind, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := root.Cards()[1]; got.Title != "Bücher" || got.Path != "publications/books" {
		t.Errorf("root card = %+v", got)
	}
}

func TestCardKey(t *testing.T) {
	for _, tc := range []struct {
		card LibraryCard
		want string
	}{
		{LibraryCard{Kind: LibraryKind, Icon: "mwb", Title: "Life and Ministry Meeting Workbook—2026"}, "library:mwb:2026"},
		{LibraryCard{Kind: LibraryKind, Icon: "mwb", Title: "Cahier Vie et ministère (2026)"}, "library:mwb:2026"},
		{LibraryCard{Kind: LibraryKind, Icon: "bk", Title: "“All Scripture” (si)"}, "sym:si"},
		{LibraryCard{Kind: PublicationKind, Path: "si"}, "sym:si"},
		{LibraryCard{Kind: LibraryKind, Icon: "w", Title: "Mai", Thumbnail: "https://wol.jw.org/de/wol/publication/r10/lp-x/w24/2024/5/thumbnail"}, "sym:w24/2024/5"},
		{LibraryCard{Kind: LibraryKind, Icon: "w", Title: "Study Edition"}, "library:w:"},
	} {
		if got := cardKey(tc.card); got != tc.want {
			t.Errorf("cardKey(%q) = %q, want %q", tc.card.Title, got, tc.want)
		}
	}
}

func TestKeySegment(t *testing.T) {
	for key, want := range map[string]string{
		"sym:fm#0":          "fm",
		"sym:w24/2024/5#0":  "w24-2024-5",
		"library:dx:1945#0": "dx-1945",
		"library:mwb:#0":    "mwb",
		"library:w:#1":      "w-1",
	} {
		if got := keySegment(key); got != want {
			t.Errorf("keySegment(%q) = %q, want %q", key, got, want)
		}
	}
}
