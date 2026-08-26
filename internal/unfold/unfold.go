// Package unfold expands the citations in a document. wol writes a bible verse
// or a passage of another publication as a link to a tooltip endpoint; unfolding
// follows those links and brings the text itself into the output, optionally
// recursing into whatever the expanded text cites in turn.
package unfold

import (
	"context"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/model"
)

// Ref is one citation in a document: the text as it appears there and the wol
// path that resolves it.
type Ref struct {
	Text string
	Path string
	// Rank breaks the tie when two references turn out to resolve to the same
	// content: the lowest rank is the one kept. Zero for anything cited in a
	// document, which is a single list with nothing to prefer; the indexes of
	// the study bible list the same passage twice and rank one above the other.
	Rank int
}

// IsVerse reports whether the citation resolves to bible text rather than to a
// passage of another publication. wol tells the two apart by endpoint.
func (r Ref) IsVerse() bool { return strings.Contains(r.Path, versePath) }

// Resolver fetches the content behind a citation path. wol answers its verse
// (/bc/) and publication (/pc/) endpoints with the same shape, so one resolver
// covers both.
type Resolver interface {
	Resolve(ctx context.Context, path string) (model.Tooltip, error)
}

// Study is what the study bible attaches to a verse beyond its text.
type Study struct {
	// Notes are printed with the verse itself.
	Notes []model.StudyNote
	// Research are the research-guide passages, which resolve through the same
	// citation endpoint as anything else and are followed like the verse's own
	// citations.
	Research []Ref
	// Links are the research-guide entries pointing at a whole article rather
	// than a passage. There is no citation endpoint behind those, so they are
	// listed instead of unfolded.
	Links []model.ResearchItem
	// Requests is how many requests gathering this cost, reported even
	// alongside an error, so the budget the user confirmed stays honest.
	Requests int
}

// StudyResolver is an optional capability of a Resolver: given the title wol
// answered a verse citation with, it returns the study material of that verse.
// A Resolver without it leaves every verse at its bare text.
type StudyResolver interface {
	Study(ctx context.Context, verseTitle string) (Study, error)
}

// Node is one expanded citation together with what its own content cites.
type Node struct {
	Ref Ref
	// Title and HTML are the resolved content. Err is set instead when the
	// reference could not be resolved; the reference is still reported, so a
	// gap in the output is never silent.
	Title string
	HTML  string
	// URL is the document the passage was lifted out of, with an anchor naming
	// the extent that was taken. It is what tells two references pointing at one
	// passage apart from two references pointing at different ones.
	URL string
	Err error
	// Notes and Links are the study material of a verse; StudyErr says the
	// study pane could not be read, for the same reason Err is kept.
	Notes    []model.StudyNote
	Links    []model.ResearchItem
	StudyErr error
	Children []Node
}

// Options controls one expansion.
type Options struct {
	// Depth is how many levels of citations to follow. Zero expands nothing.
	Depth int
	// Threshold is the number of requests a level may need before Confirm is
	// consulted. Zero consults it for every level.
	Threshold int
	// Confirm is asked before a level that needs more than Threshold requests,
	// with the exact count. Returning false stops the expansion there and keeps
	// what was already gathered. Nil proceeds without asking.
	Confirm func(level, requests int) (bool, error)
	// Progress reports each completed request within a level. Nil is silent.
	Progress func(level, done, total int)
	// RootRefs are references belonging to the fragment itself rather than to a
	// citation inside it — the research-guide passages of the verses a bible
	// passage is made of. They are expanded alongside the fragment's own
	// citations, at the same level.
	RootRefs []Ref
}

// Result is an expansion and how far it got.
type Result struct {
	Nodes []Node
	// Requests is how many citations were resolved.
	Requests int
	// Pending is how many references were left unexpanded because a level was
	// declined or the depth ran out at a level that still had references.
	Pending int
	// Stopped records that Confirm declined a level, so the caller can say the
	// output is incomplete rather than let it read as the whole picture.
	Stopped bool
}

// Group is one fragment of an expansion that keeps its own expansion: how
// jw bible read asks for the references of every verse it prints separately, so
// each verse can be followed by what it cites.
type Group struct {
	Fragment string
	// RootRefs are references belonging to the fragment itself rather than to a
	// citation inside it, as Options.RootRefs is for a single fragment.
	RootRefs []Ref
}

// Grouped is the expansion of several fragments in one run: one budget, one
// confirmation and one set of already-expanded paths across all of them, with
// the nodes of each fragment kept apart.
type Grouped struct {
	// Nodes holds the roots of every group, in the order the groups were given.
	Nodes [][]Node
	// Requests, Pending and Stopped count the whole run, as in Result.
	Requests int
	Pending  int
	Stopped  bool
}

