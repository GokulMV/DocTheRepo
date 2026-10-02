// Package githubmock is an in-memory GitHub REST API sufficient for the Hub's GitHub adapter and for
// pipeline/E2E tests: repositories with branches, commits, trees, contents, compare, refs, the Git Data
// API, pull requests with reviews/checks/merge/update-branch, protected branches, and webhooks. Test
// helpers push commits, approve PRs, set check results, and protect branches.
package githubmock

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// Commit is a snapshot: the full tree plus parent.
type Commit struct {
	SHA     string
	Parent  string
	Message string
	Author  string
	Email   string
	At      time.Time
	Files   map[string]string
}

// PR is a pull request.
type PR struct {
	Number    int
	Head      string
	Base      string
	Title     string
	Body      string
	State     string // open | closed
	Merged    bool
	Reviewers []string
	Teams     []string
	Approved  bool
	Comments  []string
	CreatedAt time.Time
}

// Repo is one repository.
type Repo struct {
	FullName      string
	DefaultBranch string
	Branches      map[string]string // name → sha
	Protected     map[string]bool
	PRs           map[int]*PR
	nextPR        int
	Hooks         []map[string]any
	Collaborators map[string]bool
}

// Server is a running mock.
type Server struct {
	*httptest.Server
	mu      sync.Mutex
	repos   map[string]*Repo
	commits map[string]*Commit
	trees   map[string]map[string]string // tree sha → files
	checks  map[string]string            // sha → pending|success|failure
	teams   map[string]bool              // "org/slug"
	// BotLogin is what GET /user and /app report.
	BotLogin string
	// ManifestCode is the one-time code POST /app-manifests/{code}/conversions accepts (the GitHub App
	// manifest flow); the conversion returns a new app with a real RSA key.
	ManifestCode string
	// AppTokenRequests counts installation-token exchanges (GitHub App auth).
	AppTokenRequests int
	// Installations records App installation state by id: "suspended" or "deleted" (absent = active).
	Installations map[string]string
	// deliveries feeds the webhook worker (in order); deliveryLog records outcomes.
	deliveries  chan delivery
	deliveryLog []Delivery
	// ConflictOnUpdate makes update-branch fail with 422 (simulates an unresolvable rebase).
	ConflictOnUpdate bool
	now              func() time.Time
}

// New starts an empty mock.
func New() *Server {
	s := &Server{repos: map[string]*Repo{}, commits: map[string]*Commit{}, trees: map[string]map[string]string{},
		checks: map[string]string{}, teams: map[string]bool{}, Installations: map[string]string{}, BotLogin: "dth-hub[bot]", now: time.Now,
		deliveries: make(chan delivery, 256)}
	go s.deliverLoop()
	r := chi.NewRouter()
	r.Route("/api/v3", func(r chi.Router) { s.routes(r) })
	s.Server = httptest.NewServer(r)
	return s
}

// APIURL is the base URL for go-github's WithEnterpriseURLs.
func (s *Server) APIURL() string { return s.URL + "/api/v3/" }

// --- test helpers ---

// CreateRepo creates a repository with an initial commit on its default branch.
func (s *Server) CreateRepo(fullName, defaultBranch string, files map[string]string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repos[fullName] = &Repo{FullName: fullName, DefaultBranch: defaultBranch, Branches: map[string]string{},
		Protected: map[string]bool{}, PRs: map[int]*PR{}, nextPR: 1, Collaborators: map[string]bool{}}
	sha := s.commitLocked("", "initial", "dev", "dev@example.com", files)
	s.repos[fullName].Branches[defaultBranch] = sha // repository creation fires no push event
	return sha
}

// Push commits changes (nil value deletes a path) on top of branch and returns the new SHA.
func (s *Server) Push(fullName, branch string, changes map[string]*string, author string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.repos[fullName]
	parent := r.Branches[branch]
	files := copyFiles(s.commits[parent].Files)
	for p, c := range changes {
		if c == nil {
			delete(files, p)
		} else {
			files[p] = *c
		}
	}
	sha := s.commitLocked(parent, "push by "+author, author, author+"@example.com", files)
	s.moveLocked(r, branch, sha, author)
	return sha
}

// Protect marks a branch as protected (direct ref updates are rejected).
func (s *Server) Protect(fullName, branch string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repos[fullName].Protected[branch] = true
}

