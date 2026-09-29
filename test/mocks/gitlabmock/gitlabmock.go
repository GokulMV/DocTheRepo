// Package gitlabmock is an in-memory GitLab REST API (v4) sufficient for the Hub's GitLab adapter and for
// pipeline/E2E tests: projects with branches, commits, trees, raw files, compare, the Commits API with
// create/update/delete actions, merge requests with pipelines/approvals/merge/rebase, protected branches,
// users, groups, and project hooks. Project and file IDs arrive URL-encoded ("acme%2Fshop"), as with GitLab.
package gitlabmock

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// MR is a merge request.
type MR struct {
	IID          int
	Source       string
	Target       string
	Title        string
	Description  string
	State        string // opened | closed | merged
	ReviewerIDs  []int
	ApprovedBy   []string
	Notes        []string
	CreatedAt    time.Time
	rebasePolls  int
	MergeError   string
	RemoveSource bool
}

// Project is one repository.
type Project struct {
	Path          string
	DefaultBranch string
	Branches      map[string]string
	Protected     map[string]bool
	MRs           map[int]*MR
	nextIID       int
	Hooks         []map[string]any
	nextHook      int
}

// Server is a running mock.
type Server struct {
	*httptest.Server
	mu        sync.Mutex
	projects  map[string]*Project
	order     []string
	commits   map[string]*Commit
	pipelines map[string]string // sha → success|failed|running|...
	users     map[string]int    // username → id
	groups    map[string][]string
	// BotUsername is what GET /user reports.
	BotUsername string
	// ConflictOnRebase makes rebases finish with a merge_error (simulates an unresolvable rebase).
	ConflictOnRebase bool
	// RebasePolls is how many GETs report rebase_in_progress before a rebase completes.
	RebasePolls int
	now         func() time.Time
}

// New starts an empty mock.
func New() *Server {
	s := &Server{projects: map[string]*Project{}, commits: map[string]*Commit{}, pipelines: map[string]string{},
		users: map[string]int{}, groups: map[string][]string{}, BotUsername: "project_1_bot", RebasePolls: 1, now: time.Now}
	s.users[s.BotUsername] = 1
	r := chi.NewRouter()
	r.Route("/api/v4", func(r chi.Router) { s.routes(r) })
	s.Server = httptest.NewServer(r)
	return s
}

// BaseURL is the instance URL for the adapter's BaseURL (client-go appends api/v4).
func (s *Server) BaseURL() string { return s.URL + "/api/v4" }

// --- test helpers ---

// CreateProject creates a project with an initial commit on its default branch.
func (s *Server) CreateProject(path, defaultBranch string, files map[string]string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projects[path] = &Project{Path: path, DefaultBranch: defaultBranch, Branches: map[string]string{}, Protected: map[string]bool{},
		MRs: map[int]*MR{}, nextIID: 1, nextHook: 1}
	s.order = append(s.order, path)
	sha := s.commitLocked("", "initial", "dev", "dev@example.com", files)
	s.projects[path].Branches[defaultBranch] = sha
	return sha
}

// Push commits changes (nil deletes a path) on top of branch and returns the new SHA.
func (s *Server) Push(path, branch string, changes map[string]*string, author string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.projects[path]
	parent := p.Branches[branch]
	files := copyFiles(s.commits[parent].Files)
	for k, v := range changes {
		if v == nil {
			delete(files, k)
		} else {
			files[k] = *v
		}
	}
	sha := s.commitLocked(parent, "push by "+author, author, author+"@example.com", files)
	p.Branches[branch] = sha
	return sha
}

// Protect makes a branch reject direct pushes and require an approval to merge.
func (s *Server) Protect(path, branch string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projects[path].Protected[branch] = true
}

// Approve records an approval on an MR.
func (s *Server) Approve(path string, iid int, user string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mr := s.projects[path].MRs[iid]
	mr.ApprovedBy = append(mr.ApprovedBy, user)
}

// SetPipeline sets the pipeline status for a commit.
func (s *Server) SetPipeline(sha, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pipelines[sha] = status
}

// AddUser registers a user; AddGroup registers a group with members.
func (s *Server) AddUser(username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addUserLocked(username)
}

func (s *Server) addUserLocked(username string) int {
	if id, ok := s.users[username]; ok {
		return id
	}
	id := len(s.users) + 1
	s.users[username] = id
	return id
}

