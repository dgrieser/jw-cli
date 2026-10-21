package service

import (
	"cmp"
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/text/unicode/norm"

	"github.com/dgrieser/jw-cli/internal/model"
)

// The pages mark the words of a glossary's index where the bible's text comes
// up — the verses of the bible page, and those a reference unfolded anywhere
// shows, another translation's included — the first time each does in a
// page, and in every section unfolded into it; for now, nowhere else.
// The server marks them in the HTML it sends (MarkGlossary), so the browser
// needs no index of its own: a marked word is a link to its entry, which the
// page opens in place like any other.
//
// A word is found as its language writes it (Glossary.CaseSensitive,
// Suffixes). At the start of a sentence a capital says nothing, so there a
// word matched by its case — any German one, a short English one ("Will",
// "Lot") — is marked only on more evidence: as written, with no ending
// guessed ("Wundere" is no Wunder), and never seen in small letters, on the
// page or on any page marked before ("immer" makes "Immer" no priest). The
// words seen in small letters are kept per language, as many as the index
// has first words.

const (
	// the shortest term marked, and the longest one that is marked only in
	// its own case where case does not matter otherwise ("Lot", not "lot");
	// a term as short as that takes only the longer endings ("Gottes", but
	// no "Daran" for Dara)
	markMin   = 3
	markExact = 4
	// how often the index kept is looked at again for a newer one
	markRecheck = time.Minute
)

var (
	markWord = regexp.MustCompile(`[\p{L}\p{M}\p{N}]+(?:['’·-][\p{L}\p{M}\p{N}]+)*`)
	// the marks of stress and syllables some words are written with
	// ("Nikodẹmus", "Ni·co·deʹmus"), which a word is compared without
	markStress = strings.NewReplacer("̣", "", "ʹ", "", "·", "", "’", "'", "‘", "'")
)

// glossaryMarker is an index made ready for marking.
type glossaryMarker struct {
	at       int64 // when the index was read
	checked  time.Time
	cs       bool
	suffixes []string
	docURL   string
	books    []GlossaryBook
	byKey    map[string][]*markEntry // by first word, in small letters
}

// markEntry is one term and the documents explaining it, in the order of
// the glossaries.
type markEntry struct {
	exact bool // matched in its own case
	words []string
	docs  []GlossaryTerm
}

// markerState is what MarkGlossary keeps per language.
type markerState struct {
	locale string
	mu     sync.Mutex
	marker *glossaryMarker
	lower  map[string]bool // first words of terms seen in small letters
	loaded bool            // lower read from the cache
}

func (s *Service) markerState(lng model.Language) *markerState {
	st, _ := s.markers.LoadOrStore(lng.Locale, &markerState{locale: lng.Locale})
	return st.(*markerState)
}

// glossaryMarker is the index of lng ready for marking, as the cache keeps
// it; nil while there is none yet, which is then read in the background.
// An index due to be read again is used as it is meanwhile.
func (s *Service) glossaryMarker(lng model.Language) *glossaryMarker {
	st := s.markerState(lng)
	st.mu.Lock()
	m := st.marker
	fresh := m != nil && time.Since(m.checked) < markRecheck
	st.mu.Unlock()
	if fresh {
		return m
	}
	var kept keptGlossary
	if !s.Cache.Get(glossaryKey(lng), &kept) {
		s.HTTP.Go("glossary "+lng.Locale, func(ctx context.Context) { _, _ = s.Glossary(ctx, lng) })
		return m
	}
	keep := glossaryKeep
	if kept.Glossary.Partial {
		keep = glossaryRetry
	}
	if time.Since(time.Unix(kept.At, 0)) >= keep {
		s.HTTP.Go("glossary "+lng.Locale, func(ctx context.Context) { _, _ = s.Glossary(ctx, lng) })
	}
	if m == nil || m.at != kept.At {
		m = newGlossaryMarker(kept.Glossary)
		m.at = kept.At
	}
	m.checked = time.Now()
	st.mu.Lock()
	st.marker = m
	st.mu.Unlock()
	return m
}

