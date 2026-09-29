package ports

import "time"

// ChunkSource distinguishes where a chunk came from (plan § 6.3).
type ChunkSource string

const (
	SourceCode         ChunkSource = "code"
	SourceGeneratedDoc ChunkSource = "generated_doc"
	SourceImportedDoc  ChunkSource = "imported_doc"
	SourceConfluence   ChunkSource = "confluence"
	SourceJira         ChunkSource = "jira"
	SourceIssueDecode  ChunkSource = "issue_decode"
)

// Chunk is a retrieval-sized, individually addressable unit of code or documentation.
type Chunk struct {
	ID          string      `json:"chunk_id"`
	RepoID      string      `json:"repo_id,omitempty"`
	Scope       string      `json:"scope"` // repo full name, confluence:<space>, jira:<project>, issue
	Source      ChunkSource `json:"source"`
	Path        string      `json:"path"`
	Symbol      string      `json:"symbol"`
	Language    string      `json:"language"`
	Content     string      `json:"content"`
	ContentHash string      `json:"content_hash"`
	Signature   string      `json:"signature,omitempty"`
	CommitSHA   string      `json:"commit_sha,omitempty"`
	URL         string      `json:"url,omitempty"`
	StartLine   int         `json:"start_line,omitempty"`
	EndLine     int         `json:"end_line,omitempty"`
	UpdatedAt   time.Time   `json:"updated_at,omitempty"`
	DeletedAt   *time.Time  `json:"deleted_at,omitempty"`
}

// Live reports whether the chunk has not been soft-deleted.
func (c Chunk) Live() bool { return c.DeletedAt == nil }

// ChunkWrite is one transactional manifest update.
type ChunkWrite struct {
	Upserts []Chunk
	Revive  []string
	Remove  []string
	// Drop hard-deletes IDs (a pure rename's old IDs, whose content now lives under new IDs).
	Drop []string
}

// Empty reports whether the write changes nothing.
func (w ChunkWrite) Empty() bool {
	return len(w.Upserts)+len(w.Revive)+len(w.Remove)+len(w.Drop) == 0
}

// DocSection is one section of a generated doc file, for the docs Tree.
type DocSection struct {
	ChunkID string
	Title   string
}
