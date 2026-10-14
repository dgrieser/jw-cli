package service

import (
	"testing"

	"github.com/dgrieser/jw-cli/internal/model"
)

// The language q names exactly leads the matches, then the names starting with
// it, then the names holding it, each in the list's order.
func TestFilterLanguagesRanksExactFirst(t *testing.T) {
	langs := []model.Language{
		{Symbol: "BS", Locale: "bas", Name: "Bassa (Cameroon)", Vernacular: "Basaa (Kamerun)"},
		{Symbol: "BEL", Locale: "be", Name: "Belorussian"},
		{Symbol: "RU", Locale: "rw", Name: "Kinyarwanda"},
		{Symbol: "RUN", Locale: "rn", Name: "Rundi"},
		{Symbol: "U", Locale: "ru", Name: "Russian", Vernacular: "русский"},
	}
	var got []string
	for _, l := range FilterLanguages(langs, "ru") {
		got = append(got, l.Name)
	}
	want := []string{"Kinyarwanda", "Russian", "Rundi", "Bassa (Cameroon)", "Belorussian"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}
