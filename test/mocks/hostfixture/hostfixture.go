// Package hostfixture wraps the GitHub and GitLab mocks behind one test-side interface so parity, push
// adapter, and pipeline tests run the same scenario against both hosts.
package hostfixture

import (
	"testing"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/adapters/codehost/github"
	"github.com/GokulMV/DocTheRepo/internal/adapters/codehost/gitlab"
	"github.com/GokulMV/DocTheRepo/internal/ports"
	"github.com/GokulMV/DocTheRepo/test/mocks/githubmock"
	"github.com/GokulMV/DocTheRepo/test/mocks/gitlabmock"
)

// Repo is the repository every fixture creates.
const Repo = "acme/shop"

// WebhookSecret is configured on every fixture's adapter.
const WebhookSecret = "s3cret"

// Fixture is one mocked git host with an adapter pointed at it.
type Fixture struct {
	Name string
	Host ports.CodeHost
	// Push commits changes (nil deletes) on top of branch as author and returns the new head.
	Push func(branch string, changes map[string]*string, author string) string
	Head func(branch string) string
	File func(branch, path string) (string, bool)
	// Protect rejects direct pushes to branch and requires an approval to merge into it.
	Protect func(branch string)
	Approve func(number int, reviewer string)
	// SetChecks sets CI for a commit: "success", "failure", or "pending".
	SetChecks func(sha, state string)
	// ConflictOnUpdate makes branch updates (update-branch / rebase) fail.
	ConflictOnUpdate func(bool)
	AddReviewer      func(user string)
	// PRComments returns the comments/notes on a PR.
	PRComments func(number int) []string
	// BotLogin is the adapter's bot identity login.
	BotLogin string
}

// GitHub starts a GitHub mock with Repo and returns its fixture.
func GitHub(t testing.TB, files map[string]string) *Fixture {
	t.Helper()
	m := githubmock.New()
	t.Cleanup(m.Close)
	m.CreateRepo(Repo, "main", files)
	h, err := github.New(ports.ProviderConfig{BaseURL: m.APIURL(), APIKey: "ghp_token",
		Extra: map[string]string{"auth": "token", "webhook_secret": WebhookSecret}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &Fixture{Name: "github", Host: h, BotLogin: m.BotLogin,
		Push:    func(b string, c map[string]*string, a string) string { return m.Push(Repo, b, c, a) },
		Head:    func(b string) string { return m.Head(Repo, b) },
		File:    func(b, p string) (string, bool) { return m.File(Repo, b, p) },
		Protect: func(b string) { m.Protect(Repo, b) },
		Approve: func(n int, _ string) { m.Approve(Repo, n) },
		SetChecks: func(sha, st string) {
			m.SetChecks(sha, st)
		},
		ConflictOnUpdate: func(v bool) { m.ConflictOnUpdate = v },
		AddReviewer:      func(u string) { m.AddCollaborator(Repo, u) },
		PRComments: func(n int) []string {
			for _, p := range m.PRs(Repo) {
				if p.Number == n {
					return p.Comments
				}
			}
			return nil
		},
	}
}

// GitLab starts a GitLab mock with Repo and returns its fixture.
func GitLab(t testing.TB, files map[string]string) *Fixture {
	t.Helper()
	m := gitlabmock.New()
	t.Cleanup(m.Close)
	m.CreateProject(Repo, "main", files)
	h, err := gitlab.New(ports.ProviderConfig{BaseURL: m.BaseURL(), APIKey: "glpat-token", Extra: map[string]string{"webhook_secret": WebhookSecret}})
	if err != nil {
		t.Fatal(err)
	}
	h.RebasePoll = 2 * time.Millisecond
	return &Fixture{Name: "gitlab", Host: h, BotLogin: m.BotUsername,
		Push:    func(b string, c map[string]*string, a string) string { return m.Push(Repo, b, c, a) },
		Head:    func(b string) string { return m.Head(Repo, b) },
		File:    func(b, p string) (string, bool) { return m.File(Repo, b, p) },
		Protect: func(b string) { m.Protect(Repo, b) },
		Approve: func(n int, r string) { m.Approve(Repo, n, r) },
		SetChecks: func(sha, st string) {
			m.SetPipeline(sha, map[string]string{"success": "success", "failure": "failed", "pending": "running"}[st])
		},
		ConflictOnUpdate: func(v bool) { m.ConflictOnRebase = v },
		AddReviewer:      func(u string) { m.AddUser(u) },
		PRComments: func(n int) []string {
			for _, mr := range m.MRs(Repo) {
				if mr.IID == n {
					return mr.Notes
				}
			}
			return nil
		},
	}
}

// All returns both fixtures, each over a fresh repository containing files.
func All(t testing.TB, files map[string]string) []*Fixture {
	return []*Fixture{GitHub(t, files), GitLab(t, files)}
}

// S returns a pointer to s (for Push change maps).
func S(s string) *string { return &s }
