package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dgrieser/jw-cli/internal/accounts"
	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
)

// Who may do what, once the server has accounts (Config.Accounts):
//
//	anonymous  nothing but the sign-in, registration and password pages —
//	           or read the pages, when the settings allow reading without
//	           an account; never the JSON API
//	user       read everything, and make API keys
//	admin      also the server's settings and the accounts
//
// Browsers sign in at /login and carry a session cookie, which the pages'
// own scripts use for the JSON API too. Other clients (jw --server, scripts)
// send one of the account's API keys on every request: Authorization: Bearer
// <key>, or X-API-Key: <key>. A key reads like its account, but does not
// open the account's pages (/account, /admin): it cannot make more keys,
// change the password or manage the server.
//
// While there is no admin, every page leads to /setup, which only the setup
// link the server printed at startup opens: there the settings are chosen
// and the first admin is made.

const (
	sessionCookie = "jw_session"
	setupCookie   = "jw_setup"
)

type userKey struct{}

// keyAuth marks a request signed in with an API key rather than a session.
type keyAuth struct{}

// byKey reports whether a request came with an API key.
func byKey(r *http.Request) bool { return r.Context().Value(keyAuth{}) != nil }

// apiKeyOf is the API key a request carries, "" for none.
func apiKeyOf(r *http.Request) string {
	if k := strings.TrimSpace(r.Header.Get("X-API-Key")); k != "" {
		return k
	}
	scheme, v, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	if ok && strings.EqualFold(scheme, "Bearer") {
		return strings.TrimSpace(v)
	}
	return ""
}