// newGlossaryMarker groups an index by term and its terms by first word,
// the longest first.
func newGlossaryMarker(g Glossary) *glossaryMarker {
	m := &glossaryMarker{cs: g.CaseSensitive, docURL: g.DocURL, books: g.Books, byKey: map[string][]*markEntry{}}
	for _, s := range g.Suffixes {
		m.suffixes = append(m.suffixes, markStress.Replace(s))
	}
	byTerm := map[string]*markEntry{}
	for _, t := range g.Terms {
		if utf8.RuneCountInString(t.Term) < markMin || t.DocID <= 0 {
			continue
		}
		e := byTerm[t.Term]
		if e == nil {
			words := markWord.FindAllString(t.Term, -1)
			if len(words) == 0 {
				continue
			}
			e = &markEntry{exact: !m.cs && utf8.RuneCountInString(t.Term) <= markExact}
			for _, w := range words {
				e.words = append(e.words, m.norm(w, e.exact))
			}
			byTerm[t.Term] = e
			key := strings.ToLower(m.norm(words[0], true))
			m.byKey[key] = append(m.byKey[key], e)
		}
		if !slices.ContainsFunc(e.docs, func(d GlossaryTerm) bool { return d.DocID == t.DocID }) {
			e.docs = append(e.docs, t)
		}
	}
	for _, list := range m.byKey {
		slices.SortStableFunc(list, func(a, b *markEntry) int {
			if d := len(b.words) - len(a.words); d != 0 {
				return d
			}
			return len(strings.Join(b.words, " ")) - len(strings.Join(a.words, " "))
		})
	}
	return m
}

// norm is a word as words are compared: without marks of stress, one
// apostrophe for all, and in small letters where case does not matter.
func (m *glossaryMarker) norm(w string, exact bool) string {
	w = norm.NFC.String(markStress.Replace(norm.NFD.String(w)))
	if exact || m.cs {
		return w
	}
	return strings.ToLower(w)
}

// cased reports whether an entry is matched by its case, which says nothing
// at the start of a sentence.
func (m *glossaryMarker) cased(e *markEntry) bool { return m.cs || e.exact }

// keys are the first words a word read can begin a term with: itself, and
// itself without an ending the language inflects with.
func (m *glossaryMarker) keys(tok string) []string {
	k := strings.ToLower(m.norm(tok, true))
	out := []string{k}
	for _, s := range m.suffixes {
		s = strings.ToLower(s)
		if len(k) > len(s)+1 && strings.HasSuffix(k, s) && !slices.Contains(out, k[:len(k)-len(s)]) {
			out = append(out, k[:len(k)-len(s)])
		}
	}
	return out
}

// takes reports whether a term word takes an ending: a short one only the
// longer endings.
func takes(w, suffix string) bool {
	n := utf8.RuneCountInString(w)
	return n > markExact || (n == markExact && utf8.RuneCountInString(suffix) > 1)
}

// same compares a word read with a term's word; inflected says it took an
// ending to.
func (m *glossaryMarker) same(tok, w string, exact, last bool) (ok, inflected bool) {
	t := m.norm(tok, exact)
	if t == w {
		return true, false
	}
	if !last {
		return false, false
	}
	for _, s := range m.suffixes {
		if takes(w, s) && t == w+m.norm(s, exact) {
			return true, true
		}
	}
	return false, false
}

// token is a word of a text, where it is in it.
type token struct {
	word       string
	from, to   int
	afterSpace bool // only spaces since the word before, in the same text
}

// match is the entry the words from toks[i] on spell, how many it takes,
// and whether its last word took an ending.
func (m *glossaryMarker) match(toks []token, i int) (e *markEntry, n int, inflected bool) {
	var tried []*markEntry
	for _, k := range m.keys(toks[i].word) {
		for _, c := range m.byKey[k] {
			if slices.Contains(tried, c) {
				continue
			}
			tried = append(tried, c)
			n := len(c.words)
			if i+n > len(toks) {
				continue
			}
			ok, infl := true, false
			for w := 0; w < n && ok; w++ {
				if w > 0 && !toks[i+w].afterSpace {
					ok = false
					break
				}
				var in bool
				ok, in = m.same(toks[i+w].word, c.words[w], c.exact, w == n-1)
				infl = infl || in
			}
			if ok {
				return c, n, infl
			}
		}
	}
	return nil, 0, false
}