// Approve records an approving review on a PR.
func (s *Server) Approve(fullName string, number int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repos[fullName].PRs[number].Approved = true
}

// SetChecks sets the combined check result for a commit.
func (s *Server) SetChecks(sha, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checks[sha] = state
}

// AddCollaborator lets a user review in a repo; AddTeam registers "org/slug".
func (s *Server) AddCollaborator(fullName, user string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repos[fullName].Collaborators[user] = true
}

// AddTeam registers an org team.
func (s *Server) AddTeam(orgSlug string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.teams[orgSlug] = true
}

// File reads a file at a branch head (for assertions).
func (s *Server) File(fullName, branch, path string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.commits[s.repos[fullName].Branches[branch]]
	v, ok := c.Files[path]
	return v, ok
}

// Head returns a branch head SHA.
func (s *Server) Head(fullName, branch string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.repos[fullName].Branches[branch]
}

// PRs returns a repo's PRs.
func (s *Server) PRs(fullName string) []PR {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []PR
	for _, p := range s.repos[fullName].PRs {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

// CommitMeta returns a commit's author and message.
func (s *Server) CommitMeta(sha string) (author, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.commits[sha]
	return c.Author, c.Message
}

// Hooks returns registered webhooks.
func (s *Server) Hooks(fullName string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.repos[fullName].Hooks...)
}

func copyFiles(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func hash(parts ...string) string {
	h := sha1.New()
	for _, p := range parts {
		io.WriteString(h, p)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func blobSHA(content string) string { return hash("blob", content) }

func (s *Server) commitLocked(parent, msg, author, email string, files map[string]string) string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{parent, msg, author, strconv.FormatInt(s.now().UnixNano(), 10)}
	for _, k := range keys {
		parts = append(parts, k, blobSHA(files[k]))
	}
	sha := hash(parts...)
	s.commits[sha] = &Commit{SHA: sha, Parent: parent, Message: msg, Author: author, Email: email, At: s.now(), Files: files}
	s.trees[sha] = files // the tree of a commit shares the commit's sha in this mock
	return sha
}

// --- routing ---

func (s *Server) routes(r chi.Router) {
	r.Get("/user", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"login": s.BotLogin, "id": 4242, "name": "DocTheRepo Hub"})
	})
	r.Get("/users/{login}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"login": chi.URLParam(r, "login"), "id": 4242})
	})
	r.Get("/app", func(w http.ResponseWriter, r *http.Request) {
		// Like GitHub: only the App's JWT (Bearer, three segments) works here, not an installation token.
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || strings.Count(tok, ".") != 2 {
			writeJSON(w, 401, map[string]any{"message": "A JSON web token could not be decoded"})
			return
		}
		writeJSON(w, 200, map[string]any{"slug": strings.TrimSuffix(s.BotLogin, "[bot]"), "id": 1,
			"owner": map[string]any{"login": "acme", "type": "Organization"}})
	})
	installation := func(state string) http.HandlerFunc { // suspend / unsuspend / delete; App JWT only
		return func(w http.ResponseWriter, r *http.Request) {
			tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || strings.Count(tok, ".") != 2 {
				writeJSON(w, 401, map[string]any{"message": "A JSON web token could not be decoded"})
				return
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			id := chi.URLParam(r, "id")
			if s.Installations[id] == "deleted" {
				notFound(w)
				return
			}
			if state == "" {
				delete(s.Installations, id)
			} else {
				s.Installations[id] = state
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}
	r.Put("/app/installations/{id}/suspended", installation("suspended"))
	r.Delete("/app/installations/{id}/suspended", installation(""))
	r.Delete("/app/installations/{id}", installation("deleted"))
	r.Get("/installation/repositories", s.listRepos)
	r.Post("/app-manifests/{code}/conversions", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		ok := s.ManifestCode != "" && chi.URLParam(r, "code") == s.ManifestCode
		if ok {
			s.ManifestCode = "" // single use, like GitHub
		}
		s.mu.Unlock()
		if !ok {
			writeJSON(w, 404, map[string]any{"message": "Not Found"})
			return
		}
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			writeJSON(w, 500, map[string]any{"message": err.Error()})
			return
		}
		pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
		writeJSON(w, 201, map[string]any{"id": 777, "slug": "docTheRepo-test", "name": "DocTheRepo test", "pem": string(pemKey),
			"webhook_secret": "whsec-from-manifest", "client_id": "Iv1.test", "owner": map[string]any{"login": "acme"}})
	})
	r.Get("/user/repos", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		var list []map[string]any
		for n, rp := range s.repos {
			list = append(list, map[string]any{"full_name": n, "default_branch": rp.DefaultBranch})
		}
		writeJSON(w, 200, list)
	})
	r.Post("/app/installations/{id}/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			writeJSON(w, 401, map[string]any{"message": "A JSON web token could not be decoded"})
			return
		}
		s.mu.Lock()
		st := s.Installations[chi.URLParam(r, "id")]
		s.AppTokenRequests++
		s.mu.Unlock()
		if st == "suspended" {
			writeJSON(w, 403, map[string]any{"message": "This installation has been suspended"})
			return
		}
		if st == "deleted" {
			notFound(w)
			return
		}
		writeJSON(w, 201, map[string]any{"token": "ghs_installation", "expires_at": s.now().Add(time.Hour).Format(time.RFC3339)})
	})
	r.Get("/orgs/{org}/teams/{slug}", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		ok := s.teams[chi.URLParam(r, "org")+"/"+chi.URLParam(r, "slug")]
		s.mu.Unlock()
		if !ok {
			notFound(w)
			return
		}
		writeJSON(w, 200, map[string]any{"slug": chi.URLParam(r, "slug"), "id": 7})
	})
	r.Route("/repos/{owner}/{repo}", func(r chi.Router) {
		r.Get("/", s.getRepo)
		r.Get("/branches/{branch}", s.getBranch)
		r.Get("/compare/{spec}", s.compare)
		r.Get("/contents/*", s.contents)
		r.Get("/commits", s.listCommits)
		r.Get("/commits/{sha}/status", s.combinedStatus)
		r.Get("/commits/{sha}/check-runs", s.checkRuns)
		r.Get("/collaborators/{user}", s.isCollaborator)
		r.Post("/hooks", s.createHook)
		r.Get("/hooks", s.listHooks)
		r.Patch("/hooks/{hook}", s.editHook)
		r.Route("/git", func(r chi.Router) {
			r.Get("/ref/*", s.getRef)
			r.Post("/refs", s.createRef)
			r.Patch("/refs/*", s.updateRef)
			r.Delete("/refs/*", s.deleteRef)
			r.Get("/commits/{sha}", s.getCommit)
			r.Post("/commits", s.createCommit)
			r.Get("/trees/{sha}", s.getTree)
			r.Post("/trees", s.createTree)
			r.Get("/blobs/{sha}", s.getBlob)
		})
		r.Post("/pulls", s.createPR)
		r.Get("/pulls", s.listPRs)
		r.Get("/pulls/{n}", s.getPR)
		r.Patch("/pulls/{n}", s.editPR)
		r.Put("/pulls/{n}/merge", s.mergePR)
		r.Put("/pulls/{n}/update-branch", s.updateBranch)
		r.Get("/pulls/{n}/reviews", s.reviews)
		r.Post("/pulls/{n}/requested_reviewers", s.requestReviewers)
		r.Post("/issues/{n}/comments", s.comment)
		r.Get("/teams", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, []any{}) })
	})
	r.Get("/orgs/{org}/teams/{slug}/repos/{owner}/{repo}", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		ok := s.teams[chi.URLParam(r, "org")+"/"+chi.URLParam(r, "slug")]
		s.mu.Unlock()
		if !ok {
			notFound(w)
			return
		}
		writeJSON(w, 200, map[string]any{"full_name": chi.URLParam(r, "owner") + "/" + chi.URLParam(r, "repo")})
	})
}

