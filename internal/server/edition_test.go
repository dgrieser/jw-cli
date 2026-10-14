package server

import "testing"

// An edition is named as wol names it, capitals included: the Reference
// Bible is Rbi8, and the reader in it unfolds its verses like any other.
func TestEditionSymbol(t *testing.T) {
	for _, ok := range []string{"nwtsty", "Rbi8", "int", "bi12"} {
		if !editionSymbol.MatchString(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	for _, bad := range []string{"", "../d", "nwt sty", "nwtsty/43", "abcdefghijklmnopq"} {
		if editionSymbol.MatchString(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}