// MarkGlossary marks the words of lng's glossaries in the verses of an HTML
// fragment of a page (see verseText): each the first time it comes up in the
// fragment, and in every section body of it (div.section-body), and never in
// a document that is its
// own entry — one the fragment marks with a docId class or data-doc, or
// own. The fragment comes back as it was while lng has no index yet.
func (s *Service) MarkGlossary(lng model.Language, fragment string, own ...int) string {
	m := s.glossaryMarker(lng)
	if m == nil || len(m.byKey) == 0 || !strings.ContainsAny(fragment, "abcdefghijklmnopqrstuvwxyz") {
		return fragment
	}
	ctxNode := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), ctxNode)
	if err != nil {
		return fragment
	}
	root := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, n := range nodes {
		root.AppendChild(n)
	}
	st := s.markerState(lng)
	learned := s.learnLower(st, m, root)
	mk := &marking{m: m, lower: learned, seen: map[*html.Node]map[*markEntry]bool{}, own: own, start: true}
	mk.walk(root, root, false, false)
	var b strings.Builder
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&b, c); err != nil {
			return fragment
		}
	}
	return b.String()
}

// learnLower notes the first words of terms the fragment writes in small
// letters, keeps the ones not known before, and gives every one known.
func (s *Service) learnLower(st *markerState, m *glossaryMarker, root *html.Node) map[string]bool {
	var found []string
	var visit func(n *html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.TextNode {
			for _, w := range markWord.FindAllString(n.Data, -1) {
				r, _ := utf8.DecodeRuneInString(w)
				if !unicode.IsLower(r) {
					continue
				}
				if k := strings.ToLower(m.norm(w, true)); len(m.byKey[k]) > 0 && !slices.Contains(found, k) {
					found = append(found, k)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(root)
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.loaded {
		st.loaded = true
		st.lower = map[string]bool{}
		var kept []string
		if s.Cache.Get(st.cacheKey(), &kept) {
			for _, k := range kept {
				st.lower[k] = true
			}
		}
	}
	added := false
	for _, k := range found {
		if !st.lower[k] {
			st.lower[k] = true
			added = true
		}
	}
	if added {
		all := make([]string, 0, len(st.lower))
		for k := range st.lower {
			all = append(all, k)
		}
		slices.Sort(all)
		s.Cache.Put(st.cacheKey(), all)
	}
	out := make(map[string]bool, len(st.lower))
	for k := range st.lower {
		out[k] = true
	}
	return out
}

// marking is one fragment being marked.
type marking struct {
	m     *glossaryMarker
	lower map[string]bool
	seen  map[*html.Node]map[*markEntry]bool // by scope
	own   []int
	// start says the text read so far ends a sentence, or none began yet
	start bool
}

// markSkip are the elements whose words are never marked: links, headings,
// controls; markSkipClass the same by class, what the page says about
// itself and a book's outline.
var (
	markSkip = map[atom.Atom]bool{
		atom.A: true, atom.Summary: true, atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true,
		atom.H5: true, atom.H6: true, atom.Button: true, atom.Sup: true, atom.Script: true,
		atom.Style: true, atom.Textarea: true, atom.Select: true, atom.Input: true,
		atom.Rt: true, // an interlinear's word for word English
	}
	markSkipClass = []string{"unfold-msg", "unfold-loader", "gloss-src", "bible-outline"}
	// what begins a sentence anew; what a word is told from it by
	markBlocks = map[atom.Atom]bool{
		atom.P: true, atom.Li: true, atom.Div: true, atom.Blockquote: true, atom.Td: true, atom.Th: true,
		atom.Dd: true, atom.Dt: true, atom.Figcaption: true, atom.Br: true, atom.Section: true,
		atom.Article: true, atom.Details: true, atom.Ul: true, atom.Ol: true, atom.Table: true,
		atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	}
)

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, class string) bool {
	return slices.Contains(strings.Fields(attr(n, "class")), class)
}

func skipped(n *html.Node) bool {
	if markSkip[n.DataAtom] || hasAttr(n, "data-ui") {
		return true
	}
	return slices.ContainsFunc(markSkipClass, func(c string) bool { return hasClass(n, c) })
}

func hasAttr(n *html.Node, key string) bool {
	return slices.ContainsFunc(n.Attr, func(a html.Attribute) bool { return a.Key == key })
}

// walk marks the texts under n in document order; scope is where the first
// occurrence of a word is counted, skip that n is in an element whose words
// are not marked, though they still begin and end sentences.
func (mk *marking) walk(n, scope *html.Node, skip, verse bool) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		switch c.Type {
		case html.TextNode:
			if skip || !verse {
				// a link, a footnote's mark, what is no verse: their words
				// end a sentence as well as any
				if c.Parent == nil || c.Parent.DataAtom != atom.Sup {
					mk.read(c.Data)
				}
			} else {
				mk.text(c, scope)
			}
		case html.ElementNode:
			if markBlocks[c.DataAtom] {
				mk.start = true
			}
			inner := scope
			if hasClass(c, "section-body") {
				inner = c
			}
			// a link of digits, a verse's number, says nothing of sentences
			if c.DataAtom == atom.A && strings.TrimFunc(textOf(c), markerRune) == "" {
				break
			}
			mk.walk(c, inner, skip || skipped(c), verse || verseText(c))
			if markBlocks[c.DataAtom] {
				mk.start = true
			}
		}
		c = next
	}
}

// verseID is the id wol gives a verse's text, a part of it each: v43-3-16-1.
var verseID = regexp.MustCompile(`^v\d+-\d+-\d+-\d+$`)

// verseText reports whether n holds the bible's text: a verse of the bible
// page (div.item-text), a verse of a passage a reference shows (its id), or
// another translation of a verse (a section of an edition, which has none).
func verseText(n *html.Node) bool {
	return hasClass(n, "item-text") || verseID.MatchString(attr(n, "id")) ||
		(n.DataAtom == atom.Details && strings.HasPrefix(attr(n, "data-ref"), "edition:"))
}

// markerRune is what a verse's number or a reference's mark is written with.
func markerRune(r rune) bool {
	return unicode.IsDigit(r) || unicode.IsSpace(r) || r == '+' || r == '*'
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(n)
	return b.String()
}

// read follows the sentences through text: what it ends with, quotes and
// brackets aside, says whether the next word begins one.
func (mk *marking) read(text string) {
	for _, r := range text {
		mk.see(r)
	}
}

func (mk *marking) see(r rune) {
	switch {
	case unicode.IsSpace(r) || strings.ContainsRune("\"'„“”‚‘’«»‹›()[]¡¿", r):
		// neither begins nor ends a sentence
	case strings.ContainsRune(".!?:…", r):
		mk.start = true
	default:
		mk.start = false
	}
}

// text marks the words of one text node.
func (mk *marking) text(node *html.Node, scope *html.Node) {
	text := node.Data
	idx := markWord.FindAllStringIndex(text, -1)
	if len(idx) == 0 {
		mk.read(text)
		return
	}
	toks := make([]token, len(idx))
	at := 0
	starts := make([]bool, len(idx))
	for i, ix := range idx {
		mk.read(text[at:ix[0]])
		starts[i] = mk.start
		toks[i] = token{word: text[ix[0]:ix[1]], from: ix[0], to: ix[1],
			afterSpace: i > 0 && strings.TrimSpace(text[idx[i-1][1]:ix[0]]) == "" && ix[0] > idx[i-1][1]}
		mk.read(toks[i].word)
		at = ix[1]
	}
	mk.read(text[at:])

	done := mk.seen[scope]
	if done == nil {
		done = map[*markEntry]bool{}
		mk.seen[scope] = done
	}
	var own []int
	type hit struct {
		from, to int
		e        *markEntry
		docs     []GlossaryTerm
	}
	var hits []hit
	for i := 0; i < len(toks); i++ {
		e, n, inflected := mk.m.match(toks, i)
		if e == nil || done[e] {
			continue
		}
		if starts[i] && mk.m.cased(e) && (inflected || mk.lower[strings.ToLower(e.words[0])]) {
			// at the start of a sentence, a capital is no evidence
			continue
		}
		if own == nil {
			own = append(ownDocs(node), mk.own...)
		}
		docs := slices.DeleteFunc(slices.Clone(e.docs), func(d GlossaryTerm) bool { return slices.Contains(own, d.DocID) })
		if len(docs) == 0 {
			continue
		}
		hits = append(hits, hit{toks[i].from, toks[i+n-1].to, e, docs})
		done[e] = true
		i += n - 1
	}
	if len(hits) == 0 {
		return
	}
	parent := node.Parent
	at = 0
	for _, h := range hits {
		if h.from > at {
			parent.InsertBefore(&html.Node{Type: html.TextNode, Data: text[at:h.from]}, node)
		}
		parent.InsertBefore(mk.anchor(h.docs, text[h.from:h.to]), node)
		at = h.to
	}
	if at < len(text) {
		parent.InsertBefore(&html.Node{Type: html.TextNode, Data: text[at:]}, node)
	}
	parent.RemoveChild(node)
}

// ownDocs are the documents a text is part of, as the elements around it
// name them.
func ownDocs(n *html.Node) []int {
	ids := []int{}
	for p := n.Parent; p != nil; p = p.Parent {
		for c := range strings.FieldsSeq(attr(p, "class")) {
			if id, ok := strings.CutPrefix(c, "docId-"); ok {
				if v, err := strconv.Atoi(id); err == nil {
					ids = append(ids, v)
				}
			}
		}
		if v, err := strconv.Atoi(attr(p, "data-doc")); err == nil {
			ids = append(ids, v)
		}
	}
	return ids
}

// markAlt is another glossary's entry for a marked word, as its link offers
// it: the document, and the glossary — with the entry's title when the
// glossary has several for the word.
type markAlt struct {
	DocID int    `json:"d"`
	Name  string `json:"b"`
}

// anchor is the link a marked word becomes: to the entry of the first
// glossary explaining it, naming that glossary (data-gloss-book), the others
// (data-gloss-alt), and all of them on hover.
func (mk *marking) anchor(docs []GlossaryTerm, text string) *html.Node {
	m := mk.m
	book := func(d GlossaryTerm) string {
		if d.Book >= 0 && d.Book < len(m.books) {
			return m.books[d.Book].Title
		}
		return ""
	}
	var titles []string
	for _, d := range docs {
		if t := book(d); t != "" && !slices.Contains(titles, t) {
			titles = append(titles, t)
		}
	}
	a := &html.Node{Type: html.ElementNode, Data: "a", DataAtom: atom.A, Attr: []html.Attribute{
		{Key: "class", Val: "gloss"},
		{Key: "href", Val: m.docURL + strconv.Itoa(docs[0].DocID)},
		{Key: "data-gloss-book", Val: book(docs[0])},
		{Key: "title", Val: strings.Join(titles, " · ")},
	}}
	if len(docs) > 1 {
		var alts []markAlt
		for _, d := range docs[1:] {
			name := book(d)
			several := 0
			for _, o := range docs {
				if o.Book == d.Book {
					several++
				}
			}
			if several > 1 || name == "" {
				name = strings.TrimPrefix(name+" – "+cmp.Or(d.Title, d.Term), " – ")
			}
			alts = append(alts, markAlt{DocID: d.DocID, Name: name})
		}
		if b, err := json.Marshal(alts); err == nil {
			a.Attr = append(a.Attr, html.Attribute{Key: "data-gloss-alt", Val: string(b)})
		}
	}
	a.AppendChild(&html.Node{Type: html.TextNode, Data: text})
	return a
}

// cacheKey is where the words seen in small letters are kept.
func (st *markerState) cacheKey() string { return "glosslower1-" + st.locale }