// AddGroup registers a group ("acme/docs") with members.
func (s *Server) AddGroup(path string, members ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range members {
		s.addUserLocked(m)
	}
	s.groups[path] = members
}

// File reads a file at a branch head.
func (s *Server) File(path, branch, file string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.commits[s.projects[path].Branches[branch]].Files[file]
	return v, ok
}

// Head returns a branch head SHA ("" when the branch does not exist).
func (s *Server) Head(path, branch string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.projects[path].Branches[branch]
}

// MRs returns a project's merge requests by IID.
func (s *Server) MRs(path string) []MR {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []MR
	for _, m := range s.projects[path].MRs {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IID < out[j].IID })
	return out
}

// CommitMeta returns a commit's author and message.
func (s *Server) CommitMeta(sha string) (author, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.commits[sha]
	return c.Author, c.Message
}

// Hooks returns registered project hooks.
func (s *Server) Hooks(path string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.projects[path].Hooks...)
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

func (s *Server) commitLocked(parent, msg, author, email string, files map[string]string) string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{parent, msg, author, strconv.FormatInt(s.now().UnixNano(), 10)}
	for _, k := range keys {
		parts = append(parts, k, hash("blob", files[k]))
	}
	sha := hash(parts...)
	s.commits[sha] = &Commit{SHA: sha, Parent: parent, Message: msg, Author: author, Email: email, At: s.now(), Files: files}
	return sha
}

// lastCommitFor returns the most recent commit reachable from head that changed path.
func (s *Server) lastCommitFor(head, path string) string {
	for c := s.commits[head]; c != nil; c = s.commits[c.Parent] {
		prev := s.commits[c.Parent]
		if prev == nil || prev.Files[path] != c.Files[path] {
			return c.SHA
		}
	}
	return ""
}

// --- routing ---

func (s *Server) routes(r chi.Router) {
	r.Get("/user", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]any{"id": s.users[s.BotUsername], "username": s.BotUsername, "name": "DocTheRepo Hub", "email": s.BotUsername + "@noreply.gitlab.example.com"})
	})
	r.Get("/users", s.listUsers)
	r.Get("/projects", s.listProjects)
	r.Get("/groups/{gid}/projects", s.listProjects)
	r.Get("/groups/{gid}/members", s.groupMembers)
	r.Route("/projects/{id}", func(r chi.Router) {
		r.Get("/", s.getProject)
		r.Get("/hooks", s.listHooks)
		r.Post("/hooks", s.addHook)
		r.Put("/hooks/{hid}", s.editHook)
		r.Get("/repository/branches/{branch}", s.getBranch)
		r.Post("/repository/branches", s.createBranch)
		r.Delete("/repository/branches/{branch}", s.deleteBranch)
		r.Get("/repository/compare", s.compare)
		r.Get("/repository/files/{file}/raw", s.rawFile)
		r.Head("/repository/files/{file}", s.fileMeta)
		r.Get("/repository/tree", s.tree)
		r.Get("/repository/commits", s.listCommits)
		r.Post("/repository/commits", s.createCommit)
		r.Post("/merge_requests", s.createMR)
		r.Get("/merge_requests/{iid}", s.getMR)
		r.Put("/merge_requests/{iid}", s.updateMR)
		r.Get("/merge_requests/{iid}/approvals", s.approvals)
		r.Put("/merge_requests/{iid}/merge", s.mergeMR)
		r.Put("/merge_requests/{iid}/rebase", s.rebaseMR)
		r.Post("/merge_requests/{iid}/notes", s.addNote)
	})
}

func param(r *http.Request, name string) string {
	v, err := url.PathUnescape(chi.URLParam(r, name))
	if err != nil {
		return chi.URLParam(r, name)
	}
	return v
}

func (s *Server) project(w http.ResponseWriter, r *http.Request) *Project {
	p := s.projects[param(r, "id")]
	if p == nil {
		notFound(w, "Project")
	}
	return p
}

func (s *Server) resolve(p *Project, ref string) string {
	if sha, ok := p.Branches[ref]; ok {
		return sha
	}
	if _, ok := s.commits[ref]; ok {
		return ref
	}
	return ""
}

