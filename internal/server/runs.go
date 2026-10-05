package server

import (
	"regexp"
	"sync"
	"time"

	"github.com/dgrieser/jw-cli/internal/unfold"
)

// runs keeps the verses shown by each run of a page — an "unfold all" over its
// verses or paragraphs, several of them streaming at once — so that a verse one
// stream shows is not shown again by another. A run is named by the page
// (?run=) and forgotten once it has not been used for a while.
type runs struct {
	mu   sync.Mutex
	sets map[string]*run
}

type run struct {
	verses *unfold.Verses
	used   time.Time
}

// runIdle is how long a run is kept after its last stream; maxRuns bounds how
// many are kept at all, the oldest going first.
const (
	runIdle = time.Hour
	maxRuns = 256
)

// runID is what a page names a run by: a random token of its own.
var runID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// verses is the set of the run named id, made on first use; a stream that is
// part of no run gets one of its own.
func (rs *runs) verses(id string) *unfold.Verses {
	if !runID.MatchString(id) {
		return unfold.NewVerses()
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	now := time.Now()
	if rs.sets == nil {
		rs.sets = map[string]*run{}
	}
	if r, ok := rs.sets[id]; ok {
		r.used = now
		return r.verses
	}
	var oldest string
	for k, r := range rs.sets {
		if now.Sub(r.used) > runIdle {
			delete(rs.sets, k)
			continue
		}
		if oldest == "" || r.used.Before(rs.sets[oldest].used) {
			oldest = k
		}
	}
	if len(rs.sets) >= maxRuns {
		delete(rs.sets, oldest)
	}
	r := &run{verses: unfold.NewVerses(), used: now}
	rs.sets[id] = r
	return r.verses
}
