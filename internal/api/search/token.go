package search

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// TokenSource fetches and caches the anonymous JWT that the search API
// requires (the same token the jw.org search page uses).
type TokenSource struct {
	fetch func(ctx context.Context) (string, error)

	mu  sync.Mutex
	tok string
	exp time.Time
}

func NewTokenSource(fetch func(ctx context.Context) (string, error)) *TokenSource {
	return &TokenSource{fetch: fetch}
}

// Token returns a cached token or fetches a fresh one.
func (t *TokenSource) Token(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tok != "" && time.Now().Before(t.exp) {
		return t.tok, nil
	}
	raw, err := t.fetch(ctx)
	if err != nil {
		return "", err
	}
	tok := strings.Trim(strings.TrimSpace(raw), `"`)
	t.tok = tok
	t.exp = expiryOf(tok)
	return tok, nil
}

// Invalidate drops the cached token (call after a 401).
func (t *TokenSource) Invalidate() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tok = ""
}

// expiryOf reads the exp claim; unparseable tokens get a short safety TTL.
func expiryOf(tok string) time.Time {
	fallback := time.Now().Add(4 * time.Minute)
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return fallback
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fallback
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp == 0 {
		return fallback
	}
	// refresh a bit before the real expiry
	return time.Unix(claims.Exp, 0).Add(-30 * time.Second)
}
