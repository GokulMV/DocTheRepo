// Package ingest turns git-host activity into jobs: verified webhooks (plan § 7.1) and polling for hosts
// the Hub cannot receive webhooks from (§ 8.16). Both produce the identical code_push job, deduplicated
// by repo + head SHA, so a webhook and a poll for the same push collapse into one job. Ingress performs no
// LLM or git I/O: verification uses the connector's stored secret, and review events become jobs.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/core/pipeline"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Enqueuer inserts jobs.
type Enqueuer interface {
	Enqueue(ctx context.Context, nj ports.NewJob) (ports.Job, bool, error)
}

// Repos finds tracked repositories.
type Repos interface {
	Get(ctx context.Context, id string) (ports.RepoConfig, error)
	ByName(ctx context.Context, connectorID, fullName string) (ports.RepoConfig, error)
	ListEnabled(ctx context.Context) ([]ports.RepoConfig, error)
}

// Hosts returns a git connector's adapter and config (cached; no network I/O to build).
type Hosts interface {
	HostAndConfig(ctx context.Context, connectorID string) (ports.CodeHost, ports.ConnectorConfig, error)
}

// Service is the ingest entry point.
type Service struct {
	Queue Enqueuer
	Repos Repos
	Hosts Hosts
	Log   *slog.Logger
	Now   func() time.Time

	mu       sync.Mutex
	lastPoll map[string]time.Time
}

// Result is the webhook response (plan § 7.1).
type Result struct {
	Status   int    `json:"-"`
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
	JobID    string `json:"job_id,omitempty"`
}

// Webhook rejection reasons (200, accepted:false).
const (
	ReasonBotLoop         = "bot_loop_guard"
	ReasonIgnoredPath     = "ignored_path"
	ReasonUntrackedRepo   = "untracked_repo"
	ReasonUntrackedBranch = "untracked_branch"
	ReasonIgnoredEvent    = "ignored_event"
)

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// GitWebhook verifies and handles a GitHub/GitLab webhook. Errors: ports.ErrNotFound (unknown connector
// or wrong kind), ports.ErrInvalidSignature, *ports.ValidationError (malformed payload).
func (s *Service) GitWebhook(ctx context.Context, kind, connectorID string, hdr http.Header, body []byte) (Result, error) {
	host, cc, err := s.Hosts.HostAndConfig(ctx, connectorID)
	if err != nil {
		return Result{}, err
	}
	if cc.Type != kind {
		return Result{}, fmt.Errorf("connector %s is %s, not %s: %w", connectorID, cc.Type, kind, ports.ErrNotFound)
	}
	if err := host.VerifyWebhook(hdr, body); err != nil {
		return Result{}, err
	}
	ev, err := host.ParseWebhook(hdr, body)
	if err != nil {
		return Result{}, err
	}
	switch e := ev.(type) {
	case ports.PushEvent:
		return s.push(ctx, cc, e)
	case ports.ReviewEvent:
		return s.review(ctx, cc, e)
	}
	return Result{Status: http.StatusOK, Reason: ReasonIgnoredEvent}, nil
}

// IsBot reports whether an identity string is the Hub's bot (connector config bot_login / bot_email).
func IsBot(cc ports.ConnectorConfig, who string) bool {
	if who == "" {
		return false
	}
	for _, k := range []string{"bot_login", "bot_email"} {
		if v := cc.Config[k]; v != "" && strings.EqualFold(v, who) {
			return true
		}
	}
	return false
}

func (s *Service) push(ctx context.Context, cc ports.ConnectorConfig, e ports.PushEvent) (Result, error) {
	repo, err := s.Repos.ByName(ctx, cc.ID, e.Repo)
	if errors.Is(err, ports.ErrNotFound) {
		return Result{Status: http.StatusOK, Reason: ReasonUntrackedRepo}, nil
	}
	if err != nil {
		return Result{}, err
	}
	if !repo.Enabled || e.Branch != repo.Branch() || e.Deleted {
		return Result{Status: http.StatusOK, Reason: ReasonUntrackedBranch}, nil
	}
	// Bot-loop guard: the Hub's own pushes (direct docs commits, docs PR merges) must never trigger work.
	if IsBot(cc, e.Pusher) {
		return Result{Status: http.StatusOK, Reason: ReasonBotLoop}, nil
	}
	if len(e.Authors) > 0 {
		allBot := true
		for _, a := range e.Authors {
			allBot = allBot && IsBot(cc, a)
		}
		if allBot {
			return Result{Status: http.StatusOK, Reason: ReasonBotLoop}, nil
		}
	}
	if len(e.Paths) > 0 {
		onlyDocs := true
		for _, p := range e.Paths {
			onlyDocs = onlyDocs && strings.HasPrefix(p, repo.DocsPath)
		}
		if onlyDocs {
			return Result{Status: http.StatusOK, Reason: ReasonIgnoredPath}, nil
		}
	}
	job, err := s.enqueuePush(ctx, repo, e.BeforeSHA, e.AfterSHA, e.Pusher, e.Delivery)
	if err != nil {
		return Result{}, err
	}
	return Result{Status: http.StatusAccepted, Accepted: true, JobID: job.ID}, nil
}

