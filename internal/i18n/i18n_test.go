package i18n

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestCatalogsComplete walks every catalog field: the struct shape guarantees a
// message exists, this guarantees it was actually translated.
func TestCatalogsComplete(t *testing.T) {
	for l, m := range catalog {
		v := reflect.ValueOf(*m)
		typ := v.Type()
		for i := range v.NumField() {
			name := typ.Field(i).Name
			switch f := v.Field(i); f.Kind() {
			case reflect.String:
				if strings.TrimSpace(f.String()) == "" {
					t.Errorf("%s: %s is empty", l, name)
				}
			case reflect.Array:
				for j := range f.Len() {
					if strings.TrimSpace(f.Index(j).String()) == "" {
						t.Errorf("%s: %s[%d] is empty", l, name, j)
					}
				}
			case reflect.Func:
				if f.IsNil() {
					t.Errorf("%s: %s is nil", l, name)
				}
			default:
				t.Errorf("%s: %s has unhandled kind %s", l, name, f.Kind())
			}
		}
	}
}

// TestCatalogsAgreeOnVerbs catches a translation that dropped or added a format
// argument, which would print "%!s(MISSING)" at runtime.
func TestCatalogsAgreeOnVerbs(t *testing.T) {
	base := reflect.ValueOf(en)
	typ := base.Type()
	for l, m := range catalog {
		if l == EN {
			continue
		}
		v := reflect.ValueOf(*m)
		for i := range base.NumField() {
			if base.Field(i).Kind() != reflect.String {
				continue
			}
			name := typ.Field(i).Name
			if got, want := verbs(v.Field(i).String()), verbs(base.Field(i).String()); got != want {
				t.Errorf("%s: %s has verbs %q, English has %q", l, name, got, want)
			}
		}
	}
}

// verbs reduces a format string to its conversion verbs, ignoring "%%".
func verbs(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' || i+1 >= len(s) {
			continue
		}
		i++
		if s[i] == '%' {
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func TestMatch(t *testing.T) {
	for in, want := range map[string]Lang{
		"de": DE, "de-AT": DE, "de_DE": DE, "DE": DE,
		"en": EN, "en-US": EN, "": EN,
		"fr": EN, "es-ES": EN, // unsupported: fall back
		"X": EN, "E": EN, // JW symbols are not language tags
		"nonsense": EN,
	} {
		if got := Match(in); got != want {
			t.Errorf("Match(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestDate(t *testing.T) {
	d := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC) // a Thursday
	if got, want := EN.Text().Date(d), "Thursday, July 30, 2026"; got != want {
		t.Errorf("en: %q, want %q", got, want)
	}
	if got, want := DE.Text().Date(d), "Donnerstag, 30. Juli 2026"; got != want {
		t.Errorf("de: %q, want %q", got, want)
	}
}

func TestResultsPlural(t *testing.T) {
	for _, tc := range []struct {
		lang  Lang
		total int
		want  string
	}{
		{EN, 1, `1 result for "Caleb"`},
		{EN, 2, `2 results for "Caleb"`},
		{EN, 0, `0 results for "Caleb"`},
		{DE, 1, `1 Ergebnis für "Caleb"`},
		{DE, 2, `2 Ergebnisse für "Caleb"`},
	} {
		if got := tc.lang.Text().Results(tc.total, "Caleb"); got != tc.want {
			t.Errorf("%v/%d: %q, want %q", tc.lang, tc.total, got, tc.want)
		}
	}
}

func TestWolResults(t *testing.T) {
	if got, want := EN.Text().WolResults(0, "Caleb", 2), `wol results for "Caleb" (page 2)`; got != want {
		t.Errorf("unknown total: %q, want %q", got, want)
	}
	if got, want := DE.Text().WolResults(5, "Caleb", 2), `5 wol-Ergebnisse für "Caleb" (Seite 2)`; got != want {
		t.Errorf("de: %q, want %q", got, want)
	}
}

func TestTextForFallsBackToEnglish(t *testing.T) {
	if TextFor("fr").NoResults != en.NoResults {
		t.Error("an unsupported language must get English text")
	}
	if TextFor("de-CH").NoResults != de.NoResults {
		t.Error("de-CH should match German")
	}
}