// paginate writes items[page] with GitLab's X-Next-Page header.
func paginate[T any](w http.ResponseWriter, r *http.Request, items []T) {
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if per <= 0 {
		per = 20
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}
	start := min((page-1)*per, len(items))
	end := min(start+per, len(items))
	if end < len(items) {
		w.Header().Set("X-Next-Page", strconv.Itoa(page+1))
	}
	out := items[start:end]
	if out == nil {
		out = []T{}
	}
	writeJSON(w, 200, out)
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	group := param(r, "gid")
	var list []map[string]any
	for _, path := range s.order {
		if group != "" && !strings.HasPrefix(path, group+"/") {
			continue
		}
		list = append(list, map[string]any{"path_with_namespace": path, "default_branch": s.projects[path].DefaultBranch})
	}
	paginate(w, r, list)
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := r.URL.Query().Get("username")
	list := []map[string]any{}
	if id, ok := s.users[name]; ok {
		list = append(list, map[string]any{"id": id, "username": name})
	}
	writeJSON(w, 200, list)
}

func (s *Server) groupMembers(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	members, ok := s.groups[param(r, "gid")]
	if !ok {
		notFound(w, "Group")
		return
	}
	list := []map[string]any{}
	for _, m := range members {
		list = append(list, map[string]any{"id": s.users[m], "username": m})
	}
	writeJSON(w, 200, list)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.project(w, r); p != nil {
		writeJSON(w, 200, map[string]any{"path_with_namespace": p.Path, "default_branch": p.DefaultBranch})
	}
}

type hookBody struct {
	URL                 string `json:"url"`
	Token               string `json:"token"`
	PushEvents          bool   `json:"push_events"`
	MergeRequestsEvents bool   `json:"merge_requests_events"`
}

func (s *Server) listHooks(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.project(w, r); p != nil {
		writeJSON(w, 200, append([]map[string]any{}, p.Hooks...))
	}
}

func (s *Server) addHook(w http.ResponseWriter, r *http.Request) {
	var b hookBody
	_ = json.NewDecoder(r.Body).Decode(&b)
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.project(w, r)
	if p == nil {
		return
	}
	h := map[string]any{"id": p.nextHook, "url": b.URL, "token": b.Token, "push_events": b.PushEvents, "merge_requests_events": b.MergeRequestsEvents}
	p.nextHook++
	p.Hooks = append(p.Hooks, h)
	writeJSON(w, 201, h)
}

func (s *Server) editHook(w http.ResponseWriter, r *http.Request) {
	var b hookBody
	_ = json.NewDecoder(r.Body).Decode(&b)
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.project(w, r)
	if p == nil {
		return
	}
	id, _ := strconv.Atoi(chi.URLParam(r, "hid"))
	for _, h := range p.Hooks {
		if h["id"] == id {
			h["url"], h["token"], h["push_events"], h["merge_requests_events"] = b.URL, b.Token, b.PushEvents, b.MergeRequestsEvents
			writeJSON(w, 200, h)
			return
		}
	}
	notFound(w, "Hook")
}

func (s *Server) getBranch(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.project(w, r)
	if p == nil {
		return
	}
	name := param(r, "branch")
	sha, ok := p.Branches[name]
	if !ok {
		notFound(w, "Branch")
		return
	}
	writeJSON(w, 200, map[string]any{"name": name, "protected": p.Protected[name], "commit": map[string]any{"id": sha}})
}

func (s *Server) createBranch(w http.ResponseWriter, r *http.Request) {
	var b struct{ Branch, Ref string }
	_ = json.NewDecoder(r.Body).Decode(&b)
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.project(w, r)
	if p == nil {
		return
	}
	if _, exists := p.Branches[b.Branch]; exists {
		writeJSON(w, 400, map[string]any{"message": "Branch already exists"})
		return
	}
	sha := s.resolve(p, b.Ref)
	if sha == "" {
		writeJSON(w, 400, map[string]any{"message": "Invalid reference name"})
		return
	}
	p.Branches[b.Branch] = sha
	writeJSON(w, 201, map[string]any{"name": b.Branch, "commit": map[string]any{"id": sha}})
}

func (s *Server) deleteBranch(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.project(w, r)
	if p == nil {
		return
	}
	name := param(r, "branch")
	if _, ok := p.Branches[name]; !ok {
		notFound(w, "Branch")
		return
	}
	delete(p.Branches, name)
	w.WriteHeader(204)
}

