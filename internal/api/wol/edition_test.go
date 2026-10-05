package wol

import (
	"net/url"
	"testing"
)

func TestEditions(t *testing.T) {
	e := Editions{Host: "wol.jw.org"}
	page := []byte(`<input id="libTitle" type="hidden" value="Deutsche Publikationen (1950-2027)" />`)
	if scope, n, ok := e.Edition("https://wol.jw.org/de/wol/library/r10/lp-x", page); !ok || scope != "Deutsche Publikationen" || n != 2027 {
		t.Fatalf("edition: %q %d %v", scope, n, ok)
	}
	if _, _, ok := e.Edition("https://www.jw.org/de/", page); ok {
		t.Fatal("another host's page told apart")
	}
	for title, want := range map[string]int{
		"English Publications (1950-2026)":     2026,
		"Publicaciones en español (1950–2027)": 2027,
		"Library (2027)":                       2027,
		"Publications (no years)":              0,
		"Publications (1950-9999)":             0,
	} {
		_, n, ok := libraryEdition(title)
		if n != want || ok != (want != 0) {
			t.Errorf("%q: %d %v, want %d", title, n, ok, want)
		}
	}
	b := e.Bust("https://wol.jw.org/de/wol/library/r10/lp-x/alle-publikationen?x=1", 2)
	u, err := url.Parse(b)
	if err != nil || u.Query().Get("x") != "1" || u.Query().Get(bustParam) == "" || u.Path != "/de/wol/library/r10/lp-x/alle-publikationen" {
		t.Fatalf("bust: %q", b)
	}
	if b == e.Bust("https://wol.jw.org/de/wol/library/r10/lp-x/alle-publikationen?x=1", 2) {
		t.Fatal("bust repeats an address")
	}
	if e.Bust("https://b.jw-cdn.org/apis/x", 0) != "" {
		t.Fatal("bust of another host")
	}
}