func (s *Server) repo(w http.ResponseWriter, r *http.Request) *Repo {
	rp := s.repos[chi.URLParam(r, "owner")+"/"+chi.URLParam(r, "repo")]
	if rp == nil {
		notFound(w)
	}
	return rp
}

func (s *Server) listRepos(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var names []string
	for n := range s.repos {
		names = append(names, n)
	}
	sort.Strings(names)
	var list []map[string]any
	for _, n := range names {
		list = append(list, map[string]any{"full_name": n, "default_branch": s.repos[n].DefaultBranch})
	}
	writeJSON(w, 200, map[string]any{"total_count": len(list), "repositories": list})
}

func (s *Server) getRepo(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rp := s.repo(w, r); rp != nil {
		writeJSON(w, 200, map[string]any{"full_name": rp.FullName, "default_branch": rp.DefaultBranch})
	}
}

func (s *Server) getBranch(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rp := s.repo(w, r)
	if rp == nil {
		return
	}
	sha, ok := rp.Branches[chi.URLParam(r, "branch")]
	if !ok {
		notFound(w)
		return
	}
	writeJSON(w, 200, map[string]any{"name": chi.URLParam(r, "branch"), "commit": map[string]any{"sha": sha}, "protected": rp.Protected[chi.URLParam(r, "branch")]})
}