func (s *Server) compare(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.project(w, r)
	if p == nil {
		return
	}
	from, to := s.resolve(p, r.URL.Query().Get("from")), s.resolve(p, r.URL.Query().Get("to"))
	if from == "" || to == "" {
		notFound(w, "Ref")
		return
	}
	a, b := s.commits[from].Files, s.commits[to].Files
	removedByContent := map[string]string{}
	for path, c := range a {
		if _, ok := b[path]; !ok {
			removedByContent[c] = path
		}
	}
	all := map[string]bool{}
	for k := range a {
		all[k] = true
	}
	for k := range b {
		all[k] = true
	}
	paths := make([]string, 0, len(all))
	for k := range all {
		paths = append(paths, k)
	}
	sort.Strings(paths)
	renamed := map[string]bool{}
	diffs := []map[string]any{}
	for _, path := range paths {
		oc, inA := a[path]
		nc, inB := b[path]
		switch {
		case inA && inB && oc != nc:
			diffs = append(diffs, map[string]any{"old_path": path, "new_path": path, "diff": "@@ -1 +1 @@\n"})
		case !inA && inB:
			if old, ok := removedByContent[nc]; ok {
				renamed[old] = true
				diffs = append(diffs, map[string]any{"old_path": old, "new_path": path, "renamed_file": true, "diff": ""})
			} else {
				diffs = append(diffs, map[string]any{"old_path": path, "new_path": path, "new_file": true, "diff": "@@ +1 @@\n"})
			}
		}
	}
	for _, path := range paths {
		if _, inB := b[path]; !inB && !renamed[path] {
			diffs = append(diffs, map[string]any{"old_path": path, "new_path": path, "deleted_file": true, "diff": "@@ -1 @@\n"})
		}
	}
	writeJSON(w, 200, map[string]any{"commit": map[string]any{"id": to}, "diffs": diffs, "compare_timeout": false})
}

func (s *Server) fileAt(w http.ResponseWriter, r *http.Request) (*Project, string, string, bool) {
	p := s.project(w, r)
	if p == nil {
		return nil, "", "", false
	}
	ref := s.resolve(p, r.URL.Query().Get("ref"))
	file := param(r, "file")
	if ref == "" {
		notFound(w, "File")
		return nil, "", "", false
	}
	c, ok := s.commits[ref].Files[file]
	if !ok {
		notFound(w, "File")
		return nil, "", "", false
	}
	return p, ref, c, true
}

func (s *Server) rawFile(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, _, content, ok := s.fileAt(w, r); ok {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, content)
	}
}

func (s *Server) fileMeta(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ref, content, ok := s.fileAt(w, r)
	if !ok {
		return
	}
	file := param(r, "file")
	w.Header().Set("X-Gitlab-File-Path", file)
	w.Header().Set("X-Gitlab-Commit-Id", ref)
	w.Header().Set("X-Gitlab-Blob-Id", hash("blob", content))
	w.Header().Set("X-Gitlab-Last-Commit-Id", s.lastCommitFor(ref, file))
	w.Header().Set("X-Gitlab-Size", strconv.Itoa(len(content)))
	w.WriteHeader(200)
}

func (s *Server) tree(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.project(w, r)
	if p == nil {
		return
	}
	ref := s.resolve(p, r.URL.Query().Get("ref"))
	if ref == "" {
		notFound(w, "Tree")
		return
	}
	var paths []string
	dirs := map[string]bool{}
	for f := range s.commits[ref].Files {
		paths = append(paths, f)
		for d := f; strings.Contains(d, "/"); {
			d = d[:strings.LastIndex(d, "/")]
			dirs[d] = true
		}
	}
	for d := range dirs {
		paths = append(paths, d+"/")
	}
	sort.Strings(paths)
	nodes := make([]map[string]any, 0, len(paths))
	for _, pth := range paths {
		if strings.HasSuffix(pth, "/") {
			nodes = append(nodes, map[string]any{"type": "tree", "path": strings.TrimSuffix(pth, "/")})
		} else {
			nodes = append(nodes, map[string]any{"type": "blob", "path": pth})
		}
	}
	paginate(w, r, nodes)
}

func (s *Server) listCommits(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.project(w, r)
	if p == nil {
		return
	}
	path := r.URL.Query().Get("path")
	var since time.Time
	if v := r.URL.Query().Get("since"); v != "" {
		since, _ = time.Parse(time.RFC3339, v)
	}
	list := []map[string]any{}
	for c := s.commits[p.Branches[p.DefaultBranch]]; c != nil; c = s.commits[c.Parent] {
		prev := s.commits[c.Parent]
		touched := path == "" || prev == nil || prev.Files[path] != c.Files[path]
		if touched && !c.At.Before(since) {
			list = append(list, map[string]any{"id": c.SHA, "author_name": c.Author, "author_email": c.Email, "message": c.Message,
				"authored_date": c.At.Format(time.RFC3339Nano), "web_url": s.URL + "/" + p.Path + "/-/commit/" + c.SHA})
		}
	}
	paginate(w, r, list)
}

