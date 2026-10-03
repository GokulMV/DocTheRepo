// Package email sends the Hub's few emails (invite and password links) over SMTP: any provider works
// (Gmail and Google Workspace, Microsoft 365, Amazon SES, SendGrid, Mailgun, Postmark, your own server).
package email

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Message is one email.
type Message struct {
	To      string
	Subject string
	Text    string
}

// Mailer sends email through an SMTP server.
type Mailer struct {
	host, port string
	user, pass string
	from       mail.Address
	// implicitTLS: smtps:// (port 465); otherwise STARTTLS is used when the server offers it, and
	// required unless the URL says ?starttls=off (a local relay).
	implicitTLS bool
	requireTLS  bool
	tlsConfig   *tls.Config
	dialTimeout time.Duration
}

// Parse builds a Mailer from an SMTP URL and a From address:
//
//	smtp://user:password@smtp.example.com:587    STARTTLS (required)
//	smtps://user:password@smtp.example.com:465   TLS from the start
//	smtp://relay.internal:25?starttls=off        a trusted relay without TLS
func Parse(rawURL, from string) (*Mailer, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "smtp" && u.Scheme != "smtps") {
		return nil, errors.New("email: the SMTP URL must look like smtp://user:password@host:587 or smtps://user:password@host:465")
	}
	addr, err := mail.ParseAddress(from)
	if err != nil {
		return nil, fmt.Errorf("email: the From address %q is not valid", from)
	}
	m := &Mailer{host: u.Hostname(), port: u.Port(), from: *addr, implicitTLS: u.Scheme == "smtps", requireTLS: u.Query().Get("starttls") != "off",
		dialTimeout: 15 * time.Second}
	if m.port == "" {
		m.port = map[bool]string{true: "465", false: "587"}[m.implicitTLS]
	}
	if u.User != nil {
		m.user = u.User.Username()
		m.pass, _ = u.User.Password()
	}
	m.tlsConfig = &tls.Config{ServerName: m.host, MinVersion: tls.VersionTLS12}
	return m, nil
}

// From is the sender address.
func (m *Mailer) From() string { return m.from.String() }

// Server is host:port, for status pages (never the credentials).
func (m *Mailer) Server() string { return net.JoinHostPort(m.host, m.port) }

// Send delivers one message.
func (m *Mailer) Send(ctx context.Context, msg Message) error {
	to, err := mail.ParseAddress(msg.To)
	if err != nil {
		return fmt.Errorf("email: recipient %q is not valid", msg.To)
	}
	d := net.Dialer{Timeout: m.dialTimeout}
	var conn net.Conn
	if m.implicitTLS {
		conn, err = tls.DialWithDialer(&d, "tcp", m.Server(), m.tlsConfig)
	} else {
		conn, err = d.DialContext(ctx, "tcp", m.Server())
	}
	if err != nil {
		return fmt.Errorf("email: connect to %s: %w", m.Server(), err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	} else {
		_ = conn.SetDeadline(time.Now().Add(time.Minute))
	}
	c, err := smtp.NewClient(conn, m.host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("email: %w", err)
	}
	defer c.Close()
	if !m.implicitTLS {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(m.tlsConfig); err != nil {
				return fmt.Errorf("email: STARTTLS: %w", err)
			}
		} else if m.requireTLS {
			return errors.New("email: the server does not offer STARTTLS; use smtps:// or add ?starttls=off for a trusted local relay")
		}
	}
	if m.user != "" {
		if err := c.Auth(smtp.PlainAuth("", m.user, m.pass, m.host)); err != nil {
			return fmt.Errorf("email: sign-in to the SMTP server failed: %w", err)
		}
	}
	if err := c.Mail(m.from.Address); err != nil {
		return fmt.Errorf("email: %w", err)
	}
	if err := c.Rcpt(to.Address); err != nil {
		return fmt.Errorf("email: recipient refused: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("email: %w", err)
	}
	if _, err := w.Write(m.compose(to, msg)); err != nil {
		return fmt.Errorf("email: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("email: %w", err)
	}
	return c.Quit()
}

func (m *Mailer) compose(to *mail.Address, msg Message) []byte {
	id := make([]byte, 12)
	_, _ = rand.Read(id)
	domain := m.from.Address[strings.LastIndex(m.from.Address, "@")+1:]
	var b strings.Builder
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	h("From", m.from.String())
	h("To", to.String())
	h("Subject", mime.QEncoding.Encode("utf-8", msg.Subject))
	h("Date", time.Now().UTC().Format(time.RFC1123Z))
	h("Message-ID", "<"+hex.EncodeToString(id)+"@"+domain+">")
	h("MIME-Version", "1.0")
	h("Content-Type", "text/plain; charset=utf-8")
	h("Content-Transfer-Encoding", "8bit")
	h("Auto-Submitted", "auto-generated")
	b.WriteString("\r\n")
	// Line endings are normalised to CRLF; net/smtp's data writer does the dot-stuffing.
	for _, line := range strings.Split(strings.ReplaceAll(msg.Text, "\r\n", "\n"), "\n") {
		b.WriteString(line + "\r\n")
	}
	return []byte(b.String())
}

// Port returns the port as a number, for tests and status.
func (m *Mailer) Port() int { p, _ := strconv.Atoi(m.port); return p }