func (s *Server) resolve(rp *Repo, ref string) string {
	if sha, ok := rp.Branches[ref]; ok {
		return sha
	}
	if _, ok := s.commits[ref]; ok {
		return ref
	}
	return ""
}

func (s *Server) compare(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rp := s.repo(w, r)
	if rp == nil {
		return
	}
	spec := chi.URLParam(r, "spec")
	parts := strings.SplitN(spec, "...", 2)
	if len(parts) != 2 {
		writeJSON(w, 422, map[string]any{"message": "bad compare spec"})
		return
	}
	base, head := s.resolve(rp, parts[0]), s.resolve(rp, parts[1])
	if base == "" || head == "" {
		notFound(w)
		return
	}
	a, b := s.commits[base].Files, s.commits[head].Files
	var files []map[string]any
	removed := map[string]string{}
	for p, c := range a {
		if _, ok := b[p]; !ok {
			removed[blobSHA(c)] = p
		}
	}
	paths := map[string]bool{}
	for p := range a {
		paths[p] = true
	}
	for p := range b {
		paths[p] = true
	}
	var sorted []string
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)
	renamedFrom := map[string]bool{}
	for _, p := range sorted {
		oc, inA := a[p]
		nc, inB := b[p]
		switch {
		case inA && inB && oc != nc:
			files = append(files, map[string]any{"filename": p, "status": "modified", "changes": 1})
		case !inA && inB:
			if old, ok := removed[blobSHA(nc)]; ok { // identical content moved: GitHub reports a rename
				files = append(files, map[string]any{"filename": p, "previous_filename": old, "status": "renamed", "changes": 0})
				renamedFrom[old] = true
			} else {
				files = append(files, map[string]any{"filename": p, "status": "added", "changes": 1})
			}
		}
	}
	for _, p := range sorted {
		if _, inB := b[p]; !inB && !renamedFrom[p] {
			if _, inA := a[p]; inA {
				files = append(files, map[string]any{"filename": p, "status": "removed", "changes": 1})
			}
		}
	}
	writeJSON(w, 200, map[string]any{"status": "ahead", "files": files, "base_commit": map[string]any{"sha": base}})
}

func (s *Server) contents(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rp := s.repo(w, r)
	if rp == nil {
		return
	}
	ref := r.URL.Query().Get("ref")
	if ref == "" {
		ref = rp.DefaultBranch
	}
	sha := s.resolve(rp, ref)
	p := chi.URLParam(r, "*")
	c, ok := s.commits[sha].Files[p]
	if sha == "" || !ok {
		notFound(w)
		return
	}
	writeJSON(w, 200, map[string]any{"type": "file", "path": p, "sha": blobSHA(c), "size": len(c), "encoding": "base64",
		"content": base64.StdEncoding.EncodeToString([]byte(c))})
}

func (s *Server) listCommits(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rp := s.repo(w, r)
	if rp == nil {
		return
	}
	path := r.URL.Query().Get("path")
	var out []map[string]any
	for sha := rp.Branches[rp.DefaultBranch]; sha != ""; sha = s.commits[sha].Parent {
		c := s.commits[sha]
		parent := s.commits[c.Parent]
		if path != "" && parent != nil && parent.Files[path] == c.Files[path] {
			continue
		}
		out = append(out, map[string]any{"sha": c.SHA, "html_url": "https://github.example/" + rp.FullName + "/commit/" + c.SHA,
			"commit": map[string]any{"message": c.Message, "author": map[string]any{"name": c.Author, "email": c.Email, "date": c.At.Format(time.RFC3339)}}})
	}
	writeJSON(w, 200, out)
}

