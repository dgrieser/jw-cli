package server

import "testing"

// A clip is read as jw.org's links write it, and anything else is no clip.
func TestParseClip(t *testing.T) {
	for in, want := range map[string]*mediaClip{
		"00:05:02.26-00:07:52.31": {Start: 302.26, End: 472.31},
		"5:02-7:52":               {Start: 302, End: 472},
		"302.26":                  {Start: 302.26},
		"":                        nil,
		"0":                       nil,
		"00:07:52-00:05:02":       nil,
		"x-y":                     nil,
		"1:2:3:4":                 nil,
		"1.5:00":                  nil,
	} {
		got := parseClip(in)
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Errorf("parseClip(%q) = %+v, want %+v", in, got, want)
		}
	}
	if f := (mediaClip{Start: 302.26, End: 472.31}).Fragment(); f != "#t=302.26,472.31" {
		t.Errorf("fragment %q", f)
	}
}
