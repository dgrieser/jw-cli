package unfold

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/model"
)

// link writes a citation the way wol does inside a document body.
func link(path, text string) string {
	return fmt.Sprintf(`<a href="%s" class="b">%s</a>`, path, text)
}

// fakeResolver answers from a table and records the order of the requests.
type fakeResolver struct {
	content map[string]model.Tooltip
	fail    map[string]bool
	asked   []string
}

func (f *fakeResolver) Resolve(_ context.Context, path string) (model.Tooltip, error) {
	f.asked = append(f.asked, path)
	if f.fail[path] {
		return model.Tooltip{}, errors.New("boom")
	}
	tip, ok := f.content[path]
	if !ok {
		return model.Tooltip{}, fmt.Errorf("no content for %s", path)
	}
	return tip, nil
}

func TestRefs(t *testing.T) {
	body := `<p>` +
		link("/wol/bc/1/1", "Matt 24:14") +
		link("/wol/pc/2/2", "w24.05 2 ¶3") +
		// not citations: a verse-number link into the concordance and a footnote
		`<a href="/wol/dx/3/3">14</a><a href="/wol/fn/4/4">*</a>` +
		// a repeat of the first, which must not be listed twice
		link("/wol/bc/1/1", "Matt 24:14") +
		`</p>`
	refs := Refs(body)
	if len(refs) != 2 {
		t.Fatalf("got %d refs, want 2: %+v", len(refs), refs)
	}
	if refs[0].Path != "/wol/bc/1/1" || refs[0].Text != "Matt 24:14" {
		t.Errorf("first ref = %+v", refs[0])
	}
	if refs[1].Path != "/wol/pc/2/2" {
		t.Errorf("second ref = %+v", refs[1])
	}
	if got := Count(body); got != 2 {
		t.Errorf("Count = %d, want 2", got)
	}
	if Refs("") != nil {
		t.Error("an empty fragment has no refs")
	}
}

func TestRunDepthZeroExpandsNothing(t *testing.T) {
	r := &fakeResolver{}
	res, err := Run(context.Background(), r, link("/wol/bc/1/1", "Matt 24:14"), Options{Depth: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Nodes) != 0 || len(r.asked) != 0 {
		t.Errorf("depth 0 expanded something: nodes=%d asked=%v", len(res.Nodes), r.asked)
	}
}

func TestRunDepthOne(t *testing.T) {
	r := &fakeResolver{content: map[string]model.Tooltip{
		"/wol/bc/1/1": {Title: "Matthew 24:14", ContentHTML: "<p>this good news " + link("/wol/bc/9/9", "+") + "</p>"},
	}}
	body := link("/wol/bc/1/1", "Matt 24:14")
	res, err := Run(context.Background(), r, body, Options{Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Nodes) != 1 {
		t.Fatalf("got %d nodes", len(res.Nodes))
	}
	n := res.Nodes[0]
	if n.Title != "Matthew 24:14" || !strings.Contains(n.HTML, "this good news") {
		t.Errorf("node = %+v", n)
	}
	if len(n.Children) != 0 {
		t.Error("depth 1 must not expand the reference found inside the content")
	}
	// that unexpanded reference is still counted, so the caller can say so
	if res.Pending != 1 {
		t.Errorf("Pending = %d, want 1", res.Pending)
	}
	if res.Requests != 1 {
		t.Errorf("Requests = %d, want 1", res.Requests)
	}
}

func TestRunDepthTwoFollowsContent(t *testing.T) {
	r := &fakeResolver{content: map[string]model.Tooltip{
		"/wol/bc/1/1": {Title: "Matthew 24:14", ContentHTML: "<p>a " + link("/wol/bc/9/9", "+") + "</p>"},
		"/wol/bc/9/9": {Title: "Isaiah 2:2", ContentHTML: "<p>b</p>"},
	}}
	res, err := Run(context.Background(), r, link("/wol/bc/1/1", "Matt 24:14"), Options{Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Nodes) != 1 || len(res.Nodes[0].Children) != 1 {
		t.Fatalf("nesting wrong: %+v", res.Nodes)
	}
	if got := res.Nodes[0].Children[0].Title; got != "Isaiah 2:2" {
		t.Errorf("child title = %q", got)
	}
	if res.Requests != 2 || res.Pending != 0 {
		t.Errorf("Requests=%d Pending=%d", res.Requests, res.Pending)
	}
}

// TestRunStopsOnCycle covers two passages citing each other: every path is
// expanded once across the whole tree, so the walk terminates.
func TestRunStopsOnCycle(t *testing.T) {
	r := &fakeResolver{content: map[string]model.Tooltip{
		"/wol/bc/a": {Title: "A", ContentHTML: link("/wol/bc/b", "B")},
		"/wol/bc/b": {Title: "B", ContentHTML: link("/wol/bc/a", "A")},
	}}
	res, err := Run(context.Background(), r, link("/wol/bc/a", "A"), Options{Depth: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Requests != 2 {
		t.Errorf("Requests = %d, want 2 (each path once)", res.Requests)
	}
	if len(r.asked) != 2 {
		t.Errorf("resolver asked %v", r.asked)
	}
}

func TestRunConfirmDeclined(t *testing.T) {
	r := &fakeResolver{content: map[string]model.Tooltip{"/wol/bc/1": {Title: "A"}, "/wol/bc/2": {Title: "B"}}}
	var askedLevel, askedRequests int
	res, err := Run(context.Background(), r, link("/wol/bc/1", "A")+link("/wol/bc/2", "B"), Options{
		Depth: 1,
		Confirm: func(level, requests int) (bool, error) {
			askedLevel, askedRequests = level, requests
			return false, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if askedLevel != 1 || askedRequests != 2 {
		t.Errorf("confirm asked with level=%d requests=%d, want 1 and 2", askedLevel, askedRequests)
	}
	if len(r.asked) != 0 {
		t.Errorf("declining must not spend requests, spent %v", r.asked)
	}
	if !res.Stopped || res.Pending != 2 {
		t.Errorf("Stopped=%v Pending=%d", res.Stopped, res.Pending)
	}
}

// TestRunConfirmThreshold keeps the prompt for the levels that are actually
// large; a handful of references must go through unasked.
func TestRunConfirmThreshold(t *testing.T) {
	var body strings.Builder
	content := map[string]model.Tooltip{}
	for i := range 3 {
		path := fmt.Sprintf("/wol/bc/%d", i)
		body.WriteString(link(path, "ref"))
		content[path] = model.Tooltip{Title: "x"}
	}
	r := &fakeResolver{content: content}
	asked := false
	res, err := Run(context.Background(), r, body.String(), Options{
		Depth:     1,
		Threshold: 5,
		Confirm:   func(int, int) (bool, error) { asked = true; return true, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if asked {
		t.Error("three references are below the threshold and must not prompt")
	}
	if res.Requests != 3 {
		t.Errorf("Requests = %d, want 3", res.Requests)
	}
}

func TestRunConfirmError(t *testing.T) {
	r := &fakeResolver{}
	wantErr := errors.New("needs --yes")
	_, err := Run(context.Background(), r, link("/wol/bc/1", "A"), Options{
		Depth:   1,
		Confirm: func(int, int) (bool, error) { return false, wantErr },
	})
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
	if len(r.asked) != 0 {
		t.Error("no requests may be spent when confirming fails")
	}
}

// TestRunKeepsGoingAfterAFailure pins that one dead reference does not sink the
// rest: it is recorded on the node so the output can say so.
func TestRunKeepsGoingAfterAFailure(t *testing.T) {
	r := &fakeResolver{
		content: map[string]model.Tooltip{"/wol/bc/2": {Title: "B"}},
		fail:    map[string]bool{"/wol/bc/1": true},
	}
	res, err := Run(context.Background(), r, link("/wol/bc/1", "A")+link("/wol/bc/2", "B"), Options{Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Nodes) != 2 {
		t.Fatalf("got %d nodes", len(res.Nodes))
	}
	if res.Nodes[0].Err == nil {
		t.Error("the failed reference should carry its error")
	}
	if res.Nodes[1].Title != "B" {
		t.Errorf("the second reference should still resolve: %+v", res.Nodes[1])
	}
}

func TestRunHonorsCancellation(t *testing.T) {
	r := &fakeResolver{content: map[string]model.Tooltip{"/wol/bc/1": {Title: "A"}, "/wol/bc/2": {Title: "B"}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, r, link("/wol/bc/1", "A")+link("/wol/bc/2", "B"), Options{Depth: 1})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if len(r.asked) != 0 {
		t.Errorf("a cancelled context must stop before requesting, spent %v", r.asked)
	}
}

func TestRunProgressReportsEachLevel(t *testing.T) {
	r := &fakeResolver{content: map[string]model.Tooltip{
		"/wol/bc/1": {Title: "A", ContentHTML: link("/wol/bc/2", "B")},
		"/wol/bc/2": {Title: "B"},
	}}
	var seen []string
	_, err := Run(context.Background(), r, link("/wol/bc/1", "A"), Options{
		Depth:    2,
		Progress: func(level, done, total int) { seen = append(seen, fmt.Sprintf("%d:%d/%d", level, done, total)) },
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1:1/1", "2:1/1"}
	if strings.Join(seen, " ") != strings.Join(want, " ") {
		t.Errorf("progress = %v, want %v", seen, want)
	}
}
