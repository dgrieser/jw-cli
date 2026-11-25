package httpx

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

// www.jw.org's bot management turns some networks away although every
// header of a request is Chrome's: the request says Chrome, and the
// connection under it — the TLS handshake Go's own stack sends, its HTTP/2
// settings — says Go. chromeTransport makes the connection say Chrome too:
// Chrome's TLS ClientHello (uTLS), HTTP/2 where the site speaks it, with the
// settings of Chrome's that the HTTP/2 stack lets one choose. It serves
// www.jw.org alone; every other host keeps Go's own transport.
//
// JW_JWORG_TLS=go turns it off, for telling whether it is what lets a
// network through.

// chromeHello is the browser whose ClientHello the connection sends.
var chromeHello = utls.HelloChrome_Auto

// Chrome's HTTP/2 settings, as far as the transport takes them.
const (
	chromeHeaderTableSize = 65536
	chromeMaxHeaderList   = 262144
)

// hostTransport sends the requests to one host through a transport of its
// own and all others through the usual one.
type hostTransport struct {
	host  string
	own   http.RoundTripper
	other http.RoundTripper
}

func (t *hostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "https" && req.URL.Host == t.host {
		return t.own.RoundTrip(req)
	}
	return t.other.RoundTrip(req)
}

// chromeTransport speaks to its hosts as Chrome does. A host that answers
// the handshake with HTTP/2 is spoken to over one connection, kept; one that
// does not — a proxy that reads TLS in between — over HTTP/1.1.
type chromeTransport struct {
	mu     sync.Mutex
	conns  map[string]*http2.ClientConn
	h1only map[string]bool
	h2     *http2.Transport
	h1     *http.Transport
	dialer net.Dialer
	// tlsConfig is the base of every handshake's; tests trust their server
	// through it
	tlsConfig *utls.Config
}

func newChromeTransport() *chromeTransport {
	t := &chromeTransport{
		conns:  map[string]*http2.ClientConn{},
		h1only: map[string]bool{},
		dialer: net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second},
	}
	t.h2 = &http2.Transport{
		MaxHeaderListSize:         chromeMaxHeaderList,
		MaxDecoderHeaderTableSize: chromeHeaderTableSize,
		ReadIdleTimeout:           30 * time.Second,
		PingTimeout:               15 * time.Second,
	}
	t.h1 = &http.Transport{
		DialTLSContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			conn, _, err := t.dial(ctx, addr, false)
			return conn, err
		},
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	}
	return t
}

func (t *chromeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	addr := req.URL.Host
	if req.URL.Port() == "" {
		addr = net.JoinHostPort(req.URL.Hostname(), "443")
	}
	for attempt := 0; ; attempt++ {
		cc, err := t.conn(req.Context(), addr)
		if err != nil {
			return nil, err
		}
		if cc == nil {
			return t.h1.RoundTrip(req)
		}
		resp, err := cc.RoundTrip(req)
		// a kept connection the site has closed since: once more on a new
		// one, for a request that can be sent again
		if err != nil && attempt == 0 && req.Body == nil && req.Context().Err() == nil {
			t.drop(addr, cc)
			continue
		}
		return resp, err
	}
}

// conn is the HTTP/2 connection to addr, opened when there is none to use;
// nil for a host spoken to over HTTP/1.1.
func (t *chromeTransport) conn(ctx context.Context, addr string) (*http2.ClientConn, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.h1only[addr] {
		return nil, nil
	}
	if cc := t.conns[addr]; cc != nil && cc.CanTakeNewRequest() {
		return cc, nil
	}
	conn, proto, err := t.dial(ctx, addr, true)
	if err != nil {
		return nil, err
	}
	if proto != http2.NextProtoTLS {
		_ = conn.Close()
		t.h1only[addr] = true
		return nil, nil
	}
	cc, err := t.h2.NewClientConn(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	t.conns[addr] = cc
	return cc, nil
}

func (t *chromeTransport) drop(addr string, cc *http2.ClientConn) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.conns[addr] == cc {
		delete(t.conns, addr)
	}
}

