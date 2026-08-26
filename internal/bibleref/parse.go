// Package bibleref parses human bible references ("Matthew 24:14",
// "mt 24:3-14", "Pr 8-9", "Pr 8:1-9:10", "1co 13:4,7-8; Joh 3:16", "40 24:14")
// into book/chapter/verse structures, with optional localized book-name tables.
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
// verse span within one chapter. A reference running from one chapter into
// another is parsed into one Ref per chapter, since a chapter is what the
// library serves.
type Ref struct {
	Book       int `json:"book"` // 1-66
	Chapter    int `json:"chapter"`
	VerseStart int `json:"verseStart,omitempty"` // 0 = whole chapter
	VerseEnd   int `json:"verseEnd,omitempty"`   // == VerseStart for single verses
}

// LastVerse is the end of a chapter in a reference that runs past it: how far
// "Pr 8:5-9:10" reaches into chapter 8 is only known once the chapter is read,
// and every verse of it from the fifth on is what was asked for either way.
const LastVerse = 999

// maxChapter is the highest chapter number any book has (Psalm 150), so a
// chapter range is refused before it is spent on requests for chapters that
// cannot exist.
const maxChapter = 150

// RunsToChapterEnd reports whether the reference was written as running past the
// chapter, so its end is whatever verse the chapter ends on.
func (r Ref) RunsToChapterEnd() bool { return r.VerseEnd >= LastVerse }

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
	case r.RunsToChapterEnd():
		// the last verse is not known here, and "ff." is how a reference says
		// "and on to the end" without naming it
		return fmt.Sprintf("%s %d:%dff.", name, r.Chapter, r.VerseStart)
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
	for part := range strings.SplitSeq(input, ";") {
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

// parseOne parses a single reference: a book name, then the comma-separated
// spans of chapters or verses that follow it.
func parseOne(s string, t *Table) ([]Ref, error) {
	book, rest, err := splitBook(s, t)
	if err != nil {
		return nil, err
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return nil, fmt.Errorf("missing chapter in %q (whole-book references are not supported)", s)
	}
	var refs []Ref
	// the chapter a bare number belongs to: none until a span names verses, so
	// "Pr 8-9, 11" is chapters and "Pr 8:8, 9" is verses of chapter 8
	chapter := 0
	for span := range strings.SplitSeq(rest, ",") {
		span = strings.TrimSpace(span)
		if span == "" {
			continue
		}
		rs, last, err := parseSpan(book, span, chapter, s)
		if err != nil {
			return nil, err
		}
		refs = append(refs, rs...)
		chapter = last
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("missing chapter in %q", s)
	}
	return refs, nil
}

// parseSpan parses one comma-separated span of a reference — "8", "8-9", "8:1",
// "8:1-9", "8:1-9:10" — and returns the refs it covers together with the chapter
// a bare number after it would belong to (zero when the span named no verse).
func parseSpan(book int, span string, chapter int, whole string) ([]Ref, int, error) {
	lo, hi, ranged := strings.Cut(span, "-")
	fromChapter, fromVerse, err := parsePoint(lo, chapter, whole)
	if err != nil {
		return nil, 0, err
	}
	if !ranged {
		if fromVerse == 0 {
			return []Ref{{Book: book, Chapter: fromChapter}}, 0, nil
		}
		return []Ref{{Book: book, Chapter: fromChapter, VerseStart: fromVerse, VerseEnd: fromVerse}},
			fromChapter, nil
	}
	// the end of a verse span is a verse of the chapter its start named, unless
	// it names one of its own: "8:1-9" ends in chapter 8, "8:1-9:10" in chapter 9
	ends := chapter
	if fromVerse > 0 {
		ends = fromChapter
	}
	toChapter, toVerse, err := parsePoint(hi, ends, whole)
	if err != nil {
		return nil, 0, err
	}
	if fromVerse == 0 && toVerse == 0 {
		refs, err := chapterRange(book, fromChapter, toChapter, whole)
		return refs, 0, err
	}
	refs, err := verseRange(book, fromChapter, max(fromVerse, 1), toChapter, toVerse, whole)
	return refs, toChapter, err
}

// parsePoint reads one end of a span: "8:1" is a chapter and a verse, and a bare
// number is a verse of chapter when there is one and a chapter otherwise.
func parsePoint(s string, chapter int, whole string) (int, int, error) {
	s = strings.TrimSpace(s)
	head, tail, hasVerse := strings.Cut(s, ":")
	first, err := number(head)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid chapter or verse %q in %q", s, whole)
	}
	if hasVerse {
		verse, err := number(tail)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid verse %q in %q", s, whole)
		}
		return first, verse, nil
	}
	if chapter > 0 {
		return chapter, first, nil
	}
	return first, 0, nil
}

// number reads a positive number, which is what every part of a reference is.
func number(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("not a number: %q", s)
	}
	return n, nil
}

// chapterRange covers whole chapters: "Pr 8-9".
func chapterRange(book, from, to int, whole string) ([]Ref, error) {
	if to < from {
		return nil, fmt.Errorf("invalid chapter range %d-%d in %q", from, to, whole)
	}
	if to > maxChapter {
		return nil, fmt.Errorf("chapter %d does not exist (in %q)", to, whole)
	}
	var refs []Ref
	for c := from; c <= to; c++ {
		refs = append(refs, Ref{Book: book, Chapter: c})
	}
	return refs, nil
}

// verseRange covers a verse span, which becomes one Ref per chapter it runs
// through: the first chapter from its start verse to wherever it ends, the
// chapters in between whole, and the last one up to its end verse.
func verseRange(book, fromChapter, fromVerse, toChapter, toVerse int, whole string) ([]Ref, error) {
	if toChapter < fromChapter || (toChapter == fromChapter && toVerse < fromVerse) {
		return nil, fmt.Errorf("invalid verse range %d:%d-%d:%d in %q",
			fromChapter, fromVerse, toChapter, toVerse, whole)
	}
	if toChapter > maxChapter {
		return nil, fmt.Errorf("chapter %d does not exist (in %q)", toChapter, whole)
	}
	if toChapter == fromChapter {
		return []Ref{{Book: book, Chapter: fromChapter, VerseStart: fromVerse, VerseEnd: toVerse}}, nil
	}
	var refs []Ref
	if fromVerse > 1 {
		refs = append(refs, Ref{Book: book, Chapter: fromChapter, VerseStart: fromVerse, VerseEnd: LastVerse})
	} else {
		// from the first verse on is the whole chapter, and reads as one
		refs = append(refs, Ref{Book: book, Chapter: fromChapter})
	}
	for c := fromChapter + 1; c < toChapter; c++ {
		refs = append(refs, Ref{Book: book, Chapter: c})
	}
	refs = append(refs, Ref{Book: book, Chapter: toChapter, VerseStart: 1, VerseEnd: toVerse})
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
