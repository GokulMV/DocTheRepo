package email_test

import (
	"context"
	"strings"
	"testing"

	"github.com/GokulMV/DocTheRepo/internal/adapters/email"
	"github.com/GokulMV/DocTheRepo/internal/adapters/email/emailtest"
)

func TestParse(t *testing.T) {
	for _, c := range []struct {
		url, from string
		port      int
		ok        bool
	}{
		{"smtp://u:p@smtp.example.com", "Docs <docs@example.com>", 587, true},
		{"smtps://u:p@smtp.example.com", "docs@example.com", 465, true},
		{"smtp://relay:25?starttls=off", "docs@example.com", 25, true},
		{"http://smtp.example.com", "docs@example.com", 0, false},
		{"smtp://smtp.example.com", "not an address", 0, false},
		{"smtp://", "docs@example.com", 0, false},
	} {
		m, err := email.Parse(c.url, c.from)
		if (err == nil) != c.ok {
			t.Fatalf("%s: err %v", c.url, err)
		}
		if c.ok && m.Port() != c.port {
			t.Fatalf("%s: port %d, want %d", c.url, m.Port(), c.port)
		}
	}
}

func TestSend(t *testing.T) {
	srv := emailtest.Start(t)
	srv.User, srv.Pass = "apikey", "s3cr:et"
	m, err := email.Parse(srv.URL(), "DocTheRepo <docs@example.com>")
	if err != nil {
		t.Fatal(err)
	}
	body := "Hello Ana,\n\n.a line starting with a dot\nLink: https://hub.example.com/invite/abc\n"
	if err := m.Send(context.Background(), email.Message{To: "Ana <ana@example.com>", Subject: "You're invited — welcome", Text: body}); err != nil {
		t.Fatal(err)
	}
	got := srv.Mail()
	if len(got) != 1 {
		t.Fatalf("got %d messages", len(got))
	}
	g := got[0]
	if g.From != "docs@example.com" || g.To != "ana@example.com" || g.User != "apikey" {
		t.Fatalf("envelope %+v", g)
	}
	for _, want := range []string{"From: \"DocTheRepo\" <docs@example.com>\r\n", "To: \"Ana\" <ana@example.com>\r\n", "Subject: =?utf-8?q?",
		"Content-Type: text/plain; charset=utf-8\r\n", "Auto-Submitted: auto-generated\r\n", "\r\n\r\nHello Ana,\r\n",
		"\r\n.a line starting with a dot\r\n", "https://hub.example.com/invite/abc\r\n"} {
		if !strings.Contains(g.Data, want) {
			t.Fatalf("message lacks %q:\n%s", want, g.Data)
		}
	}
}

func TestSendErrors(t *testing.T) {
	srv := emailtest.Start(t)
	srv.User, srv.Pass = "u", "right"
	bad := strings.Replace(srv.URL(), "u:right@", "u:wrong@", 1)
	m, _ := email.Parse(bad, "docs@example.com")
	if err := m.Send(context.Background(), email.Message{To: "a@example.com", Subject: "s", Text: "t"}); err == nil || !strings.Contains(err.Error(), "sign-in") {
		t.Fatalf("bad password: %v", err)
	}
	// STARTTLS is required unless the URL turns it off.
	strict, _ := email.Parse(strings.Replace(srv.URL(), "?starttls=off", "", 1), "docs@example.com")
	if err := strict.Send(context.Background(), email.Message{To: "a@example.com", Subject: "s", Text: "t"}); err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("no STARTTLS: %v", err)
	}
	srv.Reject = "no such user"
	ok, _ := email.Parse(srv.URL(), "docs@example.com")
	if err := ok.Send(context.Background(), email.Message{To: "a@example.com", Subject: "s", Text: "t"}); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("rejected: %v", err)
	}
	if err := ok.Send(context.Background(), email.Message{To: "nope", Subject: "s", Text: "t"}); err == nil {
		t.Fatal("bad recipient accepted")
	}
}