// Run expands the citations in an HTML fragment. References are followed breadth
// first, so the request count for the next level is known before it is spent —
// that is what Confirm is told. Every path is expanded at most once across the
// whole tree, which both removes repeats and keeps a citation cycle from running
// forever. When r is a StudyResolver, every verse also brings its study
// material: the notes are printed with it, the research-guide passages are
// followed like the citations inside it.
func Run(ctx context.Context, r Resolver, fragment string, o Options) (Result, error) {
	g, err := RunGroups(ctx, r, []Group{{Fragment: fragment, RootRefs: o.RootRefs}}, o)
	res := Result{Requests: g.Requests, Pending: g.Pending, Stopped: g.Stopped}
	if len(g.Nodes) > 0 {
		res.Nodes = g.Nodes[0]
	}
	return res, err
}

// RunGroups expands several fragments as one run, as Run does for one. The
// groups share the budget Confirm is asked about and the paths already expanded,
// so a passage cited twice is still expanded once — the fragments are one
// document as far as the expansion is concerned, only their output is kept
// apart. Options.RootRefs is ignored; every group carries its own.
func RunGroups(ctx context.Context, r Resolver, groups []Group, o Options) (Grouped, error) {
	res := Grouped{Nodes: make([][]Node, len(groups))}
	if o.Depth <= 0 {
		return res, nil
	}
	study, _ := r.(StudyResolver)
	seen := map[string]bool{}
	shown := map[string][]passage{}
	// the node lists of the level being expanded: the roots of every group to
	// start with, the children of what those resolved to after that. Nodes are
	// reached through their list rather than kept as pointers, because dropping
	// a duplicate rewrites the list it sat in.
	var tiers []*[]Node
	for i, g := range groups {
		res.Nodes[i] = plan(append(Refs(g.Fragment), g.RootRefs...), seen)
		tiers = append(tiers, &res.Nodes[i])
	}

	for level := 1; ; level++ {
		frontier := nodesIn(tiers)
		if len(frontier) == 0 {
			break
		}
		if o.Confirm != nil {
			if cost := requestCost(frontier, study != nil); cost > o.Threshold {
				ok, err := o.Confirm(level, cost)
				if err != nil {
					return res, err
				}
				if !ok {
					res.Pending += len(frontier)
					res.Stopped = true
					return res, nil
				}
			}
		}
		// the research-guide passages each verse turned out to have, kept aside
		// so they join the citations found in its text. Keyed by path rather
		// than by node: dropping a duplicate moves the nodes of a tier around.
		research := map[string][]Ref{}
		for i, n := range frontier {
			if err := ctx.Err(); err != nil {
				res.Pending += len(frontier) - i
				return res, err
			}
			tip, err := r.Resolve(ctx, n.Ref.Path)
			res.Requests++
			if err != nil {
				n.Err = err
			} else {
				n.Title, n.HTML, n.URL = tip.Title, tip.ContentHTML, tip.URL
			}
			if study != nil && n.Err == nil && n.Ref.IsVerse() {
				s, err := study.Study(ctx, n.Title)
				res.Requests += s.Requests
				if err != nil {
					n.StudyErr = err
				} else {
					n.Notes, n.Links = s.Notes, s.Links
					research[n.Ref.Path] = s.Research
				}
			}
			if o.Progress != nil {
				o.Progress(level, i+1, len(frontier))
			}
		}
		if dropped := duplicates(frontier, shown, level); len(dropped) > 0 {
			for _, tier := range tiers {
				*tier = without(*tier, dropped)
			}
		}
		// the pointers in frontier no longer stand for what they did before the
		// duplicates came out of the tiers, so the survivors are read back from
		// the tiers themselves
		var next []*[]Node
		for _, n := range nodesIn(tiers) {
			refs := plan(append(Refs(n.HTML), research[n.Ref.Path]...), seen)
			if level == o.Depth {
				// the depth is used up: report what was left rather than
				// letting the output look complete
				res.Pending += len(refs)
				continue
			}
			n.Children = refs
			next = append(next, &n.Children)
		}
		if level == o.Depth {
			break
		}
		tiers = next
	}
	return res, nil
}

// passage is a piece of content already in the output: the reference it is shown
// under, the text it brought, and the level it was expanded at.
type passage struct {
	path  string
	text  string
	rank  int
	level int
}

