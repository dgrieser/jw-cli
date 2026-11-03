package server

import (
	"context"
	"math/rand/v2"
	"time"
)

// scheduledWindow spreads periodic server work without delaying work a reader
// triggered. Jobs start in random order, one per slot within this window.
// Warm starts its first job immediately so startup still fills the start page.
const scheduledWindow = 15 * time.Minute

func spreadBackground(ctx context.Context, jobs []func(), immediate bool) bool {
	if len(jobs) == 0 {
		return true
	}
	if immediate {
		rand.Shuffle(len(jobs)-1, func(i, j int) { jobs[i+1], jobs[j+1] = jobs[j+1], jobs[i+1] })
	} else {
		rand.Shuffle(len(jobs), func(i, j int) { jobs[i], jobs[j] = jobs[j], jobs[i] })
	}
	start := time.Now()
	offsets := spreadOffsets(len(jobs), immediate)
	for i, job := range jobs {
		if i > 0 || !immediate {
			if !sleepUntil(ctx, start.Add(offsets[i])) {
				return false
			}
		} else if ctx.Err() != nil {
			return false
		}
		job()
	}
	return true
}

func spreadOffsets(n int, immediate bool) []time.Duration {
	offsets := make([]time.Duration, n)
	if n == 0 {
		return offsets
	}
	slot := scheduledWindow / time.Duration(n)
	first := 0
	if immediate {
		first = 1
	}
	for i := first; i < n; i++ {
		offsets[i] = time.Duration(i)*slot + time.Duration(rand.Int64N(int64(slot)))
	}
	return offsets
}