// unauthorized answers a client of the API that sent no usable key.
func unauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="jw"`)
	writeError(w, http.StatusUnauthorized, "unauthorized", msg)
}

// accountPath is a page about the account or the server: not for API keys.
func accountPath(p string) bool {
	return p == "/account" || strings.HasPrefix(p, "/account/") || strings.HasPrefix(p, "/admin/") || p == "/setup"
}

// userOf is the account signed in for a request, nil when nobody is.
func userOf(r *http.Request) *accounts.User {
	u, _ := r.Context().Value(userKey{}).(*accounts.User)
	return u
}

func (s *Server) authOn() bool { return s.accounts != nil }

// settingsNow are the server's settings, the defaults without accounts.
func (s *Server) settingsNow() accounts.Settings {
	if s.settings == nil {
		return accounts.Defaults()
	}
	return s.settings.Get()
}

// publicPath is reachable without an account.
func publicPath(p string) bool {
	switch p {
	case "/login", "/logout", "/register", "/forgot", "/account/password", "/setup", "/sw.js":
		return true
	}
	return strings.HasPrefix(p, "/static/")
}

// gate resolves who is asking and lets them in, or sends them to sign in.
func (s *Server) gate(next http.Handler) http.Handler {
	if !s.authOn() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var u *accounts.User
		if k := apiKeyOf(r); k != "" {
			ku, _, err := s.accounts.CheckKey(k, s.clientIP(r))
			if err != nil {
				var le *accounts.LimitError
				if errors.As(err, &le) {
					w.Header().Set("Retry-After", strconv.Itoa(int(le.Wait.Seconds())+1))
					writeError(w, http.StatusTooManyRequests, "too_many_requests", "too many failed API keys; try again later")
					return
				}
				unauthorized(w, "API key refused: "+err.Error())
				return
			}
			if accountPath(r.URL.Path) {
				writeError(w, http.StatusForbidden, "forbidden", "an API key does not open the account pages: sign in with the password")
				return
			}
			u = ku
			r = r.WithContext(context.WithValue(r.Context(), keyAuth{}, true))
		} else if _, _, ok := r.BasicAuth(); ok {
			unauthorized(w, "basic authentication is not accepted: send an API key (Authorization: Bearer <key>), made on the account's API keys page")
			return
		} else if c, err := r.Cookie(sessionCookie); err == nil {
			u = s.accounts.Session(c.Value)
			if u != nil && r.Method == http.MethodGet && !apiPath(r.URL.Path) && !strings.HasPrefix(r.URL.Path, "/static/") {
				// the session lasts SessionTTL past its last use: so does
				// the cookie
				s.setSession(w, r, c.Value)
			}
		}
		if u != nil {
			r = r.WithContext(context.WithValue(r.Context(), userKey{}, u))
		}
		// state changes only from this site's own pages
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !s.sameOrigin(r) {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
		p := r.URL.Path
		if !s.accounts.HasAdmin() {
			if p == "/setup" || strings.HasPrefix(p, "/static/") || p == "/sw.js" {
				next.ServeHTTP(w, r)
				return
			}
			if apiPath(p) || r.Method != http.MethodGet {
				writeError(w, http.StatusServiceUnavailable, "setup_required", "the server is not set up yet: open the setup link it printed at startup")
				return
			}
			http.Redirect(w, r, "/setup", http.StatusFound)
			return
		}
		// the JSON API only with an account, whatever the pages allow
		if u != nil || publicPath(p) || (s.settingsNow().Anonymous && !strings.HasPrefix(p, "/api/")) {
			next.ServeHTTP(w, r)
			return
		}
		if apiPath(p) || r.Method != http.MethodGet {
			unauthorized(w, "an API key is needed: send it as Authorization: Bearer <key>; make one on the account's API keys page (/account/keys)")
			return
		}
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
	})
}

// apiPath is a request of a script or a client rather than a page: it is
// answered with a status, not sent to the sign-in page.
func apiPath(p string) bool {
	return strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/unfold/") || strings.HasPrefix(p, "/ui/")
}

// sameOrigin reports whether a request comes from this site's own pages (or
// from no browser at all).
func (s *Server) sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	if o == "null" {
		return r.Header.Get("Sec-Fetch-Site") == "same-origin"
	}
	if strings.EqualFold(o, s.scheme(r)+"://"+r.Host) {
		return true
	}
	if pu := s.settingsNow().PublicURL; pu != "" {
		if u, err := url.Parse(pu); err == nil && strings.EqualFold(o, u.Scheme+"://"+u.Host) {
			return true
		}
	}
	return false
}

// scheme is the scheme the client used: https on TLS, or as a trusted reverse
// proxy reports it in X-Forwarded-Proto.
func (s *Server) scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if s.trusted(host) {
		if p := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])); p == "https" || p == "http" {
			return p
		}
	}
	return "http"
}

// secure reports whether cookies should be HTTPS only.
func (s *Server) secure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") ||
		strings.HasPrefix(s.settingsNow().PublicURL, "https://")
}

// clientIP names the client for the rate limits: the address of the
// connection, or behind a trusted reverse proxy the last address in
// X-Forwarded-For that is not a trusted proxy itself.
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !s.trusted(host) {
		return host
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for _, hop := range slices.Backward(hops) {
		hop = strings.TrimSpace(hop)
		if net.ParseIP(hop) == nil {
			break
		}
		if !s.trusted(hop) {
			return hop
		}
	}
	return host
}

func (s *Server) trusted(addr string) bool {
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}
	if s.proxies == nil {
		return ip.IsLoopback()
	}
	for _, n := range s.proxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ParseProxies reads a comma-separated list of addresses and CIDR ranges of
// trusted reverse proxies; "none" trusts none.
func ParseProxies(list string) ([]*net.IPNet, error) {
	list = strings.TrimSpace(list)
	if list == "" {
		return nil, nil
	}
	out := []*net.IPNet{}
	if list == "none" {
		return out, nil
	}
	for p := range strings.SplitSeq(list, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.Contains(p, "/") {
			ip := net.ParseIP(p)
			if ip == nil {
				return nil, fmt.Errorf("invalid proxy address %q", p)
			}
			bits := 128
			if ip.To4() != nil {
				ip, bits = ip.To4(), 32
			}
			out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		_, n, err := net.ParseCIDR(p)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy range %q", p)
		}
		out = append(out, n)
	}
	return out, nil
}

func (s *Server) setSession(w http.ResponseWriter, r *http.Request, tok string) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: tok, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: int(accounts.SessionTTL.Seconds()), Secure: s.secure(r)})
}

// SetupToken is the secret of the setup link, while the server has no admin
// ("" once it has, or without accounts).
func (s *Server) SetupToken() string {
	if !s.authOn() || s.accounts.HasAdmin() {
		return ""
	}
	return s.setupToken
}

func (s *Server) accountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.loginPost)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /register", s.registerPage)
	mux.HandleFunc("POST /register", s.registerPost)
	mux.HandleFunc("GET /forgot", s.forgotPage)
	mux.HandleFunc("POST /forgot", s.forgotPost)
	mux.HandleFunc("GET /account/password", s.passwordPage)
	mux.HandleFunc("POST /account/password", s.passwordPost)
	mux.HandleFunc("GET /account", s.requireUser(s.accountPage))
	mux.HandleFunc("POST /account", s.requireUser(s.accountPost))
	mux.HandleFunc("GET /account/keys", s.requireUser(s.keysPage))
	mux.HandleFunc("POST /account/keys", s.requireUser(s.keysPost))
	mux.HandleFunc("GET /admin/users", s.requireAdmin(s.usersPage))
	mux.HandleFunc("POST /admin/users", s.requireAdmin(s.usersPost))
	mux.HandleFunc("GET /admin/settings", s.requireAdmin(s.settingsPage))
	mux.HandleFunc("POST /admin/settings", s.requireAdmin(s.settingsPost))
	mux.HandleFunc("GET /setup", s.setupPage)
	mux.HandleFunc("POST /setup", s.setupPost)
}

func (s *Server) requireUser(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if userOf(r) == nil {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
			return
		}
		if byKey(r) {
			// the gate refuses these already; an account page never takes a key
			writeError(w, http.StatusForbidden, "forbidden", "an API key does not open the account pages: sign in with the password")
			return
		}
		h(w, r)
	}
}

func (s *Server) requireAdmin(h http.HandlerFunc) http.HandlerFunc {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request) {
		if !userOf(r).IsAdmin() {
			p := s.authBase(r, func(a *i18n.AuthText) string { return "HTTP 403" })
			w.Header().Set("Cache-Control", "no-store")
			s.render(w, http.StatusForbidden, "error", errorPage{basePage: p.basePage, Status: http.StatusForbidden,
				Message: http.StatusText(http.StatusForbidden)})
			return
		}
		h(w, r)
	})
}

// --- the pages --------------------------------------------------------------

// authPage is what the account pages carry.
type authPage struct {
	basePage
	Notice      string
	Next        string
	Token       string
	Kind        string
	Email       string
	Sent        bool
	CanRegister bool
	CanReset    bool
	Approval    bool
	MinPassword int
	// the settings forms
	Settings        accounts.Settings
	SMTPPasswordSet bool
	// SMTPPassword is a password typed and tested but not saved yet: the
	// form keeps it for the save that follows
	SMTPPassword string
	TestTo       string
	SetupAllowed bool
	// the users page
	Users     []accounts.Info
	MailReady bool
	// the keys page: the keys, and a secret made just now, shown once
	Keys       []accounts.KeyInfo
	NewKey     string
	NewKeyName string
	ServerURL  string
	MaxKeys    int
	MaxKeyName int
}

// authLang is the language of the account pages and of the mails a request
// sends: the reader's chosen content language where it has a catalog, else
// what the browser asks for.
func (s *Server) authLang(r *http.Request) i18n.Lang {
	spec := r.URL.Query().Get("lang")
	if spec == "" {
		if c, err := r.Cookie(langCookie); err == nil {
			spec = c.Value
		}
	}
	if spec != "" {
		if v, ok := s.langMemo.Load(strings.ToLower(spec)); ok {
			return i18n.Match(v.(model.Language).Locale)
		}
	}
	al := r.Header.Get("Accept-Language")
	if i := strings.IndexAny(al, ",;"); i >= 0 {
		al = al[:i]
	}
	return i18n.Match(al)
}

// authBase is the frame of an account page: it does not ask jw.org for the
// language list, so it shows while the site cannot be reached.
func (s *Server) authBase(r *http.Request, title func(a *i18n.AuthText) string) authPage {
	l := s.authLang(r)
	// the navigation only for who may follow it
	p := basePage{Path: r.URL.Path, Locale: l.String(), T: l.Text(), A: l.Auth(), Bare: userOf(r) == nil}
	p.Title = title(p.A)
	s.withAccount(r, &p)
	return authPage{basePage: p, MinPassword: accounts.MinPassword}
}

// withAccount fills in who is signed in, for the bar's settings.
func (s *Server) withAccount(r *http.Request, p *basePage) {
	if !s.authOn() {
		return
	}
	p.Auth = true
	if u := userOf(r); u != nil {
		p.User = u.Email
		p.Admin = u.IsAdmin()
		if p.Admin {
			p.PendingUsers = s.accounts.Pending()
		}
	}
}

func (s *Server) renderAuth(w http.ResponseWriter, status int, page string, p authPage) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set(keepHeader, "no")
	w.Header().Set("Referrer-Policy", "no-referrer") // the links carry secrets
	s.render(w, status, page, p)
}

// errText words an error of the accounts for the reader.
func errText(a *i18n.AuthText, err error) string {
	var le *accounts.LimitError
	switch {
	case errors.As(err, &le):
		return fmt.Sprintf(a.ErrTooMany, int(le.Wait.Minutes())+1)
	case errors.Is(err, accounts.ErrBadLogin):
		return a.ErrBadLogin
	case errors.Is(err, accounts.ErrPending):
		return a.ErrPending
	case errors.Is(err, accounts.ErrDisabled):
		return a.ErrDisabled
	case errors.Is(err, accounts.ErrExists):
		return a.ErrExists
	case errors.Is(err, accounts.ErrNotFound):
		return a.ErrNotFound
	case errors.Is(err, accounts.ErrLastAdmin):
		return a.ErrLastAdmin
	case errors.Is(err, accounts.ErrBadEmail):
		return a.ErrBadEmail
	case errors.Is(err, accounts.ErrBadPass):
		return fmt.Sprintf(a.ErrBadPass, accounts.MinPassword)
	case errors.Is(err, accounts.ErrNoPass):
		return a.ErrNoPass
	case errors.Is(err, accounts.ErrBadToken):
		return a.ErrBadToken
	case errors.Is(err, accounts.ErrTooMany):
		return fmt.Sprintf(a.ErrTooMany, 15)
	case errors.Is(err, accounts.ErrBadPublicURL):
		return a.ErrBadPublicURL
	case errors.Is(err, accounts.ErrRegisterNoMail):
		return a.ErrRegisterNoMail
	case errors.Is(err, accounts.ErrBadSMTP):
		return a.ErrBadSMTP
	case errors.Is(err, accounts.ErrKeyName):
		return fmt.Sprintf(a.ErrKeyName, accounts.MaxKeyName)
	case errors.Is(err, accounts.ErrManyKeys):
		return fmt.Sprintf(a.ErrManyKeys, accounts.MaxKeys)
	case errors.Is(err, accounts.ErrKeyTaken):
		return a.ErrKeyTaken
	case errors.Is(err, accounts.ErrKeyExpiry):
		return a.ErrKeyExpiry
	case errors.Is(err, accounts.ErrKeyNotFound):
		return a.ErrKeyNotFound
	case errors.Is(err, errMail):
		return fmt.Sprintf(a.ErrMail, strings.TrimPrefix(err.Error(), errMail.Error()+": "))
	}
	return err.Error()
}

// publicErrText is errText for somebody not signed in: a mail that failed
// is said to, without the mail server's own words.
func publicErrText(a *i18n.AuthText, err error) string {
	if errors.Is(err, errMail) {
		return fmt.Sprintf(a.ErrMail, "-")
	}
	return errText(a, err)
}

// safeNext keeps redirects on this site.
func safeNext(n string) string {
	if !strings.HasPrefix(n, "/") || strings.HasPrefix(n, "//") || strings.HasPrefix(n, "/\\") ||
		strings.HasPrefix(n, "/login") || strings.HasPrefix(n, "/logout") {
		return "/"
	}
	return n
}

func (s *Server) loginForm(r *http.Request) authPage {
	p := s.authBase(r, func(a *i18n.AuthText) string { return a.SignIn })
	st := s.settingsNow()
	p.CanRegister, p.CanReset = st.CanRegister(), st.MailReady()
	p.Next = safeNext(r.FormValue("next"))
	return p
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if userOf(r) != nil {
		http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusFound)
		return
	}
	s.renderAuth(w, http.StatusOK, "login", s.loginForm(r))
}

func (s *Server) loginPost(w http.ResponseWriter, r *http.Request) {
	p := s.loginForm(r)
	p.Email = strings.TrimSpace(r.PostFormValue("email"))
	tok, _, err := s.accounts.Login(p.Email, r.PostFormValue("password"), s.clientIP(r))
	if err != nil {
		p.Error = errText(p.A, err)
		s.renderAuth(w, http.StatusUnauthorized, "login", p)
		return
	}
	s.setSession(w, r, tok)
	http.Redirect(w, r, p.Next, http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.accounts.Logout(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	// the pages the browser kept were read with the login
	w.Header().Set("Clear-Site-Data", `"cache"`)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// --- mails --------------------------------------------------------------------

var errMail = errors.New("mail")

// siteName names the server in mails: its public address.
func siteName(st accounts.Settings) string {
	if u, err := url.Parse(st.PublicURL); err == nil && u.Host != "" {
		return u.Host
	}
	return "jw serve"
}

// link is an address on the public URL; mails never take the host a request
// named, which anybody can set.
func link(st accounts.Settings, path string, q url.Values) string {
	u := st.PublicURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// mail sends one message through the mail server of st.
func (s *Server) mail(ctx context.Context, st accounts.Settings, to, subject, body string) error {
	if !st.SMTP.Configured() {
		return fmt.Errorf("%w: %s", errMail, "no mail server")
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
	defer cancel()
	if err := s.mailer(st.SMTP).Send(ctx, accounts.Mail{To: to, Subject: subject, Body: body}); err != nil {
		s.logf("mail to %s: %v", to, err)
		return fmt.Errorf("%w: %v", errMail, err)
	}
	return nil
}

// sendLater sends the mails of a public form (registration, forgotten
// password) after the answer: the answer is then the same, as quick, and
// tells nothing, whether the address has an account and whether its mail
// went out. Failures are logged.
func (s *Server) sendLater(r *http.Request, send func(ctx context.Context) error) {
	ctx := context.WithoutCancel(r.Context())
	s.mailing.Go(func() {
		if err := send(ctx); err != nil {
			s.logf("mail: %v", err)
		}
	})
}

// mailLink sends a link of kind to email, in lang.
func (s *Server) mailLink(ctx context.Context, kind, email string, lang i18n.Lang, ttl time.Duration,
	subject, body string) error {
	st := s.settingsNow()
	tok, err := s.accounts.NewToken(kind, email, lang.String(), ttl)
	if err != nil {
		return err
	}
	l := link(st, "/account/password", url.Values{"token": {tok}})
	if err := s.mail(ctx, st, email, subject, fmt.Sprintf(body, siteName(st), l)); err != nil {
		// the link the address got before keeps working
		s.accounts.Revoke(tok)
		return err
	}
	s.accounts.Supersede(tok)
	return nil
}

func (s *Server) sendReset(ctx context.Context, u accounts.User, lang i18n.Lang) error {
	if u.Lang != "" {
		lang = i18n.Match(u.Lang)
	}
	a := lang.Auth()
	if u.Hash == "" {
		return s.mailLink(ctx, accounts.TokenInvite, u.Email, lang, 7*24*time.Hour, a.MailInviteSubject, a.MailInviteBody)
	}
	return s.mailLink(ctx, accounts.TokenReset, u.Email, lang, time.Hour, a.MailResetSubject, a.MailResetBody)
}

// throttle limits the mails one client, and one address, can have sent.
func (s *Server) throttle(r *http.Request, what, email string) error {
	if err := s.accounts.Throttle(what, s.clientIP(r)); err != nil {
		return err
	}
	return s.accounts.Throttle(what, "to:"+strings.ToLower(email))
}

// --- registration --------------------------------------------------------------

func (s *Server) registerForm(r *http.Request) authPage {
	p := s.authBase(r, func(a *i18n.AuthText) string { return a.Register })
	st := s.settingsNow()
	p.CanRegister, p.Approval = st.CanRegister(), st.Registration == accounts.RegisterApproval
	return p
}

func (s *Server) registerPage(w http.ResponseWriter, r *http.Request) {
	p := s.registerForm(r)
	if !p.CanRegister {
		p.Error = p.A.ErrRegisterClosed
	}
	s.renderAuth(w, http.StatusOK, "register", p)
}

func (s *Server) registerPost(w http.ResponseWriter, r *http.Request) {
	p := s.registerForm(r)
	if !p.CanRegister {
		p.Error = p.A.ErrRegisterClosed
		s.renderAuth(w, http.StatusForbidden, "register", p)
		return
	}
	p.Email = strings.TrimSpace(r.PostFormValue("email"))
	email, err := accounts.NormalizeEmail(p.Email)
	if err == nil {
		err = s.throttle(r, "register", email)
	}
	if err == nil {
		lang := s.authLang(r)
		s.sendLater(r, func(ctx context.Context) error {
			a := lang.Auth()
			if u, ok := s.accounts.Get(email); ok {
				// no telling the page who has an account: the owner hears of it
				if u.Disabled {
					return nil
				}
				if u.Lang != "" {
					lang = i18n.Match(u.Lang)
					a = lang.Auth()
				}
				kind, ttl := accounts.TokenReset, time.Hour
				if u.Hash == "" {
					kind, ttl = accounts.TokenInvite, 7*24*time.Hour
				}
				return s.mailLink(ctx, kind, email, lang, ttl, a.MailExistsSubject, a.MailExistsBody)
			}
			return s.mailLink(ctx, accounts.TokenRegister, email, lang, 24*time.Hour, a.MailRegisterSubject, a.MailRegisterBody)
		})
	}
	if err != nil {
		p.Error = publicErrText(p.A, err)
		s.renderAuth(w, http.StatusOK, "register", p)
		return
	}
	p.Sent = true
	p.Notice = p.A.RegisterSent
	s.renderAuth(w, http.StatusOK, "register", p)
}

// --- forgotten passwords and the links of mails ----------------------------------

func (s *Server) forgotPage(w http.ResponseWriter, r *http.Request) {
	p := s.authBase(r, func(a *i18n.AuthText) string { return a.ResetTitle })
	p.CanReset = s.settingsNow().MailReady()
	if !p.CanReset {
		p.Error = p.A.ErrNoMail
	}
	s.renderAuth(w, http.StatusOK, "forgot", p)
}

func (s *Server) forgotPost(w http.ResponseWriter, r *http.Request) {
	p := s.authBase(r, func(a *i18n.AuthText) string { return a.ResetTitle })
	p.CanReset = s.settingsNow().MailReady()
	p.Email = strings.TrimSpace(r.PostFormValue("email"))
	email, err := accounts.NormalizeEmail(p.Email)
	if err == nil && !p.CanReset {
		err = errors.New(p.A.ErrNoMail)
	}
	if err == nil {
		err = s.throttle(r, "reset", email)
	}
	if err == nil {
		lang := s.authLang(r)
		s.sendLater(r, func(ctx context.Context) error {
			if u, ok := s.accounts.Get(email); ok && !u.Disabled {
				return s.sendReset(ctx, u, lang)
			}
			return nil
		})
	}
	if err != nil {
		p.Error = publicErrText(p.A, err)
		s.renderAuth(w, http.StatusOK, "forgot", p)
		return
	}
	p.Sent = true
	p.Notice = p.A.ForgotSent
	s.renderAuth(w, http.StatusOK, "forgot", p)
}

func (s *Server) passwordForm(r *http.Request, secret string) (authPage, error) {
	p := s.authBase(r, func(a *i18n.AuthText) string { return a.ChangePassword })
	p.Token = secret
	t, err := s.accounts.PeekToken(secret, accounts.TokenRegister, accounts.TokenReset, accounts.TokenInvite)
	if err != nil {
		return p, err
	}
	p.Kind, p.Email = t.Kind, t.Email
	switch t.Kind {
	case accounts.TokenRegister:
		p.Title = p.A.Register
		p.Notice = fmt.Sprintf(p.A.SetPasswordRegister, t.Email)
	case accounts.TokenInvite:
		p.Notice = fmt.Sprintf(p.A.SetPasswordInvite, t.Email)
	default:
		p.Title = p.A.ResetTitle
		p.Notice = fmt.Sprintf(p.A.SetPasswordReset, t.Email)
	}
	return p, nil
}

func (s *Server) passwordPage(w http.ResponseWriter, r *http.Request) {
	p, err := s.passwordForm(r, r.URL.Query().Get("token"))
	if err != nil {
		p.Error = errText(p.A, err)
		s.renderAuth(w, http.StatusNotFound, "password", p)
		return
	}
	s.renderAuth(w, http.StatusOK, "password", p)
}

func (s *Server) passwordPost(w http.ResponseWriter, r *http.Request) {
	secret := r.PostFormValue("token")
	p, err := s.passwordForm(r, secret)
	if err != nil {
		p.Error = errText(p.A, err)
		s.renderAuth(w, http.StatusNotFound, "password", p)
		return
	}
	pw := r.PostFormValue("password")
	if pw != r.PostFormValue("password2") {
		p.Error = p.A.PasswordsDiffer
		s.renderAuth(w, http.StatusOK, "password", p)
		return
	}
	if err := accounts.CheckPassword(pw); err != nil {
		p.Error = errText(p.A, err)
		s.renderAuth(w, http.StatusOK, "password", p)
		return
	}
	var u accounts.User
	if p.Kind == accounts.TokenRegister {
		st := s.settingsNow()
		if st.Registration == accounts.RegisterClosed {
			p.Error = p.A.ErrRegisterClosed
			s.renderAuth(w, http.StatusForbidden, "password", p)
			return
		}
		u, err = s.accounts.UseRegistration(secret, pw, st.Registration == accounts.RegisterApproval)
		if err == nil && u.Pending {
			s.notifyAdmins(r.Context(), u.Email)
		}
	} else {
		u, err = s.accounts.UsePasswordLink(secret, pw)
	}
	if err != nil {
		p.Error = errText(p.A, err)
		s.renderAuth(w, http.StatusOK, "password", p)
		return
	}
	switch {
	case u.Pending:
		p.Kind, p.Notice = "", p.A.PendingNotice
	case u.Disabled:
		p.Kind, p.Notice = "", p.A.ErrDisabled
	default:
		if tok, err := s.accounts.StartSession(u.Email); err == nil {
			s.setSession(w, r, tok)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	p.Token = ""
	s.renderAuth(w, http.StatusOK, "password", p)
}

// notifyAdmins tells the admins of a registration waiting for them.
func (s *Server) notifyAdmins(ctx context.Context, email string) {
	st := s.settingsNow()
	if !st.MailReady() {
		return
	}
	l := link(st, "/admin/users", nil)
	for _, admin := range s.accounts.Admins() {
		a := i18n.Match(admin.Lang).Auth()
		_ = s.mail(ctx, st, admin.Email, a.MailPendingSubject, fmt.Sprintf(a.MailPendingBody, siteName(st), l, email))
	}
}

// --- one's own account ------------------------------------------------------------

func (s *Server) accountForm(r *http.Request) authPage {
	p := s.authBase(r, func(a *i18n.AuthText) string { return a.Account })
	p.Email = userOf(r).Email
	return p
}

func (s *Server) accountPage(w http.ResponseWriter, r *http.Request) {
	s.renderAuth(w, http.StatusOK, "account", s.accountForm(r))
}

func (s *Server) accountPost(w http.ResponseWriter, r *http.Request) {
	p := s.accountForm(r)
	pw := r.PostFormValue("password")
	var err error
	if pw != r.PostFormValue("password2") {
		err = errors.New(p.A.PasswordsDiffer)
	} else {
		keep := ""
		if c, cerr := r.Cookie(sessionCookie); cerr == nil {
			keep = c.Value
		}
		err = s.accounts.ChangePassword(p.Email, r.PostFormValue("old"), pw, keep)
	}
	if err != nil {
		p.Error = errText(p.A, err)
		s.renderAuth(w, http.StatusOK, "account", p)
		return
	}
	p.Notice = p.A.PasswordChanged
	s.renderAuth(w, http.StatusOK, "account", p)
}

// --- the accounts, for admins -----------------------------------------------------

func (s *Server) usersForm(r *http.Request) authPage {
	p := s.authBase(r, func(a *i18n.AuthText) string { return a.Users })
	p.Users = s.accounts.List()
	p.Email = userOf(r).Email
	p.MailReady = s.settingsNow().MailReady()
	return p
}

// userMessages are the confirmations the users page shows after an action,
// by the code the redirect carries.
func userMessage(a *i18n.AuthText, code, email string) string {
	f := map[string]string{
		"created": a.MsgCreated, "invited": a.MsgInvited, "approved": a.MsgApproved, "disabled": a.MsgDisabled,
		"enabled": a.MsgEnabled, "role": a.MsgRole, "reset": a.MsgResetSent, "deleted": a.MsgDeleted,
		"keys": a.MsgKeysRevoked,
	}[code]
	if f == "" {
		return ""
	}
	return fmt.Sprintf(f, email)
}

func (s *Server) usersPage(w http.ResponseWriter, r *http.Request) {
	p := s.usersForm(r)
	q := r.URL.Query()
	p.Notice = userMessage(p.A, q.Get("done"), q.Get("email"))
	s.renderAuth(w, http.StatusOK, "users", p)
}

func (s *Server) usersPost(w http.ResponseWriter, r *http.Request) {
	p := s.usersForm(r)
	me := userOf(r)
	email := strings.TrimSpace(r.PostFormValue("email"))
	op := r.PostFormValue("op")
	done, err := s.userAction(r, me, op, email)
	if err != nil {
		p.Error = errText(p.A, err)
		p.Users = s.accounts.List()
		s.renderAuth(w, http.StatusOK, "users", p)
		return
	}
	if norm, err := accounts.NormalizeEmail(email); err == nil {
		email = norm
	}
	http.Redirect(w, r, "/admin/users?"+url.Values{"done": {done}, "email": {email}}.Encode(), http.StatusSeeOther)
}

// userAction carries out one action of the users page and names it for the
// confirmation.
func (s *Server) userAction(r *http.Request, me *accounts.User, op, email string) (string, error) {
	a := s.authLang(r).Auth()
	st := s.settingsNow()
	if op == "create" {
		role := r.PostFormValue("role")
		pw := r.PostFormValue("password")
		if pw == "" && !st.MailReady() {
			return "", errors.New(a.CreateUserNoMail)
		}
		lang := s.authLang(r)
		if err := s.accounts.Create(email, pw, role, false, lang.String()); err != nil {
			return "", err
		}
		if pw != "" {
			return "created", nil
		}
		u, _ := s.accounts.Get(email)
		if err := s.sendReset(r.Context(), u, lang); err != nil {
			// no invitation went out: drop the account so adding it again works
			_ = s.accounts.Delete(u.Email)
			return "", err
		}
		return "invited", nil
	}
	u, ok := s.accounts.Get(email)
	if !ok {
		return "", accounts.ErrNotFound
	}
	self := strings.EqualFold(u.Email, me.Email)
	t, f := true, false
	switch op {
	case "approve":
		if err := s.accounts.Apply(u.Email, accounts.Update{Approve: true}); err != nil {
			return "", err
		}
		if st.MailReady() {
			ua := i18n.Match(u.Lang).Auth()
			_ = s.mail(r.Context(), st, u.Email, ua.MailApprovedSubject,
				fmt.Sprintf(ua.MailApprovedBody, siteName(st), link(st, "/login", nil)))
		}
		return "approved", nil
	case "disable", "enable":
		if self {
			return "", errors.New(a.ErrSelf)
		}
		v := &f
		if op == "disable" {
			v = &t
		}
		return op + "d", s.accounts.Apply(u.Email, accounts.Update{Disabled: v})
	case "admin", "user":
		if self {
			return "", errors.New(a.ErrSelf)
		}
		return "role", s.accounts.Apply(u.Email, accounts.Update{Role: &op})
	case "reset":
		if !st.MailReady() {
			return "", errors.New(a.ErrNoMail)
		}
		return "reset", s.sendReset(r.Context(), u, s.authLang(r))
	case "delete":
		if self {
			return "", errors.New(a.ErrSelf)
		}
		return "deleted", s.accounts.Delete(u.Email)
	case "keys":
		_, err := s.accounts.DeleteKeys(u.Email)
		return "keys", err
	}
	return "", fmt.Errorf("unknown action %q", op)
}

// --- the server's settings ------------------------------------------------------------

// settingsFrom reads the settings form over cur. An empty mail password
// keeps the stored one, unless the user name is gone too.
func settingsFrom(r *http.Request, cur accounts.Settings) accounts.Settings {
	n := cur
	n.Registration = r.PostFormValue("registration")
	n.Anonymous = r.PostFormValue("anonymous") == "on"
	n.PublicURL = r.PostFormValue("public_url")
	n.SMTP.Host = r.PostFormValue("smtp_host")
	n.SMTP.Port, _ = strconv.Atoi(strings.TrimSpace(r.PostFormValue("smtp_port")))
	n.SMTP.Security = r.PostFormValue("smtp_security")
	n.SMTP.Username = r.PostFormValue("smtp_username")
	if pw := r.PostFormValue("smtp_password"); pw != "" {
		n.SMTP.Password = pw
	}
	if strings.TrimSpace(n.SMTP.Username) == "" {
		n.SMTP.Password = ""
	}
	n.SMTP.From = r.PostFormValue("smtp_from")
	n.Normalize()
	return n
}

func (s *Server) settingsForm(r *http.Request, st accounts.Settings) authPage {
	p := s.authBase(r, func(a *i18n.AuthText) string { return a.ServerSettings })
	p.Settings = st
	p.SMTPPasswordSet = st.SMTP.Password != ""
	p.Settings.SMTP.Password = ""
	if u := userOf(r); u != nil {
		p.TestTo = u.Email
	}
	return p
}

func (s *Server) settingsPage(w http.ResponseWriter, r *http.Request) {
	s.renderAuth(w, http.StatusOK, "settings", s.settingsForm(r, s.settingsNow()))
}

func (s *Server) settingsPost(w http.ResponseWriter, r *http.Request) {
	st := settingsFrom(r, s.settingsNow())
	p := s.settingsForm(r, st)
	if r.PostFormValue("action") == "test" {
		p.TestTo = strings.TrimSpace(r.PostFormValue("test_to"))
		p.Notice, p.Error = s.testMail(r, st, p.TestTo, p.A)
		p.SMTPPassword = r.PostFormValue("smtp_password")
		s.renderAuth(w, http.StatusOK, "settings", p)
		return
	}
	if err := s.settings.Save(st); err != nil {
		p.Error = errText(p.A, err)
		s.renderAuth(w, http.StatusOK, "settings", p)
		return
	}
	p = s.settingsForm(r, s.settingsNow())
	p.Notice = p.A.MsgSaved
	s.renderAuth(w, http.StatusOK, "settings", p)
}

// testMail sends a test mail with the settings of a form, before they are
// saved; it returns the confirmation or the failure.
func (s *Server) testMail(r *http.Request, st accounts.Settings, to string, a *i18n.AuthText) (notice, failure string) {
	email, err := accounts.NormalizeEmail(to)
	if err == nil {
		err = st.Validate()
		if errors.Is(err, accounts.ErrRegisterNoMail) {
			err = nil // that is what the test is for
		}
	}
	if err == nil {
		name := siteName(st)
		if st.PublicURL == "" {
			name = r.Host
		}
		err = s.mail(r.Context(), st, email, a.MailTestSubject, fmt.Sprintf(a.MailTestBody, name))
	}
	if err != nil {
		return "", errText(a, err)
	}
	return fmt.Sprintf(a.MsgTestSent, email), ""
}

// --- setup ------------------------------------------------------------------------------

// setupAllowed reports whether the request carries the setup link's secret,
// while the server has no admin.
func (s *Server) setupAllowed(r *http.Request) bool {
	tok := s.SetupToken()
	if tok == "" {
		return false
	}
	got := r.URL.Query().Get("token")
	if got == "" {
		if c, err := r.Cookie(setupCookie); err == nil {
			got = c.Value
		}
	}
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(tok)) == 1
}

func (s *Server) setupForm(r *http.Request, st accounts.Settings) authPage {
	p := s.settingsForm(r, st)
	p.Title = p.A.SetupTitle
	p.SetupAllowed = s.setupAllowed(r)
	return p
}

func (s *Server) setupPage(w http.ResponseWriter, r *http.Request) {
	if s.accounts.HasAdmin() {
		http.Redirect(w, r, "/admin/settings", http.StatusFound)
		return
	}
	if t := r.URL.Query().Get("token"); t != "" && s.setupAllowed(r) {
		http.SetCookie(w, &http.Cookie{Name: setupCookie, Value: t, Path: "/setup", MaxAge: 86400, HttpOnly: true,
			SameSite: http.SameSiteStrictMode, Secure: s.secure(r)})
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}
	st := s.settingsNow()
	if st.PublicURL == "" {
		// what the admin's browser reached the server at is the best guess
		scheme := "http"
		if s.secure(r) {
			scheme = "https"
		}
		st.PublicURL = scheme + "://" + r.Host
	}
	p := s.setupForm(r, st)
	status := http.StatusOK
	if !p.SetupAllowed {
		status = http.StatusUnauthorized
	}
	s.renderAuth(w, status, "setup", p)
}

func (s *Server) setupPost(w http.ResponseWriter, r *http.Request) {
	// two submits of the setup link: only the first makes the admin
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if s.accounts.HasAdmin() {
		http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
		return
	}
	st := settingsFrom(r, s.settingsNow())
	p := s.setupForm(r, st)
	if !p.SetupAllowed {
		s.renderAuth(w, http.StatusUnauthorized, "setup", p)
		return
	}
	p.Email = strings.TrimSpace(r.PostFormValue("email"))
	if r.PostFormValue("action") == "test" {
		p.TestTo = p.Email
		p.Notice, p.Error = s.testMail(r, st, p.Email, p.A)
		p.SMTPPassword = r.PostFormValue("smtp_password")
		s.renderAuth(w, http.StatusOK, "setup", p)
		return
	}
	pw := r.PostFormValue("password")
	fail := func(err error) {
		p.Error = errText(p.A, err)
		s.renderAuth(w, http.StatusOK, "setup", p)
	}
	if err := st.Validate(); err != nil {
		fail(err)
		return
	}
	email, err := accounts.NormalizeEmail(p.Email)
	if err != nil {
		fail(err)
		return
	}
	// settings first: when they cannot be saved there is still no admin and
	// the setup link keeps working
	if err := s.settings.Save(st); err != nil {
		fail(err)
		return
	}
	if _, exists := s.accounts.Get(email); exists {
		// an account made earlier (jw serve user add) becomes the admin with
		// its password
		if err := s.accounts.ClaimAdmin(email, pw); err != nil {
			fail(err)
			return
		}
	} else {
		if pw != r.PostFormValue("password2") {
			fail(errors.New(p.A.PasswordsDiffer))
			return
		}
		if err := s.accounts.Create(email, pw, accounts.RoleAdmin, false, s.authLang(r).String()); err != nil {
			fail(err)
			return
		}
	}
	if tok, err := s.accounts.StartSession(email); err == nil {
		s.setSession(w, r, tok)
	}
	http.SetCookie(w, &http.Cookie{Name: setupCookie, Value: "", Path: "/setup", MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
