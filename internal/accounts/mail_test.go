package accounts

import (
	"bufio"
	"context"
	"encoding/base64"
	"net"
	"strings"
	"testing"
)

// fakeSMTP accepts one message on a local port and returns what it got.
func fakeSMTP(t *testing.T) (port int, got <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	ch := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		r := bufio.NewReader(conn)
		say := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		say("220 fake")
		var log strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			log.WriteString(line)
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				say("250-fake")
				say("250 AUTH PLAIN")
			case strings.HasPrefix(cmd, "AUTH"):
				say("235 ok")
			case cmd == "DATA":
				say("354 go")
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" {
						break
					}
					log.WriteString(l)
				}
				say("250 queued")
			case cmd == "QUIT":
				say("221 bye")
				ch <- log.String()
				return
			default:
				say("250 ok")
			}
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, ch
}

func TestSMTPMailer(t *testing.T) {
	port, got := fakeSMTP(t)
	m := SMTPMailer{SMTP: SMTP{Host: "127.0.0.1", Port: port, Security: SecurityNone,
		Username: "jw", Password: "pw", From: "JW <jw@example.org>"}}
	err := m.Send(context.Background(), Mail{To: "anne@example.org", Subject: "Bestätige\r\nBcc: x@evil", Body: "Hallo\nLink: https://x/y"})
	if err != nil {
		t.Fatal(err)
	}
	log := <-got
	for _, want := range []string{"MAIL FROM:<jw@example.org>", "RCPT TO:<anne@example.org>", "AUTH PLAIN", "To: <anne@example.org>",
		"Subject: =?utf-8?q?Best=C3=A4tige__Bcc:_x@evil?="} {
		if !strings.Contains(log, want) {
			t.Errorf("missing %q in\n%s", want, log)
		}
	}
	if strings.Contains(log, "\r\nBcc:") {
		t.Error("header injected through the subject")
	}
	body := log[strings.Index(log, "\r\n\r\n")+4:]
	body, _, _ = strings.Cut(body, "QUIT")
	dec, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(body, "\r\n", ""))
	if err != nil || string(dec) != "Hallo\r\nLink: https://x/y" {
		t.Errorf("body %q, %v", dec, err)
	}
}
