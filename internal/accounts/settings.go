package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Who may register.
const (
	RegisterClosed   = "closed"   // only admins make accounts
	RegisterApproval = "approval" // anybody, and an admin approves the account
	RegisterOpen     = "open"     // anybody, signed in once the address is confirmed
)

// SMTP security.
const (
	SecurityStartTLS = "starttls" // plain connection upgraded with STARTTLS (port 587)
	SecurityTLS      = "tls"      // TLS from the start (port 465)
	SecurityNone     = "none"     // unencrypted (a relay on the same machine)
)

// Settings is what admins configure on the server's settings page.
type Settings struct {
	// Registration is RegisterClosed, RegisterApproval or RegisterOpen.
	Registration string `json:"registration"`
	// Anonymous lets people read without an account; accounts are then only
	// for the admins.
	Anonymous bool `json:"anonymous,omitempty"`
	// PublicURL is the address readers reach the server at
	// (https://jw.example.org), which the links in mails lead to.
	PublicURL string `json:"publicURL,omitempty"`
	SMTP      SMTP   `json:"smtp"`
}

// SMTP is the mail server the mails go through.
type SMTP struct {
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Security string `json:"security,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	// From is the sender, "jw <jw@example.org>" or an address alone.
	From string `json:"from,omitempty"`
}

// Configured reports whether mails can be sent.
func (m SMTP) Configured() bool { return m.Host != "" && m.From != "" }

// MailReady reports whether mails with links can be sent: a mail server and
// the address the links lead to.
func (s Settings) MailReady() bool { return s.SMTP.Configured() && s.PublicURL != "" }

// CanRegister reports whether the registration form is offered.
func (s Settings) CanRegister() bool { return s.Registration != RegisterClosed && s.MailReady() }

// Defaults are the settings of a new server.
func Defaults() Settings {
	return Settings{Registration: RegisterClosed, SMTP: SMTP{Port: 587, Security: SecurityStartTLS}}
}

// Errors of Validate.
var (
	ErrBadPublicURL   = errors.New("the public address must be an http:// or https:// URL without a path")
	ErrRegisterNoMail = errors.New("registration needs a mail server and the public address: the confirmation is sent by email")
	ErrBadSMTP        = errors.New("the mail server needs a host, a port between 1 and 65535 and a sender address")
)

// Normalize tidies what a form sent: spaces, defaults, a trailing slash.
func (s *Settings) Normalize() {
	s.PublicURL = strings.TrimRight(strings.TrimSpace(s.PublicURL), "/")
	switch s.Registration {
	case RegisterApproval, RegisterOpen:
	default:
		s.Registration = RegisterClosed
	}
	m := &s.SMTP
	m.Host, m.Username, m.From = strings.TrimSpace(m.Host), strings.TrimSpace(m.Username), strings.TrimSpace(m.From)
	switch m.Security {
	case SecurityTLS, SecurityNone:
	default:
		m.Security = SecurityStartTLS
	}
	if m.Port == 0 {
		m.Port = map[string]int{SecurityTLS: 465, SecurityNone: 25}[m.Security]
		if m.Port == 0 {
			m.Port = 587
		}
	}
}

// Validate checks settings before they are saved.
func (s Settings) Validate() error {
	if s.PublicURL != "" {
		u, err := url.Parse(s.PublicURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || strings.Contains(s.PublicURL, "#") || u.User != nil {
			return ErrBadPublicURL
		}
	}
	m := s.SMTP
	if m.Host != "" || m.From != "" {
		if m.Host == "" || m.Port < 1 || m.Port > 65535 || strings.ContainsAny(m.Host, " /") {
			return ErrBadSMTP
		}
		if _, err := senderAddress(m.From); err != nil {
			return ErrBadSMTP
		}
	}
	if s.Registration != RegisterClosed && !s.MailReady() {
		return ErrRegisterNoMail
	}
	return nil
}

// SettingsFile is the settings in settings.json in the data folder.
type SettingsFile struct {
	path string
	mu   sync.RWMutex
	cur  Settings
}

// LoadSettings reads path, the defaults when it does not exist yet.
func LoadSettings(path string) (*SettingsFile, error) {
	f := &SettingsFile{path: path, cur: Defaults()}
	if err := f.load(); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *SettingsFile) load() error {
	b, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	s := Defaults()
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("%s: %w", f.path, err)
	}
	s.Normalize()
	f.cur = s
	return nil
}

// Path is the file.
func (f *SettingsFile) Path() string { return f.path }

// Get returns the current settings.
func (f *SettingsFile) Get() Settings {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.cur
}

// Save validates and stores s (0600: it holds the mail server's password).
func (f *SettingsFile) Save(s Settings) error {
	s.Normalize()
	if err := s.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := writeFile(f.path, b); err != nil {
		return err
	}
	f.cur = s
	return nil
}

// Paths of a data folder.
func Paths(dir string) (accounts, settings string) {
	return filepath.Join(dir, "accounts.json"), filepath.Join(dir, "settings.json")
}
