package server

import "testing"

// A section left to load leads, without scripts, to the page showing the same.
func TestLazyFallbackHref(t *testing.T) {
	fb := lazyFallback{text: "Show", lang: "de"}
	for lazy, want := range map[string]string{
		"/unfold/verse?bible=nwtsty&depth=1&part=notes&to=0&vid=43001029":              "/bible?bible=nwtsty&lang=de&to=0&unfold=1&vid=43001029",
		"/unfold/verse?bible=nwtsty&depth=1&kind=guide&part=indexes&to=0&vid=43001029": "/bible?bible=nwtsty&lang=de&to=0&unfold=1&vid=43001029",
		"/unfold/translations?bible=nwtsty&to=0&vid=43001029":                          "/bible?all=1&lang=de&to=0&vid=43001029",
		"/unfold/cited?self=2024360&to=33&vid=43014031":                                "/cited?lang=de&to=33&vid=43014031",
		"/unfold/cited?to=0&to=0&vid=1&vid=2":                                          "/cited?lang=de&to=0&to=0&vid=1&vid=2",
		"/unfold/article?url=x":                                                        "",
		"/unfold/cited":                                                                "",
	} {
		if got := fb.href(lazy); got != want {
			t.Errorf("href(%s) = %q, want %q", lazy, got, want)
		}
	}
}
