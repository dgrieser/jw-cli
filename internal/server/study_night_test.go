package server

import (
	"testing"
	"time"
)

// The study runs in the night under way, or else in the coming one.
func TestNightOf(t *testing.T) {
	at := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, time.UTC) }
	for _, c := range []struct {
		now, from, until time.Time
	}{
		{at(22, 0, 30), at(22, 1, 0), at(22, 6, 0)},
		{at(22, 3, 0), at(22, 1, 0), at(22, 6, 0)},
		{at(22, 6, 0), at(23, 1, 0), at(23, 6, 0)},
		{at(22, 23, 59), at(23, 1, 0), at(23, 6, 0)},
	} {
		from, until := nightOf(c.now)
		if !from.Equal(c.from) || !until.Equal(c.until) {
			t.Errorf("nightOf(%s) = %s, %s; want %s, %s", c.now, from, until, c.from, c.until)
		}
	}
}