func (s *Server) combinedStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.checks[chi.URLParam(r, "sha")]
	if st == "" {
		writeJSON(w, 200, map[string]any{"state": "pending", "total_count": 0, "statuses": []any{}})
		return
	}
	writeJSON(w, 200, map[string]any{"state": st, "total_count": 1, "statuses": []any{map[string]any{"state": st, "context": "ci"}}})
}

func (s *Server) checkRuns(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"total_count": 0, "check_runs": []any{}})
}

func (s *Server) isCollaborator(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rp := s.repo(w, r)
	if rp == nil {
		return
	}
	if rp.Collaborators[chi.URLParam(r, "user")] {
		w.WriteHeader(204)
		return
	}
	notFound(w)
}

func (s *Server) createHook(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	rp := s.repo(w, r)
	if rp == nil {
		return
	}
	body["id"] = len(rp.Hooks) + 1
	rp.Hooks = append(rp.Hooks, body)
	writeJSON(w, 201, body)
}

func (s *Server) editHook(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	rp := s.repo(w, r)
	if rp == nil {
		return
	}
	id, _ := strconv.Atoi(chi.URLParam(r, "hook"))
	if id < 1 || id > len(rp.Hooks) {
		notFound(w)
		return
	}
	body["id"] = id
	rp.Hooks[id-1] = body
	writeJSON(w, 200, body)
}

func (s *Server) listHooks(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rp := s.repo(w, r); rp != nil {
		writeJSON(w, 200, rp.Hooks)
	}
}

func refBranch(r *http.Request) string {
	return strings.TrimPrefix(strings.TrimPrefix(chi.URLParam(r, "*"), "refs/"), "heads/")
}

func (s *Server) getRef(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rp := s.repo(w, r)
	if rp == nil {
		return
	}
	b := refBranch(r)
	sha, ok := rp.Branches[b]
	if !ok {
		notFound(w)
		return
	}
	writeJSON(w, 200, map[string]any{"ref": "refs/heads/" + b, "object": map[string]any{"sha": sha, "type": "commit"}})
}

func (s *Server) createRef(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	rp := s.repo(w, r)
	if rp == nil {
		return
	}
	b := strings.TrimPrefix(body.Ref, "refs/heads/")
	if _, exists := rp.Branches[b]; exists {
		writeJSON(w, 422, map[string]any{"message": "Reference already exists"})
		return
	}
	if _, ok := s.commits[body.SHA]; !ok {
		writeJSON(w, 422, map[string]any{"message": "Object does not exist"})
		return
	}
	s.moveLocked(rp, b, body.SHA, s.BotLogin)
	writeJSON(w, 201, map[string]any{"ref": body.Ref, "object": map[string]any{"sha": body.SHA}})
}

