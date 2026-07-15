package lang

import "testing"

func TestDetectBCP47(t *testing.T) {
	cases := []struct {
		lcAll, lcMessages, lang string
		want                    string
	}{
		{"de_DE.UTF-8", "", "", "de-DE"},
		{"", "fr_FR", "", "fr-FR"},
		{"", "", "es_MX.UTF-8@euro", "es-MX"},
		{"C", "", "en_US.UTF-8", "en-US"},
		{"POSIX", "", "", "en"},
		{"", "", "", "en"},
		{"pt_BR", "de_DE", "", "pt-BR"},
	}
	for _, c := range cases {
		t.Setenv("LC_ALL", c.lcAll)
		t.Setenv("LC_MESSAGES", c.lcMessages)
		t.Setenv("LANG", c.lang)
		if got := DetectBCP47(); got != c.want {
			t.Errorf("LC_ALL=%q LC_MESSAGES=%q LANG=%q: got %q, want %q",
				c.lcAll, c.lcMessages, c.lang, got, c.want)
		}
	}
}
