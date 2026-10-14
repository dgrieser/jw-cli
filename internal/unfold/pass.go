package unfold

import (
	"context"
	"sync"
)

// pass is the work of one level of an expansion: what it spent, and how far it
// got, for Progress. A level can take more than one go at its nodes — the
// citations first, then the passages a citation turned out to name besides its
// first — and the count of what is done runs on across them.
//
// The nodes of a go are worked on Options.Workers at a time: every citation is
// a round trip to wol of a second or more, and an index lists hundreds. Each
// node is worked on by one worker alone, and everything that weighs the nodes
// against each other — what is a repeat of what, which verse is shown first —
// is done once they are all back, in the order they were given, so the outcome
// is the same however the answers arrive.
type pass struct {
	s     *Session
	level int
	// quiet keeps the pass from Progress: work the caller does not report as
	// a level
	quiet bool
	mu    sync.Mutex
	done  int
	total int
}

func (s *Session) pass(level int) *pass { return &pass{s: s, level: level} }

// refsByPath is what the verses of a level turned out to cite beside their
// text — their research-guide passages — by the path of each verse, as the
// workers of a pass find it.
type refsByPath struct {
	mu   sync.Mutex
	refs map[string][]Ref
}

func (r *refsByPath) set(path string, refs []Ref) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.refs == nil {
		r.refs = map[string][]Ref{}
	}
	r.refs[path] = refs
}

func (r *refsByPath) get(path string) []Ref {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.refs[path]
}

// each does work on every node, which returns the requests it spent, and
// reports every node done to Progress, in order of completion with a count
// that only goes up. Once ctx is done no further node is started: it returns
// what was spent, how many nodes were left, and ctx's error.
func (p *pass) each(ctx context.Context, nodes []*Node, work func(*Node) int) (spent, left int, err error) {
	p.total += len(nodes)
	finish := func(requests int) {
		p.mu.Lock()
		defer p.mu.Unlock()
		spent += requests
		p.done++
		if !p.quiet && p.s.o.Progress != nil {
			p.s.o.Progress(p.level, p.done, p.total)
		}
	}
	workers := min(max(p.s.o.Workers, 1), len(nodes))
	if workers <= 1 {
		for i, n := range nodes {
			if err := ctx.Err(); err != nil {
				return spent, len(nodes) - i, err
			}
			finish(work(n))
		}
		return spent, 0, nil
	}
	var wg sync.WaitGroup
	next := make(chan *Node)
	for range workers {
		wg.Go(func() {
			for n := range next {
				finish(work(n))
			}
		})
	}
	started := 0
hand:
	for _, n := range nodes {
		if ctx.Err() != nil {
			break
		}
		select {
		case next <- n:
			started++
		case <-ctx.Done():
			break hand
		}
	}
	close(next)
	wg.Wait()
	if started < len(nodes) {
		return spent, len(nodes) - started, ctx.Err()
	}
	return spent, 0, nil
}