func (s *Server) createCommit(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Branch        string `json:"branch"`
		CommitMessage string `json:"commit_message"`
		AuthorName    string `json:"author_name"`
		AuthorEmail   string `json:"author_email"`
		Actions       []struct {
			Action       string `json:"action"`
			FilePath     string `json:"file_path"`
			Content      string `json:"content"`
			LastCommitID string `json:"last_commit_id"`
		} `json:"actions"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.project(w, r)
	if p == nil {
		return
	}
	head, ok := p.Branches[b.Branch]
	if !ok {
		writeJSON(w, 400, map[string]any{"message": "You can only create or edit files when you are on a branch"})
		return
	}
	if p.Protected[b.Branch] {
		writeJSON(w, 403, map[string]any{"message": "You are not allowed to push into this branch"})
		return
	}
	files := copyFiles(s.commits[head].Files)
	for _, a := range b.Actions {
		_, exists := files[a.FilePath]
		if a.LastCommitID != "" && exists && s.lastCommitFor(head, a.FilePath) != a.LastCommitID {
			writeJSON(w, 400, map[string]any{"message": "A file with this name has been modified since you started editing it"})
			return
		}
		switch a.Action {
		case "create":
			if exists {
				writeJSON(w, 400, map[string]any{"message": "A file with this name already exists"})
				return
			}
			files[a.FilePath] = a.Content
		case "update":
			if !exists {
				writeJSON(w, 400, map[string]any{"message": "A file with this name doesn't exist"})
				return
			}
			files[a.FilePath] = a.Content
		case "delete":
			delete(files, a.FilePath)
		default:
			writeJSON(w, 400, map[string]any{"message": "unknown action " + a.Action})
			return
		}
	}
	author := b.AuthorName
	if author == "" {
		author = s.BotUsername
	}
	sha := s.commitLocked(head, b.CommitMessage, author, b.AuthorEmail, files)
	p.Branches[b.Branch] = sha
	writeJSON(w, 201, map[string]any{"id": sha, "message": b.CommitMessage, "author_name": author})
}

func (s *Server) mr(w http.ResponseWriter, r *http.Request) (*Project, *MR) {
	p := s.project(w, r)
	if p == nil {
		return nil, nil
	}
	iid, _ := strconv.Atoi(chi.URLParam(r, "iid"))
	m := p.MRs[iid]
	if m == nil {
		notFound(w, "Merge Request")
		return nil, nil
	}
	return p, m
}

func (s *Server) mrJSON(p *Project, m *MR, pollRebase bool) map[string]any {
	head := p.Branches[m.Source]
	status := "mergeable"
	switch {
	case m.MergeError != "":
		status = "conflict"
	case p.Protected[m.Target] && len(m.ApprovedBy) == 0:
		status = "not_approved"
	}
	out := map[string]any{"iid": m.IID, "web_url": s.URL + "/" + p.Path + "/-/merge_requests/" + strconv.Itoa(m.IID),
		"state": m.State, "source_branch": m.Source, "target_branch": m.Target, "sha": head, "title": m.Title,
		"detailed_merge_status": status, "has_conflicts": m.MergeError != "", "merge_error": m.MergeError,
		"created_at": m.CreatedAt.Format(time.RFC3339Nano)}
	if st, ok := s.pipelines[head]; ok && head != "" {
		out["head_pipeline"] = map[string]any{"id": 1, "sha": head, "status": st}
	}
	if pollRebase && m.rebasePolls > 0 {
		m.rebasePolls--
		out["rebase_in_progress"] = true
	}
	return out
}

func (s *Server) createMR(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Title        string `json:"title"`
		Description  string `json:"description"`
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
		ReviewerIDs  []int  `json:"reviewer_ids"`
		RemoveSource bool   `json:"remove_source_branch"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.project(w, r)
	if p == nil {
		return
	}
	if _, ok := p.Branches[b.SourceBranch]; !ok {
		writeJSON(w, 422, map[string]any{"message": []string{"Source branch does not exist"}})
		return
	}
	m := &MR{IID: p.nextIID, Source: b.SourceBranch, Target: b.TargetBranch, Title: b.Title, Description: b.Description,
		State: "opened", ReviewerIDs: b.ReviewerIDs, RemoveSource: b.RemoveSource, CreatedAt: s.now()}
	p.MRs[m.IID] = m
	p.nextIID++
	writeJSON(w, 201, s.mrJSON(p, m, false))
}

