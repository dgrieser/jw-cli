package server

import (
	"context"
	"testing"
	"time"
)

func TestSpreadOffsetsStayWithinWindow(t *testing.T) {
	for _, n := range []int{0, 1, 2, 20} {
		offsets := spreadOffsets(n, true)
		if len(offsets) != n {
			t.Fatalf("%d jobs: %d offsets", n, len(offsets))
		}
		if n == 0 {
			continue
		}
		if offsets[0] != 0 {
			t.Errorf("first job starts at %s", offsets[0])
		}
		slot := scheduledWindow / time.Duration(n)
		for i := 1; i < n; i++ {
			from := time.Duration(i) * slot
			if offsets[i] < from || offsets[i] >= from+slot {
				t.Errorf("job %d of %d starts at %s, outside [%s, %s)", i, n, offsets[i], from, from+slot)
			}
		}
	}
	for _, n := range []int{1, 2, 20} {
		offsets := spreadOffsets(n, false)
		slot := scheduledWindow / time.Duration(n)
		for i, offset := range offsets {
			from := time.Duration(i) * slot
			if offset < from || offset >= from+slot {
				t.Errorf("jittered job %d of %d starts at %s, outside [%s, %s)", i, n, offset, from, from+slot)
			}
		}
	}
}

func TestSpreadBackgroundStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ran := false
	if spreadBackground(ctx, []func(){func() { ran = true }}, true) || ran {
		t.Fatal("scheduled work ran after cancellation")
	}
}
