package ports

import (
	"context"
	"time"
)

// SharedSources are chunk sources that belong to no repository (Confluence pages, Jira issues). Their
// repo-less chunks are readable by every viewer: the Hub syncs them with one read-only service account,
// so access is decided by what an admin chose to sync, not by repository grants.
var SharedSources = []ChunkSource{SourceConfluence, SourceJira}

// KnowledgeDoc is one Confluence page or Jira issue, converted to Markdown by its adapter.
type KnowledgeDoc struct {
	Source     ChunkSource `json:"source"`      // confluence | jira
	ExternalID string      `json:"external_id"` // Confluence page ID or Jira issue key
	Space      string      `json:"space"`       // Confluence space key or Jira project key
	Title      string      `json:"title"`
	URL        string      `json:"url"`
	Markdown   string      `json:"markdown"`
	Labels     []string    `json:"labels"`
	Status     string      `json:"status"` // Jira status name; Confluence page status
	// Done is true when a Jira issue's status category is Done (a fixed bug).
	Done      bool      `json:"done"`
	UpdatedAt time.Time `json:"updated_at"`
}

// KnowledgeSource reads a knowledge connector (plan § 8.16). Streams are spaces or projects; each keeps its
// own cursor, which the caller stores only after a page of documents is committed.
type KnowledgeSource interface {
	// Type is the connector type served (confluence, jira).
	Type() string
	// Streams lists the configured spaces or projects.
	Streams(cc ConnectorConfig) ([]string, error)
	// Changed emits documents of one stream updated since cursor ("" = first sync), oldest first, with the
	// cursor to store once each page is committed.
	Changed(ctx context.Context, cc ConnectorConfig, stream, cursor string, emit func(docs []KnowledgeDoc, cursor string) error) error
	// Labeled returns every document in the configured streams carrying label (the known-issue import).
	Labeled(ctx context.Context, cc ConnectorConfig, label string) ([]KnowledgeDoc, error)
	// Owns reports whether a browser URL points into this connector's site.
	Owns(cc ConnectorConfig, url string) bool
	// Fetch loads the document a browser URL points to.
	Fetch(ctx context.Context, cc ConnectorConfig, url string) (KnowledgeDoc, error)
}

// KnowledgeReconciler is implemented by sources that can list every live document of a stream cheaply, so
// documents deleted upstream are removed from the index.
type KnowledgeReconciler interface {
	IDs(ctx context.Context, cc ConnectorConfig, stream string) ([]string, error)
}
