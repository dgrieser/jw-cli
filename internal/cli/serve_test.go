package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/app"
)

// lockedBuffer lets the test read the server's log while the command goroutine
// writes it.
type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

var listenLine = regexp.MustCompile(`listening on (http://[^\s]+)`)

// TestServeSmoke boots jw serve on a free port, drives one API and one UI
// request through it, then shuts it down via context cancellation — the same
// path a signal takes.
func TestServeSmoke(t *testing.T) {
	upstream := httptest.NewServer(languagesMux(t))
	t.Cleanup(upstream.Close)

	a := app.New(app.Flags{})
	var out lockedBuffer
	a.Stdout = &out
	a.Stderr = &out

	root := NewRootCmd(a)
	root.SetArgs([]string{
		"--base-cdn", upstream.URL, "--base-jworg", upstream.URL, "--base-wol", upstream.URL,
		"--cache-dir", t.TempDir(),
		"serve", "--port", "0",
	})
	root.SetOut(&out)
	root.SetErr(&out)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()

	// wait for the startup line to learn the bound port
	var base string
	deadline := time.Now().Add(5 * time.Second)
	for base == "" {
		if time.Now().After(deadline) {
			t.Fatalf("server did not start:\n%s", out.String())
		}
		if m := listenLine.FindStringSubmatch(out.String()); m != nil {
			base = m[1]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	resp, err := http.Get(base + "/api/v1/languages")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"symbol": "X"`) {
		t.Errorf("api: status %d body %.200s", resp.StatusCode, body)
	}

	resp, err = http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(page), "Daily text") {
		t.Errorf("ui: status %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve returned %v\n%s", err, out.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not shut down after cancel")
	}

	// the request log carries method, path and status
	if !strings.Contains(out.String(), "GET /api/v1/languages 200") {
		t.Errorf("request log missing:\n%s", out.String())
	}
}

// TestServeRejectsBadAddr pins the address validation.
func TestServeRejectsBadAddr(t *testing.T) {
	a := app.New(app.Flags{})
	var out strings.Builder
	a.Stdout = &out
	a.Stderr = &out
	root := NewRootCmd(a)
	root.SetArgs([]string{"serve", "--addr", "not:a:valid:addr"})
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err == nil {
		t.Fatal("expected an address error")
	}
}

func TestLoopbackHost(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost": true,
		"127.0.0.1": true,
		"::1":       true,
		"0.0.0.0":   false,
		"10.1.2.3":  false,
	} {
		if got := loopbackHost(host); got != want {
			t.Errorf("loopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestHostForURL(t *testing.T) {
	if got := hostForURL("0.0.0.0:8080", "0.0.0.0"); got != "localhost:8080" {
		t.Errorf("wildcard bind should print localhost, got %q", got)
	}
	if got := hostForURL("127.0.0.1:8080", "127.0.0.1"); got != "127.0.0.1:8080" {
		t.Errorf("got %q", got)
	}
}
