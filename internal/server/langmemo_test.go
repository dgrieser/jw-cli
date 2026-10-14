package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/service"
)

// memoServer is a Server over a mock language list, counting how often the
// list is asked for.
func memoServer(t *testing.T) (*Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		b, err := os.ReadFile(filepath.Join("testdata", "languages.json"))
		if err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
	t.Cleanup(up.Close)
	hc := httpx.New(httpx.WithBaseURLs(httpx.BaseURLs{CDN: up.URL, JWOrg: up.URL, WOL: up.URL}),
		httpx.WithRetryDelays(time.Millisecond, time.Millisecond))
	return New(Config{Svc: service.New(hc, httpx.OpenCacheAt(t.TempDir()))}), &hits
}

func memoSize(s *Server) int {
	n := 0
	s.langMemo.Range(func(any, any) bool { n++; return true })
	return n
}

// The memo keeps what the language list names a language by, not whatever a
// request made up that resolves to it.
func TestLanguageMemoBounded(t *testing.T) {
	s, _ := memoServer(t)
	ctx := context.Background()
	for _, spec := range []string{"de-x-abc1", "de-x-abc2", "de-DE-u-co-phonebk", "X", "x", "de", "DE"} {
		lng, err := s.resolveLanguage(ctx, spec)
		if err != nil || lng.Symbol != "X" {
			t.Fatalf("%s: %+v, %v", spec, lng, err)
		}
	}
	if n := memoSize(s); n != 2 {
		t.Errorf("memo holds %d entries, want 2 (x and de)", n)
	}
}

// A request reading past the cache reads the language list anew.
func TestLanguageMemoRefresh(t *testing.T) {
	s, hits := memoServer(t)
	ctx := context.Background()
	for range 2 {
		if _, err := s.resolveLanguage(ctx, "de"); err != nil {
			t.Fatal(err)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("language list read %d times, want 1", n)
	}
	if _, err := s.resolveLanguage(httpx.WithRefresh(ctx), "de"); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("a refresh read the memo: language list read %d times, want 2", n)
	}
}