// dial connects to addr — through the proxy the environment names, if any —
// and shakes hands as Chrome does. With h2 the handshake is Chrome's as it
// is and the protocol the host chose is returned; without, it offers
// HTTP/1.1 alone, for the transport that speaks nothing else.
func (t *chromeTransport) dial(ctx context.Context, addr string, h2 bool) (net.Conn, string, error) {
	raw, err := t.dialRaw(ctx, addr)
	if err != nil {
		return nil, "", err
	}
	host, _, _ := net.SplitHostPort(addr)
	cfg := &utls.Config{ServerName: host}
	if t.tlsConfig != nil {
		cfg = t.tlsConfig.Clone()
		cfg.ServerName = host
	}
	var uconn *utls.UConn
	if h2 {
		uconn = utls.UClient(raw, cfg, chromeHello)
	} else {
		spec, err := utls.UTLSIdToSpec(chromeHello)
		if err != nil {
			_ = raw.Close()
			return nil, "", err
		}
		for _, ext := range spec.Extensions {
			if alpn, ok := ext.(*utls.ALPNExtension); ok {
				alpn.AlpnProtocols = []string{"http/1.1"}
			}
		}
		uconn = utls.UClient(raw, cfg, utls.HelloCustom)
		if err := uconn.ApplyPreset(&spec); err != nil {
			_ = raw.Close()
			return nil, "", err
		}
	}
	if err := uconn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, "", fmt.Errorf("TLS handshake with %s: %w", addr, err)
	}
	return uconn, uconn.ConnectionState().NegotiatedProtocol, nil
}

// dialRaw opens the connection the handshake runs over: to addr itself, or
// a tunnel to it through the proxy of HTTPS_PROXY (NO_PROXY honored), as
// Go's own transport does.
func (t *chromeTransport) dialRaw(ctx context.Context, addr string) (net.Conn, error) {
	proxy, err := http.ProxyFromEnvironment(&http.Request{URL: &url.URL{Scheme: "https", Host: addr}})
	if err != nil {
		return nil, err
	}
	if proxy == nil {
		return t.dialer.DialContext(ctx, "tcp", addr)
	}
	paddr := proxy.Host
	if proxy.Port() == "" {
		port := "80"
		if proxy.Scheme == "https" {
			port = "443"
		}
		paddr = net.JoinHostPort(proxy.Hostname(), port)
	}
	conn, err := t.dialer.DialContext(ctx, "tcp", paddr)
	if err != nil {
		return nil, fmt.Errorf("proxy %s: %w", paddr, err)
	}
	switch proxy.Scheme {
	case "http":
	case "https":
		tc := tls.Client(conn, &tls.Config{ServerName: proxy.Hostname()})
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("proxy %s: %w", paddr, err)
		}
		conn = tc
	default:
		_ = conn.Close()
		return nil, fmt.Errorf("proxy scheme %q is not supported for %s", proxy.Scheme, addr)
	}
	connect := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: addr},
		Host:   addr,
		Header: http.Header{},
	}
	if u := proxy.User; u != nil {
		pass, _ := u.Password()
		connect.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(u.Username()+":"+pass)))
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(90 * time.Second))
	}
	if err := connect.Write(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("proxy %s: %w", paddr, err)
	}
	// the host says nothing before the handshake, so nothing of its is
	// read ahead here
	resp, err := http.ReadResponse(bufio.NewReader(conn), connect)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("proxy %s: %w", paddr, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("proxy %s refused the tunnel to %s: %s", paddr, addr, resp.Status)
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

// chromeFor wraps base so that the requests to the https site at raw are
// sent as Chrome sends them; base itself for a site that is not https (a
// test's) or with JW_JWORG_TLS=go.
func chromeFor(raw string, base http.RoundTripper) http.RoundTripper {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || os.Getenv("JW_JWORG_TLS") == "go" {
		return base
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return &hostTransport{host: u.Host, own: newChromeTransport(), other: base}
}