func (s *Server) updateRef(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SHA   string `json:"sha"`
		Force bool   `json:"force"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	rp := s.repo(w, r)
	if rp == nil {
		return
	}
	b := refBranch(r)
	cur, ok := rp.Branches[b]
	if !ok {
		notFound(w)
		return
	}
	if rp.Protected[b] {
		writeJSON(w, 422, map[string]any{"message": "Protected branch update failed for refs/heads/" + b + "."})
		return
	}
	if !body.Force && s.commits[body.SHA] != nil && s.commits[body.SHA].Parent != cur {
		writeJSON(w, 422, map[string]any{"message": "Update is not a fast forward"})
		return
	}
	s.moveLocked(rp, b, body.SHA, s.BotLogin)
	writeJSON(w, 200, map[string]any{"ref": "refs/heads/" + b, "object": map[string]any{"sha": body.SHA}})
}

func (s *Server) deleteRef(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rp := s.repo(w, r)
	if rp == nil {
		return
	}
	delete(rp.Branches, refBranch(r))
	w.WriteHeader(204)
}

func (s *Server) getCommit(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.commits[chi.URLParam(r, "sha")]
	if c == nil {
		notFound(w)
		return
	}
	writeJSON(w, 200, map[string]any{"sha": c.SHA, "tree": map[string]any{"sha": c.SHA}, "message": c.Message,
		"parents": []any{map[string]any{"sha": c.Parent}}})
}

func (s *Server) getTree(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	files, ok := s.trees[chi.URLParam(r, "sha")]
	if !ok {
		notFound(w)
		return
	}
	var entries []map[string]any
	var paths []string
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		entries = append(entries, map[string]any{"path": p, "type": "blob", "mode": "100644", "sha": blobSHA(files[p]), "size": len(files[p])})
	}
	writeJSON(w, 200, map[string]any{"sha": chi.URLParam(r, "sha"), "tree": entries, "truncated": false})
}

func (s *Server) getBlob(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := chi.URLParam(r, "sha")
	for _, c := range s.commits {
		for _, content := range c.Files {
			if blobSHA(content) == want {
				writeJSON(w, 200, map[string]any{"sha": want, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(content))})
				return
			}
		}
	}
	notFound(w)
}

func (s *Server) createTree(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BaseTree string `json:"base_tree"`
		Tree     []struct {
			Path    string  `json:"path"`
			Content *string `json:"content"`
			SHA     *string `json:"sha"`
		} `json:"tree"`
	}
	raw, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(raw, &body)
	s.mu.Lock()
	defer s.mu.Unlock()
	files := copyFiles(s.trees[body.BaseTree])
	for _, e := range body.Tree {
		switch {
		case e.Content != nil:
			files[e.Path] = *e.Content
		case e.SHA == nil: // "sha": null deletes the path
			delete(files, e.Path)
		default:
			if c, ok := s.blobContent(*e.SHA); ok {
				files[e.Path] = c
			}
		}
	}
	sha := hash("tree", body.BaseTree, string(raw))
	s.trees[sha] = files
	writeJSON(w, 201, map[string]any{"sha": sha})
}

func (s *Server) blobContent(sha string) (string, bool) {
	for _, c := range s.commits {
		for _, content := range c.Files {
			if blobSHA(content) == sha {
				return content, true
			}
		}
	}
	return "", false
}

func (s *Server) createCommit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Message string   `json:"message"`
		Tree    string   `json:"tree"`
		Parents []string `json:"parents"`
		Author  struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"author"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	files, ok := s.trees[body.Tree]
	if !ok {
		writeJSON(w, 422, map[string]any{"message": "tree not found"})
		return
	}
	parent := ""
	if len(body.Parents) > 0 {
		parent = body.Parents[0]
	}
	author := body.Author.Name
	if author == "" {
		author = s.BotLogin
	}
	sha := s.commitLocked(parent, body.Message, author, body.Author.Email, copyFiles(files))
	writeJSON(w, 201, map[string]any{"sha": sha, "tree": map[string]any{"sha": body.Tree}})
}

func (s *Server) prJSON(rp *Repo, p *PR) map[string]any {
	headSHA := rp.Branches[p.Head]
	var mergeable any = true
	if s.commits[headSHA] != nil && s.commits[rp.Branches[p.Base]] != nil && !s.descendsFrom(headSHA, rp.Branches[p.Base]) {
		mergeable = s.cleanlyMergeable(rp, p)
	}
	state := p.State
	return map[string]any{"number": p.Number, "html_url": fmt.Sprintf("https://github.example/%s/pull/%d", rp.FullName, p.Number),
		"state": state, "merged": p.Merged, "mergeable": mergeable, "title": p.Title, "body": p.Body,
		"head": map[string]any{"ref": p.Head, "sha": headSHA}, "base": map[string]any{"ref": p.Base, "sha": rp.Branches[p.Base]},
		"created_at": p.CreatedAt.Format(time.RFC3339)}
}

func (s *Server) descendsFrom(sha, ancestor string) bool {
	for c := s.commits[sha]; c != nil; c = s.commits[c.Parent] {
		if c.SHA == ancestor {
			return true
		}
	}
	return false
}

// cleanlyMergeable: the PR is mergeable unless the base changed a path the PR also changed.
func (s *Server) cleanlyMergeable(rp *Repo, p *PR) bool {
	if s.ConflictOnUpdate {
		return false
	}
	return true
}

func (s *Server) createPR(w http.ResponseWriter, r *http.Request) {
	var body struct{ Title, Head, Base, Body string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	rp := s.repo(w, r)
	if rp == nil {
		return
	}
	if _, ok := rp.Branches[body.Head]; !ok {
		writeJSON(w, 422, map[string]any{"message": "Validation Failed: head"})
		return
	}
	p := &PR{Number: rp.nextPR, Head: body.Head, Base: body.Base, Title: body.Title, Body: body.Body, State: "open", CreatedAt: s.now()}
	rp.PRs[p.Number] = p
	rp.nextPR++
	writeJSON(w, 201, s.prJSON(rp, p))
}

func (s *Server) pr(w http.ResponseWriter, r *http.Request) (*Repo, *PR) {
	rp := s.repo(w, r)
	if rp == nil {
		return nil, nil
	}
	n, _ := strconv.Atoi(chi.URLParam(r, "n"))
	p := rp.PRs[n]
	if p == nil {
		notFound(w)
		return nil, nil
	}
	return rp, p
}

func (s *Server) listPRs(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rp := s.repo(w, r)
	if rp == nil {
		return
	}
	var out []map[string]any
	for _, p := range rp.PRs {
		out = append(out, s.prJSON(rp, p))
	}
	writeJSON(w, 200, out)
}

func (s *Server) getPR(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rp, p := s.pr(w, r); p != nil {
		writeJSON(w, 200, s.prJSON(rp, p))
	}
}

func (s *Server) editPR(w http.ResponseWriter, r *http.Request) {
	var body struct {
		State string `json:"state"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	if rp, p := s.pr(w, r); p != nil {
		if body.State != "" {
			p.State = body.State
		}
		writeJSON(w, 200, s.prJSON(rp, p))
	}
}