// duplicates picks out the references of a level that say what another one
// already says, and returns the paths to drop. Two entries of the study bible's
// indexes regularly point at the same passage — the research guide spelling the
// publication out, the publications index citing it by symbol — through paths of
// their own, and each cuts the passage where it likes, so the duplication is
// only visible once both are resolved and the same article comes back twice. The
// lower rank wins, and equal ranks leave the reference that came first standing;
// a passage already shown at an earlier level keeps its place, since the output
// above it is written by then.
func duplicates(frontier []*Node, shown map[string][]passage, level int) map[string]bool {
	dropped := map[string]bool{}
	for _, n := range frontier {
		text := plainText(n)
		if text == "" {
			// nothing came back: an unresolved reference is reported on its own
			continue
		}
		doc := document(n)
		i := saidAlready(shown[doc], text, n.Ref.Rank)
		here := passage{path: n.Ref.Path, text: text, rank: n.Ref.Rank, level: level}
		if i < 0 {
			shown[doc] = append(shown[doc], here)
			continue
		}
		if prev := shown[doc][i]; prev.level == level && n.Ref.Rank < prev.rank {
			dropped[prev.path] = true
			shown[doc][i] = here
			continue
		}
		dropped[n.Ref.Path] = true
	}
	return dropped
}

// saidAlready finds the passage of the same document that says what text says,
// and returns -1 when none does. Word for word is the same passage however it
// was cited. One text containing the other is too — but only across indexes,
// which cite one passage at lengths of their own: within a single list two
// citations of overlapping length are two citations, a verse and the passage it
// opens, and both belong in the output.
func saidAlready(list []passage, text string, rank int) int {
	for i, p := range list {
		switch {
		case p.text == text:
			return i
		case p.rank != rank && (strings.Contains(p.text, text) || strings.Contains(text, p.text)):
			return i
		}
	}
	return -1
}

// document identifies the article a passage was lifted out of: the resolved URL
// without the anchor naming the extent, which is exactly what two index entries
// for the same passage differ in. A citation that came back without a URL falls
// back to its title, so passages of one article are still only weighed against
// each other.
func document(n *Node) string {
	if n.URL == "" {
		return n.Title
	}
	if i := strings.IndexByte(n.URL, '#'); i >= 0 {
		return n.URL[:i]
	}
	return n.URL
}

// plainText is the words a resolved reference brought, without the markup they
// came wrapped in: the same passage cited at two lengths is nested markup in one
// and top-level paragraphs in the other, and only the text of it is comparable.
func plainText(n *Node) string {
	if n.Err != nil {
		return ""
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(n.HTML))
	if err != nil {
		return collapseSpace(n.HTML)
	}
	return collapseSpace(doc.Find("body").Text())
}

// without drops the nodes whose reference path is in paths, keeping the order of
// the rest.
func without(nodes []Node, paths map[string]bool) []Node {
	out := nodes[:0]
	for _, n := range nodes {
		if paths[n.Ref.Path] {
			continue
		}
		out = append(out, n)
	}
	return out
}

// nodesIn is every node of a level, in the order of the lists holding them.
func nodesIn(tiers []*[]Node) []*Node {
	var out []*Node
	for _, tier := range tiers {
		for i := range *tier {
			out = append(out, &(*tier)[i])
		}
	}
	return out
}

// requestCost is how many requests a level needs. With study material a verse
// costs one more than its own text: the chapter page the study pane lives on.
// Chapter pages are shared by every verse in the chapter and fetched once, so
// this is an upper bound — the right side to err on for a question that is
// answered before the traffic is spent.
func requestCost(frontier []*Node, withStudy bool) int {
	cost := len(frontier)
	if !withStudy {
		return cost
	}
	for _, n := range frontier {
		if n.Ref.IsVerse() {
			cost++
		}
	}
	return cost
}

// plan turns refs into unresolved nodes, skipping paths already accounted for
// anywhere in the tree.
func plan(refs []Ref, seen map[string]bool) []Node {
	var out []Node
	for _, ref := range refs {
		if seen[ref.Path] {
			continue
		}
		seen[ref.Path] = true
		out = append(out, Node{Ref: ref})
	}
	return out
}

// The two endpoints wol resolves a citation through.
const (
	versePath       = "/bc/" // bible text
	publicationPath = "/pc/" // a passage of another publication
)

// citationLinks matches a citation of either kind.
var citationLinks = fmt.Sprintf("a[href*=%q], a[href*=%q]", versePath, publicationPath)

// Refs finds the citations in an HTML fragment, in document order and without
// repeats within the fragment.
func Refs(fragment string) []Ref {
	if fragment == "" {
		return nil
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return nil
	}
	var out []Ref
	seen := map[string]bool{}
	doc.Find(citationLinks).Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		if href == "" || seen[href] {
			return
		}
		seen[href] = true
		out = append(out, Ref{Text: collapseSpace(s.Text()), Path: href})
	})
	return out
}

// Count is how many citations a fragment holds, for sizing an expansion before
// starting one.
func Count(fragment string) int { return len(Refs(fragment)) }

func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }
