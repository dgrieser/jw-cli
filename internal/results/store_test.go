package results

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/model"
)

// Save numbers and stamps the caller's set, with or without a cache to write
// it to: the listing printed from it must be the one a later index resolves.
func TestSaveNumbersAndStampsTheCallersSet(t *testing.T) {
	for _, dir := range []string{"", t.TempDir()} {
		rs := ResultSet{Kind: "search", Items: []model.Result{{Title: "a"}, {Title: "b"}}}
		if err := Save(dir, &rs); err != nil {
			t.Fatal(err)
		}
		if rs.Timestamp.IsZero() {
			t.Errorf("dir %q: timestamp not set on the caller's set", dir)
		}
		if rs.Items[0].Index != 1 || rs.Items[1].Index != 2 {
			t.Errorf("dir %q: items not numbered: %+v", dir, rs.Items)
		}
	}

	dir := t.TempDir()
	rs := ResultSet{Kind: "search", Total: 354, Items: []model.Result{{Title: "a"}}}
	if err := Save(dir, &rs); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Total != 354 || !loaded.Timestamp.Equal(rs.Timestamp) {
		t.Errorf("loaded %+v, saved %+v", loaded, rs)
	}
}

// A row nobody numbered says nothing about its index, rather than all of them
// claiming 0.
func TestUnnumberedResultOmitsIndex(t *testing.T) {
	b, err := json.Marshal(model.Result{Kind: "video", Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"index"`) {
		t.Errorf("unnumbered result marshals an index: %s", b)
	}
}