func (s *Server) mergePR(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SHA string `json:"sha"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	rp, p := s.pr(w, r)
	if p == nil {
		return
	}
	head := rp.Branches[p.Head]
	if body.SHA != "" && body.SHA != head {
		writeJSON(w, 409, map[string]any{"message": "Head branch was modified"})
		return
	}
	if rp.Protected[p.Base] && (!p.Approved || s.checks[head] == "failure") {
		writeJSON(w, 405, map[string]any{"message": "Pull Request is not mergeable: approval or checks required"})
		return
	}
	// Squash: apply the PR's changes (head vs its merge base) onto the current base.
	base := rp.Branches[p.Base]
	files := copyFiles(s.commits[base].Files)
	mb := s.mergeBase(head, base)
	for path, c := range s.commits[head].Files {
		if s.commits[mb].Files[path] != c {
			files[path] = c
		}
	}
	for path := range s.commits[mb].Files {
		if _, ok := s.commits[head].Files[path]; !ok {
			delete(files, path)
		}
	}
	sha := s.commitLocked(base, p.Title, s.BotLogin, "", files)
	s.moveLocked(rp, p.Base, sha, s.BotLogin)
	p.Merged, p.State = true, "closed"
	writeJSON(w, 200, map[string]any{"merged": true, "sha": sha})
}

func (s *Server) mergeBase(a, b string) string {
	seen := map[string]bool{}
	for c := s.commits[b]; c != nil; c = s.commits[c.Parent] {
		seen[c.SHA] = true
	}
	for c := s.commits[a]; c != nil; c = s.commits[c.Parent] {
		if seen[c.SHA] {
			return c.SHA
		}
	}
	return b
}

func (s *Server) updateBranch(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rp, p := s.pr(w, r)
	if p == nil {
		return
	}
	if s.ConflictOnUpdate {
		writeJSON(w, 422, map[string]any{"message": "merge conflict between base and head"})
		return
	}
	head, base := rp.Branches[p.Head], rp.Branches[p.Base]
	files := copyFiles(s.commits[base].Files)
	mb := s.mergeBase(head, base)
	for path, c := range s.commits[head].Files {
		if s.commits[mb].Files[path] != c {
			files[path] = c
		}
	}
	sha := s.commitLocked(base, "Merge base into "+p.Head, s.BotLogin, "", files)
	s.moveLocked(rp, p.Head, sha, s.BotLogin)
	writeJSON(w, 202, map[string]any{"message": "Updating pull request branch.", "url": ""})
}

func (s *Server) reviews(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, p := s.pr(w, r)
	if p == nil {
		return
	}
	if p.Approved {
		writeJSON(w, 200, []any{map[string]any{"state": "APPROVED", "user": map[string]any{"login": "approver"}, "submitted_at": s.now().Format(time.RFC3339)}})
		return
	}
	writeJSON(w, 200, []any{})
}

func (s *Server) requestReviewers(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reviewers     []string `json:"reviewers"`
		TeamReviewers []string `json:"team_reviewers"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	rp, p := s.pr(w, r)
	if p == nil {
		return
	}
	p.Reviewers = append(p.Reviewers, body.Reviewers...)
	p.Teams = append(p.Teams, body.TeamReviewers...)
	writeJSON(w, 201, s.prJSON(rp, p))
}

