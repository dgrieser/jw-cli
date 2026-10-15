package server

import (
	"strconv"
	"strings"
)

// mediaClip is the stretch of a recording a link names — a verse's clip,
// which jw.org links as "finder?lank=pub-gnj_3_VIDEO&ts=00:05:02.26-00:07:52.31"
// — in seconds; End is zero for a stretch that runs to the end.
type mediaClip struct {
	Start, End float64
}

// Fragment is the clip as a media fragment of the file's address (#t=start,end):
// the player starts there and pauses at the clip's end, script or no script.
func (c mediaClip) Fragment() string {
	f := "#t=" + seconds(c.Start)
	if c.End > 0 {
		f += "," + seconds(c.End)
	}
	return f
}

func seconds(s float64) string { return strconv.FormatFloat(s, 'f', -1, 64) }

// parseClip reads a clip as jw.org writes it, "HH:MM:SS.ss-HH:MM:SS.ss" (either
// side may be shorter: "5:02", "302.26"), nil for none or anything else.
func parseClip(ts string) *mediaClip {
	from, to, ranged := strings.Cut(strings.TrimSpace(ts), "-")
	start, ok := parseClock(from)
	if !ok {
		return nil
	}
	c := &mediaClip{Start: start}
	if ranged {
		end, ok := parseClock(to)
		if !ok || end <= start {
			return nil
		}
		c.End = end
	}
	if c.Start == 0 && c.End == 0 {
		return nil
	}
	return c
}

// parseClock reads "HH:MM:SS.ss", "MM:SS" or plain seconds.
func parseClock(s string) (float64, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) > 3 || parts[0] == "" {
		return 0, false
	}
	var sec float64
	for i, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v < 0 || (i < len(parts)-1 && v != float64(int(v))) {
			return 0, false
		}
		sec = sec*60 + v
	}
	return sec, true
}