func (s *Server) getMR(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, m := s.mr(w, r); m != nil {
		writeJSON(w, 200, s.mrJSON(p, m, r.URL.Query().Get("include_rebase_in_progress") == "true"))
	}
}

func (s *Server) updateMR(w http.ResponseWriter, r *http.Request) {
	var b struct {
		StateEvent string `json:"state_event"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	s.mu.Lock()
	defer s.mu.Unlock()
	p, m := s.mr(w, r)
	if m == nil {
		return
	}
	switch b.StateEvent {
	case "close":
		m.State = "closed"
	case "reopen":
		m.State = "opened"
	}
	writeJSON(w, 200, s.mrJSON(p, m, false))
}

func (s *Server) approvals(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, m := s.mr(w, r)
	if m == nil {
		return
	}
	by := []map[string]any{}
	for _, u := range m.ApprovedBy {
		by = append(by, map[string]any{"user": map[string]any{"username": u, "id": s.users[u]}})
	}
	writeJSON(w, 200, map[string]any{"iid": m.IID, "approved": len(m.ApprovedBy) > 0, "approvals_left": 0, "approved_by": by})
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

// applyOnto replays head's changes since its merge base with base onto base's tree.
func (s *Server) applyOnto(head, base string) map[string]string {
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
	return files
}

func (s *Server) mergeMR(w http.ResponseWriter, r *http.Request) {
	var b struct {
		SHA                      string `json:"sha"`
		ShouldRemoveSourceBranch bool   `json:"should_remove_source_branch"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	s.mu.Lock()
	defer s.mu.Unlock()
	p, m := s.mr(w, r)
	if m == nil {
		return
	}
	head := p.Branches[m.Source]
	if m.State != "opened" {
		writeJSON(w, 405, map[string]any{"message": "Method Not Allowed"})
		return
	}
	if b.SHA != "" && b.SHA != head {
		writeJSON(w, 409, map[string]any{"message": "SHA does not match HEAD of source branch"})
		return
	}
	if m.MergeError != "" || (p.Protected[m.Target] && (len(m.ApprovedBy) == 0 || s.pipelines[head] == "failed")) {
		writeJSON(w, 405, map[string]any{"message": "Method Not Allowed"})
		return
	}
	base := p.Branches[m.Target]
	sha := s.commitLocked(base, m.Title, s.BotUsername, "", s.applyOnto(head, base))
	p.Branches[m.Target] = sha
	m.State = "merged"
	if b.ShouldRemoveSourceBranch {
		delete(p.Branches, m.Source)
	}
	writeJSON(w, 200, s.mrJSON(p, m, false))
}

func (s *Server) rebaseMR(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, m := s.mr(w, r)
	if m == nil {
		return
	}
	m.rebasePolls = s.RebasePolls
	if s.ConflictOnRebase {
		m.MergeError = "Rebase failed: Rebase locally, resolve all conflicts, then push the branch."
	} else {
		m.MergeError = ""
		head, base := p.Branches[m.Source], p.Branches[m.Target]
		p.Branches[m.Source] = s.commitLocked(base, "Rebase "+m.Source, s.BotUsername, "", s.applyOnto(head, base))
	}
	writeJSON(w, 202, map[string]any{"rebase_in_progress": true})
}

func (s *Server) addNote(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Body string `json:"body"`
	}
	_ = json.NewDecoder(r.Body).Decode(&b)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, m := s.mr(w, r)
	if m == nil {
		return
	}
	m.Notes = append(m.Notes, b.Body)
	writeJSON(w, 201, map[string]any{"id": len(m.Notes), "body": b.Body})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func notFound(w http.ResponseWriter, what string) {
	writeJSON(w, 404, map[string]any{"message": "404 " + what + " Not Found"})
}

// SetConflict marks an MR as conflicting with its target (as GitLab does after a failed rebase).
func (s *Server) SetConflict(path string, iid int, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projects[path].MRs[iid].MergeError = msg
}
