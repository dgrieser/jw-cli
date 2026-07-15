// Package bibleref parses human bible references ("Matthew 24:14",
// "mt 24:3-14", "1co 13:4,7-8; Joh 3:16", "40 24:14") into book/chapter/verse
// structures, with optional localized book-name tables.
package bibleref

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Ref is one contiguous reference: a whole chapter (VerseStart == 0) or a
// verse span within one chapter.
type Ref struct {
	Book       int `json:"book"` // 1-66
	Chapter    int `json:"chapter"`
	VerseStart int `json:"verseStart,omitempty"` // 0 = whole chapter
	VerseEnd   int `json:"verseEnd,omitempty"`   // == VerseStart for single verses
}

func (r Ref) IsWholeChapter() bool { return r.VerseStart == 0 }

// String renders the reference with English book names.
func (r Ref) String() string {
	name := "?"
	if r.Book >= 1 && r.Book <= 66 {
		name = bookNames[r.Book][0]
	}
	switch {
	case r.VerseStart == 0:
		return fmt.Sprintf("%s %d", name, r.Chapter)
	case r.VerseEnd > r.VerseStart:
		return fmt.Sprintf("%s %d:%d-%d", name, r.Chapter, r.VerseStart, r.VerseEnd)
	default:
		return fmt.Sprintf("%s %d:%d", name, r.Chapter, r.VerseStart)
	}
}

// Table resolves book names to numbers. English names/abbreviations are
// always available; localized names can be merged in.
type Table struct {
	byName map[string]int
	names  [67]string // display names (localized when merged)
}

// English returns the base table.
func English() *Table {
	t := &Table{byName: map[string]int{}}
	for num := 1; num <= 66; num++ {
		for i, name := range bookNames[num] {
			key := normalizeName(name)
			t.byName[key] = num
			if i == 0 {
				t.names[num] = name
				// full names also match without spaces ("songofsolomon")
				t.byName[strings.ReplaceAll(key, " ", "")] = num
			}
		}
	}
	return t
}

// Merge adds localized names (book number -> display name plus aliases).
// Merged names win for display; lookups keep both.
func (t *Table) Merge(localized map[int][]string) {
	for num, names := range localized {
		if num < 1 || num > 66 {
			continue
		}
		for i, n := range names {
			key := normalizeName(n)
			if key == "" {
				continue
			}
			t.byName[key] = num
			t.byName[strings.ReplaceAll(key, " ", "")] = num
			if i == 0 {
				t.names[num] = n
			}
		}
	}
}

// Name returns the display name of a book number.
func (t *Table) Name(book int) string {
	if book >= 1 && book <= 66 {
		return t.names[book]
	}
	return fmt.Sprintf("Book %d", book)
}

// Lookup resolves a normalized book-name token, trying exact match then an
// unambiguous prefix match.
func (t *Table) Lookup(name string) (int, bool) {
	key := normalizeName(name)
	if n, ok := t.byName[key]; ok {
		return n, true
	}
	if n, ok := t.byName[strings.ReplaceAll(key, " ", "")]; ok {
		return n, true
	}
	// unambiguous prefix of a known name
	found, book := 0, 0
	for k, n := range t.byName {
		if strings.HasPrefix(k, key) {
			if book != n {
				found++
				book = n
			}
		}
	}
	if found == 1 {
		return book, true
	}
	return 0, false
}

var stripMarks = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// normalizeName lowercases, strips periods/diacritics, normalizes roman
// ordinals (i/ii/iii -> 1/2/3), and collapses whitespace.
func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, ".", "")
	if out, _, err := transform.String(stripMarks, s); err == nil {
		s = out
	}
	fields := strings.Fields(s)
	if len(fields) > 1 {
		switch fields[0] {
		case "i":
			fields[0] = "1"
		case "ii":
			fields[0] = "2"
		case "iii":
			fields[0] = "3"
		}
	}
	return strings.Join(fields, " ")
}

