package server

import (
	"testing"
	"time"
)

// The latest media are found anew at 6, 9, 12, 15, 18 and 21 o'clock, Berlin
// time, and not at night.
func TestLatestUpdates(t *testing.T) {
	at := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, latestZone) }
	for _, c := range []struct {
		now, last, next time.Time
	}{
		{at(22, 5, 59), at(21, 21, 0), at(22, 6, 0)},
		{at(22, 6, 0), at(22, 6, 0), at(22, 9, 0)},
		{at(22, 10, 30), at(22, 9, 0), at(22, 12, 0)},
		{at(22, 21, 0), at(22, 21, 0), at(23, 6, 0)},
		{at(22, 23, 30), at(22, 21, 0), at(23, 6, 0)},
		// the night the clocks go back
		{at(25, 2, 30), at(24, 21, 0), at(25, 6, 0)},
	} {
		if got := lastUpdate(c.now); !got.Equal(c.last) {
			t.Errorf("lastUpdate(%s) = %s, want %s", c.now, got, c.last)
		}
		if got := nextUpdate(c.now); !got.Equal(c.next) {
			t.Errorf("nextUpdate(%s) = %s, want %s", c.now, got, c.next)
		}
	}
	// whatever the server's own zone
	utc := time.Date(2026, 7, 1, 4, 30, 0, 0, time.UTC) // 6:30 in Berlin
	if got := lastUpdate(utc); !got.Equal(time.Date(2026, 7, 1, 6, 0, 0, 0, latestZone)) {
		t.Errorf("lastUpdate(%s) = %s", utc, got)
	}
}
