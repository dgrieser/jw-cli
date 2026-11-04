package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dgrieser/jw-cli/internal/accounts"
	"github.com/dgrieser/jw-cli/internal/i18n"
)

// The API keys page (/account/keys): a user's keys, making, changing,
// rotating and deleting them. A secret shows once, in the answer to the form
// that made it, never in an address.

func (s *Server) keysForm(r *http.Request) authPage {
	p := s.authBase(r, func(a *i18n.AuthText) string { return a.APIKeys })
	p.Email = userOf(r).Email
	p.Keys = s.accounts.Keys(p.Email)
	p.MaxKeys, p.MaxKeyName = accounts.MaxKeys, accounts.MaxKeyName
	p.ServerURL = s.settingsNow().PublicURL
	if p.ServerURL == "" {
		p.ServerURL = s.scheme(r) + "://" + r.Host
	}
	return p
}

func (s *Server) keysPage(w http.ResponseWriter, r *http.Request) {
	s.renderAuth(w, http.StatusOK, "keys", s.keysForm(r))
}

// expiryFrom reads the expiry fields of a form: the choice in field, and a
// date in field_date for the choice "date". keep reports "as it is".
func expiryFrom(r *http.Request, field string) (exp time.Time, keep bool, err error) {
	v := r.PostFormValue(field)
	switch v {
	case "keep":
		return time.Time{}, true, nil
	case "date":
		d := strings.TrimSpace(r.PostFormValue(field + "_date"))
		if d == "" {
			return time.Time{}, false, errKeyNoDate
		}
		v = d
	case "never", "7d", "30d", "90d", "1y", "":
	default:
		return time.Time{}, false, accounts.ErrKeyExpiry
	}
	exp, err = accounts.ParseExpiry(v, time.Now(), time.Local)
	return exp, false, err
}

var errKeyNoDate = errors.New("no date")

// graceFrom reads how long an old secret keeps working after a rotation.
func graceFrom(r *http.Request) time.Duration {
	return map[string]time.Duration{"1h": time.Hour, "1d": 24 * time.Hour, "7d": 7 * 24 * time.Hour}[r.PostFormValue("grace")]
}

func (s *Server) keysPost(w http.ResponseWriter, r *http.Request) {
	p := s.keysForm(r)
	email, id := p.Email, r.PostFormValue("id")
	name := func() string {
		for _, k := range p.Keys {
			if k.ID == id {
				return k.Name
			}
		}
		return id
	}
	var err error
	switch r.PostFormValue("op") {
	case "create":
		var exp time.Time
		if exp, _, err = expiryFrom(r, "expires"); err == nil {
			var k accounts.KeyInfo
			if p.NewKey, k, err = s.accounts.CreateKey(email, r.PostFormValue("name"), exp); err == nil {
				p.NewKeyName = k.Name
			}
		}
	case "rotate":
		exp, keep, ferr := expiryFrom(r, "expires")
		err = ferr
		if err == nil {
			var expires *time.Time
			if !keep {
				expires = &exp
			}
			var k accounts.KeyInfo
			if p.NewKey, k, err = s.accounts.RotateKey(email, id, graceFrom(r), expires); err == nil {
				p.NewKeyName = k.Name
			}
		}
	case "update":
		exp, keep, ferr := expiryFrom(r, "expires")
		err = ferr
		if err == nil {
			up := accounts.KeyUpdate{}
			if n := r.PostFormValue("name"); n != "" {
				up.Name = &n
			}
			if !keep {
				up.Expires = &exp
			}
			var k accounts.KeyInfo
			if k, err = s.accounts.UpdateKey(email, id, up); err == nil {
				p.Notice = fmt.Sprintf(p.A.MsgKeyUpdated, k.Name)
			}
		}
	case "delete":
		n := name()
		if err = s.accounts.DeleteKey(email, id); err == nil {
			p.Notice = fmt.Sprintf(p.A.MsgKeyDeleted, n)
		}
	default:
		err = fmt.Errorf("unknown action %q", r.PostFormValue("op"))
	}
	if errors.Is(err, errKeyNoDate) {
		err = errors.New(p.A.ErrKeyBadExpiry)
	}
	if err != nil {
		p.Error = errText(p.A, err)
	}
	p.Keys = s.accounts.Keys(email)
	s.renderAuth(w, http.StatusOK, "keys", p)
}
