package unfold

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/model"
)

// slowResolver answers from tables after a moment, safe for concurrent use,
// and counts how many requests it answers at once.
type slowResolver struct {
	content map[string]model.Tooltip
	study   map[string]Study
	delay   time.Duration
	mu      sync.Mutex
	now     int
	peak    int
}

func (r *slowResolver) wait() func() {
	r.mu.Lock()
	r.now++
	r.peak = max(r.peak, r.now)
	r.mu.Unlock()
	time.Sleep(r.delay)
	return func() {
		r.mu.Lock()
		r.now--
		r.mu.Unlock()
	}
}

func (r *slowResolver) Resolve(_ context.Context, path string) (model.Tooltip, error) {
	defer r.wait()()
	tip, ok := r.content[path]
	if !ok {
		return model.Tooltip{}, fmt.Errorf("no content for %s", path)
	}
	return tip, nil
}

func (r *slowResolver) Study(_ context.Context, title string) (Study, error) {
	defer r.wait()()
	return r.study[title], nil
}

// library is a dozen verses citing each other — so the same verse is reached
// through several of them — each with a research passage citing a verse in
// turn, some of the passages alike, and a marginal reference naming two verses.
func library(delay time.Duration) *slowResolver {
	r := &slowResolver{content: map[string]model.Tooltip{}, study: map[string]Study{}, delay: delay}
	for i := 1; i <= 12; i++ {
		title := fmt.Sprintf("Psalm %d:1", i)
		r.content[fmt.Sprintf("/wol/bc/%d", i)] = model.Tooltip{Title: title, ContentHTML: verseHTML(19, i, 1,
			fmt.Sprintf("verse %d", i)+link(fmt.Sprintf("/wol/bc/%d", i%5+1), "+")+link("/wol/bc/multi", "+"))}
		r.content[fmt.Sprintf("/wol/pc/%d", i)] = model.Tooltip{Title: "Passage",
			ContentHTML: fmt.Sprintf("<p>passage %d (%s)</p>", i%4, link(fmt.Sprintf("/wol/bc/%d", (i+3)%12+1), "Ps 1")),
			URL:         fmt.Sprintf("/d/%d#h=%d", i%3, i)}
		r.study[title] = Study{Research: []Ref{{Text: "w", Path: fmt.Sprintf("/wol/pc/%d", i)}}, Requests: 1}
	}
	r.content["/wol/bc/multi"] = model.Tooltip{Title: "John 1:1", ContentHTML: verseHTML(43, 1, 1, "the Word"),
		More: []model.Tooltip{{Title: "John 1:2", ContentHTML: verseHTML(43, 1, 2, "in the beginning")}}}
	return r
}

// outline writes a tree out, everything the output is made of in order.
func outline(nodes []Node, depth int, b *strings.Builder) {
	for _, n := range nodes {
		fmt.Fprintf(b, "%s%s %s %q err=%v\n", strings.Repeat("  ", depth), n.Ref.Path, n.Title, n.HTML, n.Err)
		outline(n.Children, depth+1, b)
		if n.Parts != nil {
			outline(n.Parts.Research, depth+1, b)
			outline(n.Parts.NoteRefs, depth+1, b)
		}
	}
}

// Workers change how long an expansion takes, nothing else: the same nodes in
// the same order, the same requests and the same progress.
func TestWorkersChangeNothingButTime(t *testing.T) {
	var fragment strings.Builder
	for i := 1; i <= 12; i++ {
		fragment.WriteString(link(fmt.Sprintf("/wol/bc/%d", i), fmt.Sprintf("Ps %d:1", i)))
	}
	for _, hops := range []bool{false, true} {
		run := func(workers int) (string, int, []string, int) {
			r := library(2 * time.Millisecond)
			var progress []string
			var mu sync.Mutex
			o := Options{Depth: 3, Workers: workers, Hops: hops, Progress: func(level, done, total int) {
				mu.Lock()
				defer mu.Unlock()
				progress = append(progress, fmt.Sprintf("%d:%d/%d", level, done, total))
			}}
			res, err := NewSession(r, o).Run(context.Background(), []Group{{Fragment: fragment.String(), Hops: 2}})
			if err != nil {
				t.Fatal(err)
			}
			var b strings.Builder
			outline(res.Nodes[0], 0, &b)
			return b.String(), res.Requests, progress, r.peak
		}
		one, oneRequests, oneProgress, _ := run(1)
		many, manyRequests, manyProgress, peak := run(4)
		if lines := strings.Count(one, "\n"); lines < 20 || !strings.Contains(one, "John 1:2") {
			t.Fatalf("hops=%v: too little of a tree to tell anything by (%d nodes):\n%s", hops, lines, one)
		}
		if one != many {
			t.Errorf("hops=%v: the tree depends on the workers:\none:\n%s\nfour:\n%s", hops, one, many)
		}
		if oneRequests != manyRequests || !slices.Equal(oneProgress, manyProgress) {
			t.Errorf("hops=%v: requests %d vs %d, progress %v vs %v", hops, oneRequests, manyRequests, oneProgress, manyProgress)
		}
		if peak < 2 || peak > 4 {
			t.Errorf("hops=%v: %d requests at once, want 2 to 4", hops, peak)
		}
	}
}

// A cancelled expansion stops handing out work and says how much it left.
func TestWorkersStopWhenCancelled(t *testing.T) {
	r := library(20 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	var refs []Ref
	for i := 1; i <= 12; i++ {
		refs = append(refs, Ref{Path: fmt.Sprintf("/wol/pc/%d", i)})
	}
	o := Options{Depth: 1, Workers: 2, Progress: func(_, done, _ int) {
		if done == 2 {
			cancel()
		}
	}}
	res, err := NewSession(r, o).Run(ctx, []Group{{RootRefs: refs}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if res.Pending == 0 || res.Pending+res.Requests != 12 {
		t.Errorf("pending %d + requests %d, want the 12 references", res.Pending, res.Requests)
	}
}
