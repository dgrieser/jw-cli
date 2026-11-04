package accounts

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Mail is one message: plain text, UTF-8.
type Mail struct {
	To      string
	Subject string
	Body    string
}

// Mailer sends mails.
type Mailer interface {
	Send(ctx context.Context, m Mail) error
}

// SMTPMailer sends through the mail server of the settings.
type SMTPMailer struct{ SMTP SMTP }

// mailTimeout bounds one delivery to the mail server.
const mailTimeout = 30 * time.Second

func senderAddress(from string) (*mail.Address, error) {
	a, err := mail.ParseAddress(from)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// Send delivers m.
func (s SMTPMailer) Send(ctx context.Context, m Mail) error {
	cfg := s.SMTP
	if !cfg.Configured() {
		return fmt.Errorf("no mail server configured")
	}
	from, err := senderAddress(cfg.From)
	if err != nil {
		return fmt.Errorf("sender %q: %w", cfg.From, err)
	}
	to, err := mail.ParseAddress(m.To)
	if err != nil {
		return fmt.Errorf("recipient %q: %w", m.To, err)
	}
	msg := compose(from, to, m.Subject, m.Body)

	ctx, cancel := context.WithTimeout(ctx, mailTimeout)
	defer cancel()
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	d := &net.Dialer{}
	var conn net.Conn
	if cfg.Security == SecurityTLS {
		conn, err = (&tls.Dialer{NetDialer: d, Config: &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("mail server %s: %w", addr, err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("mail server %s: %w", addr, err)
	}
	defer func() { _ = c.Close() }()
	if cfg.Security == SecurityStartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("mail server %s does not offer STARTTLS", addr)
		}
		if err := c.StartTLS(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("mail server %s: STARTTLS: %w", addr, err)
		}
	}
	if cfg.Username != "" {
		if ok, _ := c.Extension("AUTH"); !ok {
			return fmt.Errorf("mail server %s does not offer authentication", addr)
		}
		// PlainAuth refuses to send the password unencrypted except to
		// localhost: with security "none" a remote server gets no password
		if err := c.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return fmt.Errorf("mail server %s: login: %w", addr, err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("mail server %s: sender: %w", addr, err)
	}
	if err := c.Rcpt(to.Address); err != nil {
		return fmt.Errorf("mail server %s: recipient: %w", addr, err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("mail server %s: %w", addr, err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("mail server %s: %w", addr, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail server %s: %w", addr, err)
	}
	return c.Quit()
}

// compose writes the message: headers, and the body as base64 so any line
// length and character survive.
func compose(from, to *mail.Address, subject, body string) []byte {
	var b bytes.Buffer
	host := "localhost"
	if at := strings.LastIndexByte(from.Address, '@'); at >= 0 {
		host = from.Address[at+1:]
	}
	hdr := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	hdr("From", from.String())
	hdr("To", to.String())
	hdr("Subject", mime.QEncoding.Encode("utf-8", stripCRLF(subject)))
	hdr("Date", time.Now().Format(time.RFC1123Z))
	hdr("Message-ID", "<"+NewSecret()+"@"+host+">")
	hdr("MIME-Version", "1.0")
	hdr("Content-Type", `text/plain; charset="utf-8"`)
	hdr("Content-Transfer-Encoding", "base64")
	hdr("Auto-Submitted", "auto-generated")
	b.WriteString("\r\n")
	enc := base64.StdEncoding.EncodeToString([]byte(strings.ReplaceAll(body, "\n", "\r\n")))
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n")
	return b.Bytes()
}

func stripCRLF(s string) string { return strings.NewReplacer("\r", " ", "\n", " ").Replace(s) }
