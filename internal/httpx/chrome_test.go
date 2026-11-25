package httpx

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	utls "github.com/refraction-networking/utls"
)

// grease reports one of the reserved values Chrome scatters through its
// ClientHello (0x0a0a, 0x1a1a, …), which Go's own handshake never sends.
func grease(v uint16) bool { return v&0x0f0f == 0x0a0a && v>>8 == v&0xff }

// chromeServer is a TLS server that notes the ClientHello of each
// connection, speaking HTTP/2 or HTTP/1.1 alone.
func chromeServer(t *testing.T, h2 bool) (*httptest.Server, *chromeTransport, func() []*tls.ClientHelloInfo) {
	t.Helper()
	var mu sync.Mutex
	var hellos []*tls.ClientHelloInfo
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Proto", r.Proto)
		_, _ = w.Write([]byte("page"))
	}))
	srv.EnableHTTP2 = h2
	srv.TLS = &tls.Config{GetConfigForClient: func(h *tls.ClientHelloInfo) (*tls.Config, error) {
		mu.Lock()
		defer mu.Unlock()
		hellos = append(hellos, h)
		return nil, nil
	}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	tr := newChromeTransport()
	tr.tlsConfig = &utls.Config{RootCAs: srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs}
	return srv, tr, func() []*tls.ClientHelloInfo {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(hellos)
	}
}

func chromeGet(t *testing.T, tr http.RoundTripper, u string) (proto, body string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.Header.Get("X-Proto"), string(b)
}

// The connection to the site says Chrome: a ClientHello with Chrome's
// reserved values in its cipher suites and both protocols on offer, spoken
// on over HTTP/2 on one connection that is kept.
func TestChromeTransportHTTP2(t *testing.T) {
	srv, tr, hellos := chromeServer(t, true)
	for range 3 {
		if proto, body := chromeGet(t, tr, srv.URL+"/a"); proto != "HTTP/2.0" || body != "page" {
			t.Fatalf("proto %q body %q", proto, body)
		}
	}
	seen := hellos()
	if len(seen) != 1 {
		t.Fatalf("%d connections for 3 requests, want 1 kept", len(seen))
	}
	h := seen[0]
	if !slices.ContainsFunc(h.CipherSuites, grease) {
		t.Errorf("cipher suites without Chrome's reserved values — Go's own hello? %x", h.CipherSuites)
	}
	if !slices.Equal(h.SupportedProtos, []string{"h2", "http/1.1"}) {
		t.Errorf("protocols offered: %v", h.SupportedProtos)
	}
}

// A host that speaks no HTTP/2 — a proxy reading TLS in between — is
// spoken to over HTTP/1.1, the hello still Chrome's but for the protocols.
func TestChromeTransportHTTP1(t *testing.T) {
	srv, tr, hellos := chromeServer(t, false)
	for range 2 {
		if proto, body := chromeGet(t, tr, srv.URL+"/a"); proto != "HTTP/1.1" || body != "page" {
			t.Fatalf("proto %q body %q", proto, body)
		}
	}
	seen := hellos()
	last := seen[len(seen)-1]
	if !slices.ContainsFunc(last.CipherSuites, grease) {
		t.Errorf("the HTTP/1.1 hello is not Chrome's: %x", last.CipherSuites)
	}
	if !slices.Equal(last.SupportedProtos, []string{"http/1.1"}) {
		t.Errorf("protocols offered: %v", last.SupportedProtos)
	}
}

// Only the https site named gets the transport: a test's http server and
// JW_JWORG_TLS=go keep Go's own.
func TestChromeForScope(t *testing.T) {
	if _, ok := chromeFor("http://127.0.0.1:1", nil).(*hostTransport); ok {
		t.Error("an http site got the Chrome transport")
	}
	ht, ok := chromeFor("https://www.jw.org", nil).(*hostTransport)
	if !ok || ht.host != "www.jw.org" {
		t.Fatalf("www.jw.org without the Chrome transport: %#v", ht)
	}
	t.Setenv("JW_JWORG_TLS", "go")
	if _, ok := chromeFor("https://www.jw.org", nil).(*hostTransport); ok {
		t.Error("JW_JWORG_TLS=go did not turn the Chrome transport off")
	}
}
