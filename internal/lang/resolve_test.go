package lang

import (
	"context"
	"testing"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
)

type fakeSource struct{ langs []model.Language }

func (f fakeSource) Languages(context.Context) ([]model.Language, error) { return f.langs, nil }

func testResolver(t *testing.T) *Resolver {
	t.Helper()
	return &Resolver{
		Source: fakeSource{langs: []model.Language{
			{Symbol: "E", Locale: "en", Name: "English"},
			{Symbol: "X", Locale: "de", Name: "German"},
			{Symbol: "F", Locale: "fr", Name: "French"},
			{Symbol: "T", Locale: "pt", Name: "Portuguese"},
			{Symbol: "TPO", Locale: "pt-pt", Name: "Portuguese (Portugal)"},
		}},
		Cache: httpx.OpenCacheAt(t.TempDir()),
	}
}

func TestResolve(t *testing.T) {
	r := testResolver(t)
	ctx := context.Background()
	cases := []struct {
		spec string
		want string // expected symbol
	}{
		{"X", "X"},
		{"x", "X"},
		{"de", "X"},
		{"de-AT", "X"}, // BCP-47 matcher
		{"fr", "F"},
		{"pt-PT", "TPO"},
		{"pt-BR", "T"},
		{"en-GB", "E"},
	}
	for _, c := range cases {
		got, err := r.Resolve(ctx, c.spec)
		if err != nil {
			t.Errorf("Resolve(%q): %v", c.spec, err)
			continue
		}
		if got.Symbol != c.want {
			t.Errorf("Resolve(%q) = %s, want %s", c.spec, got.Symbol, c.want)
		}
	}
}

func TestResolveDefaultUsesSystemLocale(t *testing.T) {
	r := testResolver(t)
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	got, err := r.Resolve(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Symbol != "X" {
		t.Errorf("got %s, want X", got.Symbol)
	}
}

func TestResolveUnknown(t *testing.T) {
	r := testResolver(t)
	if _, err := r.Resolve(context.Background(), "zz-ZZ"); err == nil {
		t.Error("expected error for unknown language")
	}
}