func (s *Service) enqueuePush(ctx context.Context, repo ports.RepoConfig, before, after, pusher, delivery string) (ports.Job, error) {
	job, _, err := s.Queue.Enqueue(ctx, ports.NewJob{Type: ports.JobCodePush, RepoID: repo.ID,
		SerialKey: "repo:" + repo.ID, DedupeKey: "push:" + repo.ID + ":" + after, CorrelationID: delivery,
		Payload: pipeline.CodePushPayload{RepoID: repo.ID, Before: before, After: after, Pusher: pusher, Delivery: delivery}})
	return job, err
}

// ReviewPayload is the pr_review job payload.
type ReviewPayload struct {
	RepoID string `json:"repo_id"`
	Number int    `json:"number"`
}

func (s *Service) review(ctx context.Context, cc ports.ConnectorConfig, e ports.ReviewEvent) (Result, error) {
	if !e.Approved {
		return Result{Status: http.StatusOK, Reason: ReasonIgnoredEvent}, nil
	}
	repo, err := s.Repos.ByName(ctx, cc.ID, e.Repo)
	if errors.Is(err, ports.ErrNotFound) {
		return Result{Status: http.StatusOK, Reason: ReasonUntrackedRepo}, nil
	}
	if err != nil {
		return Result{}, err
	}
	job, _, err := s.Queue.Enqueue(ctx, ports.NewJob{Type: ports.JobPRReview, RepoID: repo.ID,
		DedupeKey: fmt.Sprintf("review:%s:%d", repo.ID, e.Number), Payload: ReviewPayload{RepoID: repo.ID, Number: e.Number}})
	if err != nil {
		return Result{}, err
	}
	return Result{Status: http.StatusAccepted, Accepted: true, JobID: job.ID}, nil
}

// Poll checks every enabled repo on a poll-mode connector and enqueues a code_push when its tracked
// branch moved past the last processed commit. Each connector is polled at most once per its interval.
func (s *Service) Poll(ctx context.Context) (int, error) {
	repos, err := s.Repos.ListEnabled(ctx)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	if s.lastPoll == nil {
		s.lastPoll = map[string]time.Time{}
	}
	s.mu.Unlock()
	due := map[string]bool{}
	var errs []error
	enqueued := 0
	for _, repo := range repos {
		host, cc, err := s.Hosts.HostAndConfig(ctx, repo.ConnectorID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if cc.Mode != "poll" && cc.Mode != "both" {
			continue
		}
		isDue, seen := due[cc.ID]
		if !seen {
			s.mu.Lock()
			interval := time.Duration(cc.PollSeconds) * time.Second
			isDue = s.now().Sub(s.lastPoll[cc.ID]) >= interval
			if isDue {
				s.lastPoll[cc.ID] = s.now()
			}
			s.mu.Unlock()
			due[cc.ID] = isDue
		}
		if !isDue {
			continue
		}
		head, err := host.BranchHead(ctx, repo.FullName, repo.Branch())
		if err != nil {
			errs = append(errs, fmt.Errorf("poll %s: %w", repo.FullName, err))
			continue
		}
		if head == repo.LastProcessedSHA {
			continue
		}
		if _, err := s.enqueuePush(ctx, repo, repo.LastProcessedSHA, head, "", "poll:"+head[:min(12, len(head))]); err != nil {
			errs = append(errs, err)
			continue
		}
		enqueued++
	}
	if len(errs) > 0 {
		s.log().Warn("git polling had errors", "errors", len(errs), "first", errs[0])
	}
	return enqueued, errors.Join(errs...)
}

// RequeueDocs regenerates a closed docs PR's files from current code (lifecycle.Requeuer).
func (s *Service) RequeueDocs(ctx context.Context, rec ports.PRRecord, reason string) error {
	if len(rec.SourcePaths) == 0 {
		return nil
	}
	_, _, err := s.Queue.Enqueue(ctx, ports.NewJob{Type: ports.JobCodePush, RepoID: rec.RepoID, SerialKey: "repo:" + rec.RepoID,
		DedupeKey: fmt.Sprintf("requeue:%s:%d", rec.RepoID, rec.Number), ReplayedFrom: rec.JobID,
		Payload: pipeline.CodePushPayload{RepoID: rec.RepoID, ForcePaths: rec.SourcePaths, Reason: reason}})
	return err
}