func (s *Server) comment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Body string `json:"body"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, p := s.pr(w, r); p != nil {
		p.Comments = append(p.Comments, body.Body)
		writeJSON(w, 201, map[string]any{"body": body.Body})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func notFound(w http.ResponseWriter) { writeJSON(w, 404, map[string]any{"message": "Not Found"}) }

// --- webhook delivery ---

// Delivery is one attempted webhook delivery.
type Delivery struct {
	Repo, URL, After, Pusher string
	Status                   int // 0 when the request failed
}

type delivery struct {
	repo, url, secret, after, pusher string
	body                             []byte
}

// Deliveries returns the webhook delivery log.
func (s *Server) Deliveries() []Delivery {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Delivery(nil), s.deliveryLog...)
}

// moveLocked moves a branch head and queues a signed push event to every hook subscribed to "push",
// like GitHub does for any ref update (pushes, API commits, merges). The caller holds s.mu.
func (s *Server) moveLocked(rp *Repo, branch, sha, pusher string) {
	before := rp.Branches[branch]
	rp.Branches[branch] = sha
	if len(rp.Hooks) == 0 {
		return
	}
	if before == "" {
		before = strings.Repeat("0", 40)
	}
	var commits []map[string]any
	for c := s.commits[sha]; c != nil && c.SHA != before && len(commits) < 20; c = s.commits[c.Parent] {
		var added, modified, removed []string
		var parent map[string]string
		if p := s.commits[c.Parent]; p != nil {
			parent = p.Files
		}
		for path, content := range c.Files {
			old, ok := parent[path]
			switch {
			case !ok:
				added = append(added, path)
			case old != content:
				modified = append(modified, path)
			}
		}
		for path := range parent {
			if _, ok := c.Files[path]; !ok {
				removed = append(removed, path)
			}
		}
		sort.Strings(added)
		sort.Strings(modified)
		sort.Strings(removed)
		commits = append([]map[string]any{{"id": c.SHA, "message": c.Message, "added": nonNil(added), "modified": nonNil(modified),
			"removed": nonNil(removed), "author": map[string]any{"name": c.Author, "email": c.Email, "username": c.Author},
			"committer": map[string]any{"name": c.Author, "email": c.Email, "username": c.Author}}}, commits...)
	}
	payload := map[string]any{"ref": "refs/heads/" + branch, "before": before, "after": sha, "created": before == strings.Repeat("0", 40),
		"repository": map[string]any{"full_name": rp.FullName, "name": rp.FullName[strings.Index(rp.FullName, "/")+1:], "default_branch": rp.DefaultBranch},
		"pusher":     map[string]any{"name": pusher}, "sender": map[string]any{"login": pusher}, "commits": commits}
	body, _ := json.Marshal(payload)
	for _, h := range rp.Hooks {
		cfg, _ := h["config"].(map[string]any)
		url, _ := cfg["url"].(string)
		secret, _ := cfg["secret"].(string)
		if url == "" || !subscribed(h, "push") {
			continue
		}
		select {
		case s.deliveries <- delivery{repo: rp.FullName, url: url, secret: secret, after: sha, pusher: pusher, body: body}:
		default: // a stuck receiver must not block the mock
		}
	}
}

func subscribed(h map[string]any, event string) bool {
	evs, ok := h["events"].([]any)
	if !ok {
		return true
	}
	for _, e := range evs {
		if e == event {
			return true
		}
	}
	return false
}

func nonNil(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func (s *Server) deliverLoop() {
	c := &http.Client{Timeout: 10 * time.Second}
	n := 0
	for d := range s.deliveries {
		n++
		mac := hmac.New(sha256.New, []byte(d.secret))
		mac.Write(d.body)
		req, _ := http.NewRequest(http.MethodPost, d.url, bytes.NewReader(d.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitHub-Event", "push")
		req.Header.Set("X-GitHub-Delivery", fmt.Sprintf("mock-%d-%s", n, d.after[:8]))
		req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		status := 0
		if resp, err := c.Do(req); err == nil {
			status = resp.StatusCode
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		s.mu.Lock()
		s.deliveryLog = append(s.deliveryLog, Delivery{Repo: d.repo, URL: d.url, After: d.after, Pusher: d.pusher, Status: status})
		s.mu.Unlock()
	}
}
