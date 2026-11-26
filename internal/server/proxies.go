package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/relay"
)

// The admin's page of the proxies (jw proxy): who they are and how they are
// doing, which hosts the server asks through them right now, and making
// room for a new one — a one-time token and the command that sets the
// proxy up with it.

type proxiesPage struct {
	authPage
	List        []relay.Status
	Enrollments []relay.Enrollment
	// Relayed says, a line a host, which hosts turned the server away and
	// are asked through the proxies first
	Relayed []string
	// a proxy just made room for: its name, and the command with the token
	NewName, Command string
	ServerURL        string
	DownloadURL      string
}

// StateText is a proxy's state in the reader's words.
func (p proxiesPage) StateText(st relay.Status) string {
	switch st.State() {
	case "disabled":
		return p.A.StatusDisabled
	case "online":
		return p.A.ProxyOnline
	case "never":
		return p.A.ProxyNever
	}
	return p.A.ProxyOffline
}

// When is a time as the page shows it, or "never".
func (p proxiesPage) When(t time.Time) string {
	if t.IsZero() {
		return p.A.Never
	}
	return t.Local().Format("2006-01-02 15:04")
}

func (s *Server) proxiesForm(r *http.Request) proxiesPage {
	p := proxiesPage{authPage: s.authBase(r, func(a *i18n.AuthText) string { return a.Proxies })}
	now := time.Now()
	p.List = s.relays.Proxies(now)
	p.Enrollments = s.relays.Enrollments(now)
	if s.svc.HTTP != nil {
		for _, h := range s.svc.HTTP.RelayedHosts() {
			p.Relayed = append(p.Relayed, fmt.Sprintf(p.A.ProxyRelayed, h.Host, h.Reason, h.Until.Local().Format("15:04")))
		}
	}
	p.ServerURL = s.settingsNow().PublicURL
	if p.ServerURL == "" {
		p.ServerURL = s.scheme(r) + "://" + r.Host
	}
	p.DownloadURL = p.VersionURL()
	if p.DownloadURL == "" {
		p.DownloadURL = "https://github.com/dgrieser/jw-cli/releases/latest"
	}
	switch done, name := r.FormValue("done"), r.FormValue("name"); done {
	case "enabled":
		p.Notice = fmt.Sprintf(p.A.MsgProxyEnabled, name)
	case "disabled":
		p.Notice = fmt.Sprintf(p.A.MsgProxyDisabled, name)
	case "deleted":
		p.Notice = fmt.Sprintf(p.A.MsgProxyDeleted, name)
	case "revoked":
		p.Notice = fmt.Sprintf(p.A.MsgProxyRevoked, name)
	}
	return p
}

func (s *Server) proxiesPage(w http.ResponseWriter, r *http.Request) {
	s.renderProxies(w, s.proxiesForm(r))
}

func (s *Server) renderProxies(w http.ResponseWriter, p proxiesPage) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set(keepHeader, "no")
	w.Header().Set("Referrer-Policy", "no-referrer") // the page may show a token
	s.render(w, http.StatusOK, "proxies", p)
}

func (s *Server) proxiesPost(w http.ResponseWriter, r *http.Request) {
	op, id, name := r.PostFormValue("op"), r.PostFormValue("id"), r.PostFormValue("name")
	if op == "enroll" {
		token, e, err := s.relays.Enroll(name, userOf(r).Email, time.Now())
		p := s.proxiesForm(r)
		if err != nil {
			p.Error = err.Error()
			if errors.Is(err, relay.ErrBadName) {
				p.Error = p.A.ProxyName + ": " + p.A.ProxyNameHint
			}
			s.renderProxies(w, p)
			return
		}
		p.Enrollments = s.relays.Enrollments(time.Now())
		p.NewName = e.Name
		p.Command = "jw proxy setup " + p.ServerURL + " " + token
		s.renderProxies(w, p)
		return
	}
	var err error
	done := ""
	switch op {
	case "enable", "disable":
		err, done = s.relays.SetEnabled(id, op == "enable"), op+"d"
	case "delete":
		err, done = s.relays.Delete(id), "deleted"
	case "revoke":
		err, done = s.relays.Revoke(id), "revoked"
	default:
		err = errors.New("unknown action")
	}
	if err != nil {
		p := s.proxiesForm(r)
		p.Error = err.Error()
		s.renderProxies(w, p)
		return
	}
	http.Redirect(w, r, "/admin/proxies?"+url.Values{"done": {done}, "name": {name}}.Encode(), http.StatusSeeOther)
}
