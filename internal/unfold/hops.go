package unfold

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
)

// An expansion counted in bible references (Options.Hops) reads a verse the way
// the study pane does: with references left, a verse brings its study notes,
// its footnotes, the passages its indexes point at and its marginal references,
// and every verse any of them cites is one reference further down; with none
// left, it brings its text, and its sections are left to be opened. A passage of
// another publication is part of the verse or the document that cites it rather
// than a step of its own: it brings the verses it cites, at the step it sits on.
//
// The steps are expanded breadth first, as levels are, so the request count of
// the next is known before it is spent, and every verse is shown at most once:
// where it is first reached, which is where it unfolds furthest.

// footnotePath is the endpoint wol answers a footnote marker ("*") through.
const footnotePath = "/fn/"

// footnotePaths are the footnote markers of a fragment, in document order and
// without repeats.
func footnotePaths(fragment string) []string {
	if !strings.Contains(fragment, footnotePath) {
		return nil
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href := a.AttrOr("href", "")
		if strings.Contains(href, footnotePath) && !seen[href] {
			seen[href] = true
			out = append(out, href)
		}
	})
	return out
}

// verseSpanID matches the id wol wraps a verse of bible text in,
// "v43-3-16-1": book, chapter, verse, segment.
var verseSpanID = regexp.MustCompile(`\bid="v(\d+)-(\d+)-(\d+)-\d+"`)

