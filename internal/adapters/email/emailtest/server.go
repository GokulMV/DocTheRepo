// Package emailtest is a tiny in-process SMTP server for tests: it accepts every message (optionally
// requiring AUTH PLAIN) and records it.
package emailtest

import (
	"bufio"
	"encoding/base64"
	"net"
	"strings"
	"sync"
	"testing"
)

// Mail is one received message.
type Mail struct {
	From, To string
	Data     string // headers and body, dot-unstuffed, CRLF line endings
	User     string // the AUTH PLAIN user, if any
}

// Server is a running fake SMTP server.
type Server struct {
	Addr string
	// User and Pass, when set, are required (AUTH PLAIN; the server does not offer STARTTLS, so
	// clients connect with ?starttls=off).
	User, Pass string
	// Reject, when set, makes RCPT TO fail with this text.
	Reject string

	l    net.Listener
	mu   sync.Mutex
	mail []Mail
}

// Start runs a server until the test ends.
func Start(t testing.TB) *Server {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Addr: l.Addr().String(), l: l}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s
}

// URL is the smtp:// URL to reach the server.
func (s *Server) URL() string {
	auth := ""
	if s.User != "" {
		auth = s.User + ":" + s.Pass + "@"
	}
	return "smtp://" + auth + s.Addr + "?starttls=off"
}

// Mail returns what has been received so far.
func (s *Server) Mail() []Mail {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Mail(nil), s.mail...)
}

func (s *Server) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	say := func(line string) { _, _ = c.Write([]byte(line + "\r\n")) }
	say("220 emailtest ready")
	var cur Mail
	authed := s.User == ""
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			say("250-emailtest")
			say("250-8BITMIME")
			say("250 AUTH PLAIN")
		case strings.HasPrefix(cmd, "AUTH PLAIN"):
			b, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(line[len("AUTH PLAIN"):]))
			parts := strings.Split(string(b), "\x00")
			if len(parts) == 3 && parts[1] == s.User && parts[2] == s.Pass {
				authed, cur.User = true, parts[1]
				say("235 ok")
			} else {
				say("535 bad credentials")
			}
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			if !authed {
				say("530 authentication required")
				continue
			}
			cur.From = strings.Trim(strings.Fields(line[len("MAIL FROM:"):]+" ")[0], "<>")
			say("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			if s.Reject != "" {
				say("550 " + s.Reject)
				continue
			}
			cur.To = strings.Trim(line[len("RCPT TO:"):], "<> ")
			say("250 ok")
		case cmd == "DATA":
			say("354 go ahead")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(strings.TrimPrefix(l, "."))
			}
			cur.Data = b.String()
			s.mu.Lock()
			s.mail = append(s.mail, cur)
			s.mu.Unlock()
			cur = Mail{User: cur.User}
			say("250 queued")
		case cmd == "RSET", cmd == "NOOP":
			say("250 ok")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("502 not implemented")
		}
	}
}
