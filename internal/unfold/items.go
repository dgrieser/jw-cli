package unfold

import (
	"strconv"
	"strings"

	"github.com/dgrieser/jw-cli/internal/model"
)

// wol answers a citation with as many passages as it names: a marginal
// reference ("+") pointing at seven verses comes back as seven verses, a
// citation of a publication spanning two places as two passages. Each of them
// is a reference in its own right — a verse with its own study material, shown
// once like any other — so each becomes a node of its own, in the order wol
// lists them, right where the citation stands.

// itemMark joins the path of a citation and the number of one of its
// passages. It is a fragment, which a request never carries: the path of an
// item still asks for the citation it belongs to.
const itemMark = "#item-"

// ItemPath is the path of passage item (from one) of the citation at path.
// The first passage keeps the citation's own path.
func ItemPath(path string, item int) string {
	if item <= 1 {
		return path
	}
	return path + itemMark + strconv.Itoa(item)
}

// SplitItemPath is the citation an item's path belongs to, and which of its
// passages the path names: the path itself and the first for any other path.
func SplitItemPath(path string) (string, int) {
	i := strings.LastIndex(path, itemMark)
	if i < 0 {
		return path, 1
	}
	n, err := strconv.Atoi(path[i+len(itemMark):])
	if err != nil || n < 1 {
		return path, 1
	}
	return path[:i], n
}

// CitationPath is the citation an item's path belongs to, and any other path
// itself: what a caller matches a node against the citations it handed in.
func CitationPath(path string) string {
	base, _ := SplitItemPath(path)
	return base
}

// resolved fills in what a citation turned out to be: its first passage, and
// the others for splitItems to make nodes of.
func (n *Node) resolved(tip model.Tooltip) {
	n.Title, n.HTML, n.URL, n.Publication, n.more = tip.Title, tip.ContentHTML, tip.URL, tip.PublicationTitle, tip.More
}

// setCited records who quotes the verse a node resolved to.
func (n *Node) setCited(c Cited) {
	n.Cited, n.CitedTotal, n.CitedRef, n.CitedLazy, n.CitedErr = c.Results, c.Total, c.Ref, c.Lazy, c.Err
}

// splitItems makes a node of every passage after the first of a citation
// answered with several, right after the first, and returns the nodes it made
// as they now sit in the lists. keep, when given, says which are worth a node:
// a passage left out is not shown and costs nothing more.
func (s *Session) splitItems(lists []*[]Node, keep func(*Node) bool) []*Node {
	made := map[string]bool{}
	for _, list := range lists {
		split := false
		for _, n := range *list {
			split = split || len(n.more) > 0
		}
		if !split {
			continue
		}
		out := make([]Node, 0, len(*list))
		for _, n := range *list {
			more := n.more
			n.more = nil
			if len(more) > 0 {
				n.Item = 1
			}
			out = append(out, n)
			for i, tip := range more {
				ref := n.Ref
				ref.Path = ItemPath(n.Ref.Path, i+2)
				if s.seen[ref.Path] {
					continue
				}
				s.seen[ref.Path] = true
				item := Node{Ref: ref, Item: i + 2, Hops: n.Hops, ready: true}
				item.resolved(tip)
				if keep != nil && !keep(&item) {
					continue
				}
				out = append(out, item)
				made[ref.Path] = true
			}
		}
		*list = out
	}
	if len(made) == 0 {
		return nil
	}
	var out []*Node
	for _, n := range nodesIn(lists) {
		if made[n.Ref.Path] {
			out = append(out, n)
		}
	}
	return out
}
