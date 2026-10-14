// Package results persists the most recent numbered listing (search results,
// browse pages, file lists) so follow-up commands — jw show N, jw open N,
// jw download N — can act on an index without re-querying.
package results

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dgrieser/jw-cli/internal/model"
)

const fileName = "last-results.json"

// ResultSet is one saved listing.
type ResultSet struct {
	Kind  string `json:"kind"` // search|media-browse|pub-files|bible-media|article-images
	Query string `json:"query,omitempty"`
	Lang  string `json:"lang,omitempty"`
	Page  int    `json:"page,omitempty"`
	// Total is how many results the engine reports for the whole query, of
	// which Items is one page; zero when it reports none.
	Total     int            `json:"total,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Items     []model.Result `json:"items"`

	// Notes say what the listing could not include — a lookup that failed —
	// so a partial answer says it is partial. Already in the user's language.
	Notes []string `json:"notes,omitempty"`
}

// Save numbers and stamps the result set, then writes it into dir (the app
// cache dir). The numbers and the stamp land in the caller's set, cache or no
// cache, so a listing prints — and -o json shows — the set exactly as saved.
func Save(dir string, rs *ResultSet) error {
	rs.Timestamp = time.Now()
	for i := range rs.Items {
		rs.Items[i].Index = i + 1
	}
	if dir == "" {
		return nil // cache disabled; indexes just won't be resolvable later
	}
	b, err := json.MarshalIndent(rs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, fileName), b, 0o644)
}

// Load reads the last saved result set from dir.
func Load(dir string) (ResultSet, error) {
	var rs ResultSet
	if dir == "" {
		return rs, errors.New("no cached results (cache directory unavailable)")
	}
	b, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		return rs, errors.New("no cached results; run a listing command like 'jw search' first")
	}
	if err := json.Unmarshal(b, &rs); err != nil {
		return rs, fmt.Errorf("corrupt results cache: %w", err)
	}
	return rs, nil
}

// Resolve returns item number index (1-based, as printed) from the last
// saved listing.
func Resolve(dir string, index int) (model.Result, error) {
	rs, err := Load(dir)
	if err != nil {
		return model.Result{}, err
	}
	if index < 1 || index > len(rs.Items) {
		return model.Result{}, fmt.Errorf("index %d out of range: last listing had %d items", index, len(rs.Items))
	}
	return rs.Items[index-1], nil
}
