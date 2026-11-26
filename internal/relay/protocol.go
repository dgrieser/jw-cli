// Package relay lets a jw serve hand what it cannot fetch itself to proxies:
// copies of jw that run elsewhere (jw proxy), on a network jw.org lets
// through, and that registered with it.
//
// A proxy never takes connections. It asks its server for work over HTTPS —
// a long poll that the server answers with a request to make, or with
// nothing after a while — makes the request, and sends the answer back. The
// server asks its proxies only for what it could not get itself (a bot
// protection's challenge, a refusal, a network that fails), and only for
// jw.org's own sites; a proxy fetches nothing else either way.
//
// The protocol, under /proxy/v1/ on the server:
//
//	POST /register      {"token","name","version"} → 201 {"id","credential","name"}
//	                    the token is a one-time enrollment an admin made
//	                    (jwpe_…); the credential is the proxy's from then on
//	GET  /poll          → 200 a Job, 204 none within PollWait, 403 disabled,
//	                    401 credential refused (deleted)
//	POST /jobs/{id}     the answer's body as is; its status, headers and
//	                    any failure in the X-JW-Result header (a Result)
//	GET  /self          → {"id","name","enabled"}: how the server sees it
//
// Every call but /register carries "Authorization: JW-Proxy <credential>"
// — never an API key, which opens nothing here, as a credential opens
// nothing else.
package relay

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// PathPrefix is where the protocol lives on a server.
	PathPrefix = "/proxy/v1/"
	// AuthScheme names a proxy's credential in the Authorization header.
	AuthScheme = "JW-Proxy"
	// ResultHeader carries a Result with the answer's body.
	ResultHeader = "X-JW-Result"
	// VersionHeader names the version of jw a proxy runs.
	VersionHeader = "X-JW-Proxy-Version"
	// ServerVersionHeader names the version of jw the server runs.
	ServerVersionHeader = "X-JW-Server-Version"
	// ProxyHeader names, on an answer a proxy fetched, the proxy.
	ProxyHeader = "X-JW-Proxy"

	// PollWait is how long the server holds a poll with no work.
	PollWait = 25 * time.Second
	// MaxBody bounds an answer a proxy fetches and sends back: the largest
	// pictures and pages are well below it.
	MaxBody = 32 << 20
	// EnrollTTL is how long an admin's enrollment token can be used.
	EnrollTTL = time.Hour

	credentialPrefix = "jwp_"
	enrollPrefix     = "jwpe_"
)

// Job is a request a server asks a proxy to make.
type Job struct {
	ID     string            `json:"id"`
	Method string            `json:"method"`
	URL    string            `json:"url"`
	Header map[string]string `json:"header,omitempty"`
}

// Result is what came of a Job: the answer's status and the headers worth
// passing on, or why there is none (Error, Status 0).
type Result struct {
	Status int               `json:"status,omitempty"`
	Header map[string]string `json:"header,omitempty"`
	Error  string            `json:"error,omitempty"`
	// Millis is how long the request took the proxy.
	Millis int64 `json:"ms,omitempty"`
}

// Registration is what a proxy sends to register, Registered what it gets.
type Registration struct {
	Token   string `json:"token"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type Registered struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Credential string `json:"credential"`
}

// Self is how the server sees a proxy.
type Self struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// requestHeaders are the headers of a request a job passes on: what selects
// the answer (its language, its type, a revalidation, a range). Cookies and
// credentials stay where they are.
var requestHeaders = []string{"Accept", "Accept-Language", "If-None-Match", "If-Modified-Since", "Range", "Referer", "X-Requested-With"}

// answerHeaders are the headers of an answer a proxy passes back.
var answerHeaders = []string{"Content-Type", "Content-Language", "ETag", "Last-Modified", "Cache-Control", "Expires", "Content-Range", "Accept-Ranges"}

// pick copies the headers named in names from h.
func pick(h http.Header, names []string) map[string]string {
	out := map[string]string{}
	for _, n := range names {
		if v := h.Get(n); v != "" {
			out[n] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// RequestHeader is what of hdr a job carries.
func RequestHeader(hdr http.Header) map[string]string { return pick(hdr, requestHeaders) }

// AnswerHeader is what of an answer's headers a result carries.
func AnswerHeader(hdr http.Header) map[string]string { return pick(hdr, answerHeaders) }

// Header turns a job's or a result's headers back into an http.Header,
// keeping only those the protocol passes on.
func Header(m map[string]string, allowed bool) http.Header {
	names := answerHeaders
	if allowed {
		names = requestHeaders
	}
	h := http.Header{}
	for _, n := range names {
		if v, ok := m[n]; ok && v != "" && !strings.ContainsAny(v, "\r\n") {
			h.Set(n, v)
		}
	}
	return h
}

// relayDomains are the sites a proxy fetches from: jw.org's and its CDN's.
var relayDomains = []string{"jw.org", "jw-cdn.org"}

// Allowed reports whether a request may go through a proxy: a GET or HEAD
// over https to one of jw.org's sites, with no credentials in the address.
// Both the server, before it asks, and the proxy, before it fetches, hold
// to it.
func Allowed(method, raw string) bool {
	if method != http.MethodGet && method != http.MethodHead {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" {
		return false
	}
	if p := u.Port(); p != "" && p != "443" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, d := range relayDomains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// ErrInsecureServer is a server address a proxy will not send its
// credential to: anything but https, but for the machine itself.
var ErrInsecureServer = errors.New("the server must be reached over https (http only on localhost)")

// CheckServerURL is a server's address as a proxy keeps it, refused when it
// is not one to send a credential to.
func CheckServerURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errors.New("not a server address: want https://host[:port]")
	}
	if u.Scheme == "http" && !loopback(u.Hostname()) {
		return "", ErrInsecureServer
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("a server address has no user, query or fragment")
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/"), nil
}

func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// secret is n random bytes, URL-safe.
func secret(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("relay: no randomness: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// hash is what is kept of a secret: its SHA-256, never the secret itself.
func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// sameHash compares two hashes in constant time.
func sameHash(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// newCredential is a proxy's id and its credential, jwp_<id>_<secret>.
func newCredential() (id, credential, secretHash string) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic("relay: no randomness: " + err.Error())
	}
	id = hex.EncodeToString(b)
	s := secret(32)
	return id, credentialPrefix + id + "_" + s, hash(s)
}

// splitCredential is a credential's id and secret.
func splitCredential(c string) (id, s string, ok bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(c), credentialPrefix)
	if !ok {
		return "", "", false
	}
	id, s, ok = strings.Cut(rest, "_")
	return id, s, ok && id != "" && len(s) >= 32
}

// credentialOf is the credential a request carries.
func credentialOf(r *http.Request) string {
	scheme, v, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	if !ok || !strings.EqualFold(scheme, AuthScheme) {
		return ""
	}
	return strings.TrimSpace(v)
}

// newEnrollToken is a one-time enrollment token and what is kept of it.
func newEnrollToken() (token, tokenHash string) {
	t := enrollPrefix + secret(24)
	return t, hash(t)
}