// Parse parses a reference string, possibly containing several references
// separated by ';'. Verse lists with ',' expand into separate Refs.
func Parse(input string, t *Table) ([]Ref, error) {
	var refs []Ref
	for _, part := range strings.Split(input, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		rs, err := parseOne(part, t)
		if err != nil {
			return nil, err
		}
		refs = append(refs, rs...)
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("empty bible reference")
	}
	return refs, nil
}

func parseOne(s string, t *Table) ([]Ref, error) {
	// split off the trailing chapter[:verses] part: find last token starting
	// with a digit that isn't the whole string's leading ordinal
	book, rest, err := splitBook(s, t)
	if err != nil {
		return nil, err
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return nil, fmt.Errorf("missing chapter in %q (whole-book references are not supported)", s)
	}
	chapterPart, versePart, hasVerses := strings.Cut(rest, ":")
	chapter, err := strconv.Atoi(strings.TrimSpace(chapterPart))
	if err != nil || chapter < 1 {
		return nil, fmt.Errorf("invalid chapter %q in %q", chapterPart, s)
	}
	if !hasVerses {
		return []Ref{{Book: book, Chapter: chapter}}, nil
	}
	var refs []Ref
	for _, span := range strings.Split(versePart, ",") {
		span = strings.TrimSpace(span)
		lo, hi, found := strings.Cut(span, "-")
		start, err := strconv.Atoi(strings.TrimSpace(lo))
		if err != nil || start < 1 {
			return nil, fmt.Errorf("invalid verse %q in %q", span, s)
		}
		end := start
		if found {
			end, err = strconv.Atoi(strings.TrimSpace(hi))
			if err != nil || end < start {
				return nil, fmt.Errorf("invalid verse range %q in %q", span, s)
			}
		}
		refs = append(refs, Ref{Book: book, Chapter: chapter, VerseStart: start, VerseEnd: end})
	}
	return refs, nil
}

// splitBook separates the book name from the chapter/verse tail.
func splitBook(s string, t *Table) (int, string, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0, "", fmt.Errorf("empty reference")
	}
	// try the longest book-name prefix first: "song of solomon 2:1"
	for i := len(fields) - 1; i >= 1; i-- {
		name := strings.Join(fields[:i], " ")
		if book, ok := t.Lookup(name); ok {
			return book, strings.Join(fields[i:], " "), nil
		}
	}
	// numeric book: "40 24:14"
	if n, err := strconv.Atoi(fields[0]); err == nil && n >= 1 && n <= 66 && len(fields) > 1 {
		return n, strings.Join(fields[1:], " "), nil
	}
	// glued forms like "joh3:16"
	if i := strings.IndexFunc(fields[0], unicode.IsDigit); i > 0 {
		head := fields[0][:i]
		// keep leading ordinal intact ("1co13:4" -> head "1co"? no: digit at 0)
		if book, ok := t.Lookup(head); ok {
			rest := fields[0][i:]
			if len(fields) > 1 {
				rest += " " + strings.Join(fields[1:], " ")
			}
			return book, rest, nil
		}
	}
	// ordinal-led glued forms: "1co13:4"
	if len(fields[0]) > 2 && fields[0][0] >= '1' && fields[0][0] <= '3' {
		tail := fields[0][1:]
		if i := strings.IndexFunc(tail, unicode.IsDigit); i > 0 {
			head := fields[0][:i+1]
			if book, ok := t.Lookup(head); ok {
				rest := fields[0][i+1:]
				if len(fields) > 1 {
					rest += " " + strings.Join(fields[1:], " ")
				}
				return book, rest, nil
			}
		}
	}
	return 0, "", fmt.Errorf("unknown bible book in %q (try an English name, abbreviation, or book number 1-66)", s)
}

// VerseID encodes a verse as BBCCCVVV (Genesis 1:1 = 1001001).
func VerseID(book, chapter, verse int) int {
	return book*1_000_000 + chapter*1_000 + verse
}
