package unfold

import "context"

// pass is the work of one level of an expansion: what it spent, and how far it
// got, for Progress. A level can take more than one go at its nodes — the
// citations first, then the passages a citation turned out to name besides its
// first — and the count of what is done runs on across them.
type pass struct {
	s     *Session
	level int
	done  int
	total int
}

func (s *Session) pass(level int) *pass { return &pass{s: s, level: level} }

// each does work on every node, which returns the requests it spent, and
// reports every node done to Progress. Once ctx is done no further node is
// started: it returns what was spent, how many nodes were left, and ctx's
// error.
func (p *pass) each(ctx context.Context, nodes []*Node, work func(*Node) int) (spent, left int, err error) {
	p.total += len(nodes)
	for i, n := range nodes {
		if err := ctx.Err(); err != nil {
			return spent, len(nodes) - i, err
		}
		spent += work(n)
		p.done++
		if p.s.o.Progress != nil {
			p.s.o.Progress(p.level, p.done, p.total)
		}
	}
	return spent, 0, nil
}
