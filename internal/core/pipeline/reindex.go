package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// ReindexPayload is the reindex job payload.
type ReindexPayload struct {
	PageSize int `json:"page_size,omitempty"`
}

// ReindexResult is stored on the job.
type ReindexResult struct {
	From     *ports.EmbeddingSpec `json:"from,omitempty"`
	To       ports.EmbeddingSpec  `json:"to"`
	Version  int                  `json:"version"`
	Embedded int                  `json:"embedded"`
	CaughtUp int                  `json:"caught_up"`
}

// Reindex rebuilds the vector index under the embedding route's current model without downtime
// (plan § 8.18): reads stay on the live version while a pending version is filled, pushes during the
// rebuild embed with the new model straight into the pending version, a catch-up pass covers chunks
// written meanwhile, and the swap is atomic. A transient failure keeps the pending version so the retry
// resumes it; a permanent one aborts it and leaves the live index untouched.
func (p *Pipeline) Reindex(ctx context.Context, job ports.Job) (ports.Outcome, error) {
	var pl ReindexPayload
	_ = json.Unmarshal(job.Payload, &pl)
	page := pl.PageSize
	if page <= 0 {
		page = 256
	}
	idx := p.Indexer.Index
	meta := llmgateway.CallMeta{JobID: job.ID}
	vecs, route, err := p.GW.Embed(ctx, meta, []string{"dimension probe"})
	if err != nil {
		return p.spendOrErr(err)
	}
	spec := ports.EmbeddingSpec{ProviderKind: route.ProviderKind, Model: route.Model, Dimensions: len(vecs[0])}
	st, err := idx.State(ctx)
	if err != nil {
		return ports.Outcome{}, err
	}
	res := ReindexResult{From: st.Live, To: spec}
	if st.Live == nil {
		if err := idx.EnsureIndex(ctx, spec); err != nil {
			return ports.Outcome{}, err
		}
		n, err := p.walk(ctx, meta, page, time.Time{}, 0)
		res.Embedded = n
		if err != nil {
			return p.spendOrErr(err)
		}
		return ports.Outcome{Status: ports.JobDone, Result: res, Message: "index initialised"}, nil
	}
	if *st.Live == spec {
		return ports.Outcome{Status: ports.JobAborted, Result: res, Message: "the index already uses " + spec.Model}, nil
	}
	v, err := idx.BeginReindex(ctx, spec)
	if err != nil {
		return ports.Outcome{}, err
	}
	res.Version = v
	start := time.Now()
	abort := func(err error) (ports.Outcome, error) {
		var pe *ports.PermanentError
		if errors.As(err, &pe) {
			_ = idx.AbortReindex(context.WithoutCancel(ctx), v)
		}
		return p.spendOrErr(err)
	}
	if res.Embedded, err = p.walk(ctx, meta, page, time.Time{}, v); err != nil {
		return abort(err)
	}
	if res.CaughtUp, err = p.walk(ctx, meta, page, start, v); err != nil {
		return abort(err)
	}
	if err := idx.SwapReindex(ctx, v); err != nil {
		return abort(err)
	}
	p.log().Info("reindex swapped", "from", res.From.Model, "to", spec.Model, "version", v, "embedded", res.Embedded)
	return ports.Outcome{Status: ports.JobDone, Result: res}, nil
}

// walk embeds every live chunk (updated at or after since, when set) into version (0 = live).
func (p *Pipeline) walk(ctx context.Context, meta llmgateway.CallMeta, page int, since time.Time, version int) (int, error) {
	total, after := 0, ""
	for {
		chunks, err := p.Chunks.LiveAfter(ctx, after, page)
		if err != nil {
			return total, err
		}
		if len(chunks) == 0 {
			return total, nil
		}
		after = chunks[len(chunks)-1].ID
		batch := chunks[:0:0]
		for _, c := range chunks {
			if since.IsZero() || !c.UpdatedAt.Before(since) {
				batch = append(batch, c)
			}
		}
		n, err := p.Indexer.EmbedVersion(ctx, meta, batch, version)
		if err != nil {
			return total, fmt.Errorf("embed chunks after %s: %w", after, err)
		}
		total += n
	}
}

func (p *Pipeline) spendOrErr(err error) (ports.Outcome, error) {
	var sb *ports.SpendBlockedError
	if errors.As(err, &sb) {
		return ports.Outcome{Status: ports.JobSpendBlocked, Message: sb.Error()}, nil
	}
	return ports.Outcome{}, err
}