// VerseIDs are the verses a passage of bible text holds, by wol verse id
// (book*1e6 + chapter*1e3 + verse), in the order it reads and without repeats;
// none for anything that is not bible text.
func VerseIDs(fragment string) []int {
	var out []int
	seen := map[int]bool{}
	for _, m := range verseSpanID.FindAllStringSubmatch(fragment, -1) {
		book, _ := strconv.Atoi(m[1])
		chapter, _ := strconv.Atoi(m[2])
		verse, _ := strconv.Atoi(m[3])
		id := book*1_000_000 + chapter*1_000 + verse
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// Verses is a set of verses shown, by wol verse id (book*1e6 + chapter*1e3 +
// verse). The pieces of one page may unfold at once and share one, so it is
// safe for concurrent use.
type Verses struct {
	mu  sync.Mutex
	ids map[int]bool
	// parent is the set a tracked view (Track) reads and writes, and added
	// what the view put into it.
	parent *Verses
	added  []int
}

// maxVerses bounds a set: far more than any page shows, and what keeps one a
// page keeps adding to from growing without end.
const maxVerses = 100_000

// NewVerses is an empty set.
func NewVerses() *Verses { return &Verses{ids: map[int]bool{}} }

// Track is a view of v for one piece of a page: it reads and adds to v as v
// itself does, so pieces unfolding at once see each other's verses, and keeps
// what it added, for Release.
func (v *Verses) Track() *Verses { return &Verses{parent: v} }

// Release takes back what a tracked view added: the piece failed, and what it
// showed is not on the page.
func (v *Verses) Release() {
	if v.parent == nil {
		return
	}
	v.mu.Lock()
	added := v.added
	v.added = nil
	v.mu.Unlock()
	p := v.parent
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, id := range added {
		delete(p.ids, id)
	}
}

// Add counts ids as shown.
func (v *Verses) Add(ids ...int) { v.claim(ids, false) }

// Shown reports whether every one of ids is shown already. None is never
// shown: a reference wol answers without verse ids is no repeat of anything.
func (v *Verses) Shown(ids []int) bool {
	if v.parent != nil {
		return v.parent.Shown(ids)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.shown(ids)
}

func (v *Verses) shown(ids []int) bool {
	if len(ids) == 0 {
		return false
	}
	for _, id := range ids {
		if !v.ids[id] {
			return false
		}
	}
	return true
}

// Claim reports whether every one of ids is shown already, and counts them as
// shown when they are not: the check and the claim are one step, so of two
// pieces reaching a verse at once only one shows it.
func (v *Verses) Claim(ids []int) bool { return v.claim(ids, true) }

func (v *Verses) claim(ids []int, check bool) bool {
	if v.parent != nil {
		shown, added := v.parent.put(ids, check)
		v.mu.Lock()
		v.added = append(v.added, added...)
		v.mu.Unlock()
		return shown
	}
	shown, _ := v.put(ids, check)
	return shown
}

// put adds ids to a set of its own, unless check finds them all shown, and
// returns what was not there before.
func (v *Verses) put(ids []int, check bool) (bool, []int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if check && v.shown(ids) {
		return true, nil
	}
	var added []int
	for _, id := range ids {
		if !v.ids[id] && len(v.ids) < maxVerses {
			v.ids[id] = true
			added = append(added, id)
		}
	}
	return false, added
}

// Len is how many verses are shown.
func (v *Verses) Len() int {
	if v.parent != nil {
		return v.parent.Len()
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.ids)
}

// ShowVerses counts verses as shown already (Options.Verses): the verse an
// expansion is of, or what an earlier piece of it showed.
func (s *Session) ShowVerses(ids ...int) {
	if s.o.Verses == nil {
		s.o.Verses = NewVerses()
	}
	s.o.Verses.Add(ids...)
}

// shownAlready reports whether every verse of a resolved verse node is shown
// already, and counts them as shown otherwise.
func (s *Session) shownAlready(n *Node) bool {
	return s.o.Verses.Claim(VerseIDs(n.HTML))
}

// Show resolves refs and counts what they say as shown, without showing them:
// a passage saying the same is then left out of what the session expands. It is
// how one index of a verse is loaded on its own without repeating what the
// other one lists. What it cost is spent like everything else.
func (s *Session) Show(ctx context.Context, refs []Ref) error {
	s.run++
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.seen[ref.Path] {
			continue
		}
		s.seen[ref.Path] = true
		tip, err := s.r.Resolve(ctx, ref.Path)
		s.Spend(1)
		if err != nil {
			continue
		}
		n := Node{Ref: ref, Title: tip.Title, HTML: tip.ContentHTML, URL: tip.URL}
		if text := plainText(&n); text != "" {
			doc := document(&n)
			s.shown[doc] = append(s.shown[doc], passage{path: ref.Path, text: text, rank: ref.Rank, run: s.run})
		}
	}
	return nil
}

// planHops turns refs into unresolved nodes with hops left, as plan does: a
// verse has hops left, a passage cites its verses with one fewer.
func (s *Session) planHops(refs []Ref, hops int) []Node {
	var out []Node
	for _, ref := range refs {
		if s.seen[ref.Path] {
			continue
		}
		s.seen[ref.Path] = true
		n := Node{Ref: ref, Hops: hops}
		if !ref.IsVerse() {
			n.Hops = hops - 1
		}
		out = append(out, n)
	}
	return out
}

// CostHops is what the first step of expanding refs with hops left would cost,
// as Cost is for an expansion counted in levels.
func (s *Session) CostHops(refs []Ref, hops int) int {
	var frontier []*Node
	planned := map[string]bool{}
	for _, ref := range refs {
		if s.seen[ref.Path] || planned[ref.Path] {
			continue
		}
		planned[ref.Path] = true
		n := &Node{Ref: ref, Hops: hops}
		if !ref.IsVerse() {
			n.Hops = hops - 1
		}
		frontier = append(frontier, n)
	}
	return s.hopCost(frontier)
}

// hopCost is how many requests resolving a step needs: one per reference, and
// the chapter page of every verse that brings its study material.
func (s *Session) hopCost(frontier []*Node) int {
	_, withStudy := s.r.(StudyResolver)
	cost := len(frontier)
	if withStudy {
		for _, n := range frontier {
			if n.Ref.IsVerse() && n.Hops > 0 {
				cost++
			}
		}
	}
	return cost
}

// notesHTML is what the study notes of a verse say, for the citations in them.
func notesHTML(n *Node) string {
	var b strings.Builder
	for _, note := range n.Notes {
		b.WriteString(note.HTML)
	}
	return b.String()
}

// runHops is Run for an expansion counted in bible references.
func (s *Session) runHops(ctx context.Context, groups []Group) (res Grouped, err error) {
	r, o := s.r, s.o
	res = Grouped{Nodes: make([][]Node, len(groups))}
	s.run++
	defer func() {
		s.carried += res.Requests
		s.total += res.Requests
	}()
	study, _ := r.(StudyResolver)
	// who quotes a verse is only ever a heading here: a resolver answering it
	// is asked for where the page loads it from
	cited, _ := r.(CitedResolver)
	known, _ := r.(KnownResolver)
	var tiers []*[]Node
	for i, g := range groups {
		res.Nodes[i] = s.planHops(append(Refs(g.Fragment), g.RootRefs...), g.Hops)
		tiers = append(tiers, &res.Nodes[i])
	}
	// what a step costs is asked about before it is spent: what is pending
	// when the answer is no is everything of the step
	ask := func(level, cost, pending int) (bool, error) {
		ok, err := s.Check(level, cost)
		if err == nil && !ok {
			res.Pending += pending
			res.Stopped = true
		}
		return ok, err
	}
	// recall fills in what is at hand already, and counts what is not: what
	// reading the nodes will cost
	recall := func(nodes []*Node) int {
		unknown := 0
		for _, n := range nodes {
			if known != nil {
				if tip, ok := known.Known(ctx, n.Ref); ok {
					n.Title, n.HTML, n.URL, n.ready = tip.Title, tip.ContentHTML, tip.URL, true
					continue
				}
			}
			unknown++
		}
		return unknown
	}
	resolve := func(n *Node) {
		if n.ready {
			return
		}
		tip, err := r.Resolve(ctx, n.Ref.Path)
		res.Requests++
		if err != nil {
			n.Err = err
			return
		}
		n.Title, n.HTML, n.URL = tip.Title, tip.ContentHTML, tip.URL
	}
	for level := 1; ; level++ {
		frontier := nodesIn(tiers)
		if len(frontier) == 0 {
			return res, nil
		}
		// a verse the citation names by its text, shown already, is left out
		// before it is read
		if known != nil {
			early := map[string]bool{}
			for _, n := range frontier {
				if n.Ref.IsVerse() && s.o.Verses.Shown(known.VersesOf(ctx, n.Ref)) {
					early[n.Ref.Path] = true
				}
			}
			if len(early) > 0 {
				for _, tier := range tiers {
					*tier = without(*tier, early)
				}
				if frontier = nodesIn(tiers); len(frontier) == 0 {
					return res, nil
				}
			}
		}
		unknown := recall(frontier)
		if ok, err := ask(level, s.hopCost(frontier)-(len(frontier)-unknown), len(frontier)); err != nil || !ok {
			return res, err
		}
		research := map[string][]Ref{}
		for i, n := range frontier {
			if err := ctx.Err(); err != nil {
				res.Pending += len(frontier) - i
				return res, err
			}
			resolve(n)
			if n.Err == nil && n.Ref.IsVerse() {
				n.Parts = &Parts{Lazy: n.Hops <= 0}
				if cited != nil {
					c := cited.Cited(ctx, n.Title)
					res.Requests += c.Requests
					n.Cited, n.CitedTotal, n.CitedRef, n.CitedLazy = c.Results, c.Total, c.Ref, c.Lazy
				}
				if n.Hops > 0 && study != nil {
					st, err := study.Study(ctx, n.Title)
					res.Requests += st.Requests
					if err != nil {
						n.StudyErr = err
					} else {
						n.Notes, n.Links = st.Notes, st.Links
						research[n.Ref.Path] = st.Research
					}
				}
			}
			if o.Progress != nil {
				o.Progress(level, i+1, len(frontier))
			}
		}
		// a verse shown already is not shown again; a passage saying what
		// another one says, as at any level
		dropped := map[string]bool{}
		var passages []*Node
		for _, n := range frontier {
			switch {
			case n.Err != nil:
			case n.Ref.IsVerse():
				if s.shownAlready(n) {
					dropped[n.Ref.Path] = true
				}
			default:
				passages = append(passages, n)
			}
		}
		for path := range duplicates(passages, s.shown, s.run, level) {
			dropped[path] = true
		}
		if len(dropped) > 0 {
			for _, tier := range tiers {
				*tier = without(*tier, dropped)
			}
		}
		survivors := nodesIn(tiers)

		// what the verses of the step bring that has to be read before what
		// it cites is known: the passages of their indexes, their footnotes
		var footnotes, indexes []*[]Node
		for _, n := range survivors {
			if n.Parts == nil || n.Parts.Lazy {
				continue
			}
			n.Parts.Research = s.planHops(research[n.Ref.Path], n.Hops)
			for _, p := range footnotePaths(n.HTML) {
				if s.seen[p] {
					continue
				}
				s.seen[p] = true
				n.Parts.Footnotes = append(n.Parts.Footnotes, Node{Ref: Ref{Text: "*", Path: p}, Hops: n.Hops - 1})
			}
			footnotes = append(footnotes, &n.Parts.Footnotes)
			indexes = append(indexes, &n.Parts.Research)
		}
		if count := recall(append(nodesIn(footnotes), nodesIn(indexes)...)); count > 0 {
			if ok, err := ask(level, count, count); err != nil || !ok {
				for _, list := range append(footnotes, indexes...) {
					*list = nil
				}
				return res, err
			}
		}
		for _, n := range append(nodesIn(footnotes), nodesIn(indexes)...) {
			if err := ctx.Err(); err != nil {
				return res, err
			}
			resolve(n)
		}
		// the two indexes of a verse regularly point at one passage
		if dropped := duplicates(nodesIn(indexes), s.shown, s.run, level); len(dropped) > 0 {
			for _, list := range indexes {
				*list = without(*list, dropped)
			}
		}

		// what the step cites, one reference further down
		var next []*[]Node
		cite := func(list *[]Node, fragment string, hops int) {
			if hops < 0 || fragment == "" {
				return
			}
			*list = s.planHops(verseRefs(Refs(fragment)), hops)
			if len(*list) > 0 {
				next = append(next, list)
			}
		}
		for _, n := range survivors {
			switch {
			case n.Err != nil:
			case !n.Ref.IsVerse():
				cite(&n.Children, n.HTML, n.Hops)
			case n.Parts != nil && !n.Parts.Lazy:
				k := n.Hops - 1
				cite(&n.Parts.NoteRefs, notesHTML(n), k)
				for i := range n.Parts.Footnotes {
					if f := &n.Parts.Footnotes[i]; f.Err == nil {
						cite(&f.Children, f.HTML, k)
					}
				}
				cite(&n.Children, n.HTML, k)
				for i := range n.Parts.Research {
					if p := &n.Parts.Research[i]; p.Err == nil {
						cite(&p.Children, p.HTML, k)
					}
				}
			}
		}
		tiers = next
	}
}
