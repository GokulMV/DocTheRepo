// Package memprs is an in-memory ports.PRStore for push and pipeline unit tests.
package memprs

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Store is an in-memory PR store.
type Store struct {
	mu   sync.Mutex
	recs map[string]ports.PRRecord
}

// New returns an empty store.
func New() *Store { return &Store{recs: map[string]ports.PRRecord{}} }

func key(repo string, n int) string { return fmt.Sprintf("%s#%d", repo, n) }

// SavePR stores a record.
func (s *Store) SavePR(_ context.Context, r ports.PRRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.OpenedAt.IsZero() {
		if old, ok := s.recs[key(r.RepoID, r.Number)]; ok {
			r.OpenedAt = old.OpenedAt
		} else {
			r.OpenedAt = time.Now()
		}
	}
	r.UpdatedAt = time.Now()
	s.recs[key(r.RepoID, r.Number)] = r
	return nil
}

// GetPR loads a record.
func (s *Store) GetPR(_ context.Context, repo string, n int) (ports.PRRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.recs[key(repo, n)]
	if !ok {
		return r, ports.ErrNotFound
	}
	return r, nil
}

// ActivePRs lists a repo's active records.
func (s *Store) ActivePRs(_ context.Context, repo string) ([]ports.PRRecord, error) {
	return s.filter(func(r ports.PRRecord) bool { return r.RepoID == repo && r.Active() }), nil
}

// AllActivePRs lists all active records.
func (s *Store) AllActivePRs(context.Context) ([]ports.PRRecord, error) {
	return s.filter(ports.PRRecord.Active), nil
}

// All lists every record.
func (s *Store) All() []ports.PRRecord { return s.filter(func(ports.PRRecord) bool { return true }) }

// SetPRState updates a record's state.
func (s *Store) SetPRState(_ context.Context, repo string, n int, state, note string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.recs[key(repo, n)]
	if !ok {
		return ports.ErrNotFound
	}
	r.State, r.Note, r.UpdatedAt = state, note, time.Now()
	s.recs[key(repo, n)] = r
	return nil
}

// Age backdates a record's opened time (staleness tests).
func (s *Store) Age(repo string, n int, d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.recs[key(repo, n)]
	r.OpenedAt = r.OpenedAt.Add(-d)
	s.recs[key(repo, n)] = r
}

func (s *Store) filter(f func(ports.PRRecord) bool) []ports.PRRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ports.PRRecord
	for _, r := range s.recs {
		if f(r) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}
