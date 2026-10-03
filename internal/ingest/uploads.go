package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/core/docconv"
	"github.com/GokulMV/DocTheRepo/internal/core/llmgateway"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// UploadStore holds uploaded documents (store.Knowledge).
type UploadStore interface {
	UploadConnector(ctx context.Context) (string, error)
}

// UploadFile is one file as uploaded.
type UploadFile struct {
	Name string
	Data []byte
}

// UploadedDoc reports one stored file.
type UploadedDoc struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// UploadResult reports an upload: stored documents, files refused (with why), embedded chunks.
type UploadResult struct {
	Documents []UploadedDoc       `json:"documents"`
	Refused   []map[string]string `json:"refused,omitempty"`
	Embedded  int                 `json:"embedded"`
	Notes     []string            `json:"notes,omitempty"`
}

// DefaultCollection holds uploads with no collection named.
const DefaultCollection = "Uploads"

// UploadID is an uploaded document's stable ID: uploading the same file name to the same collection
// replaces the document.
func UploadID(collection, name string) string {
	h := sha256.Sum256([]byte(strings.ToLower(collection) + "/" + strings.ToLower(name)))
	return "up-" + hex.EncodeToString(h[:12])
}

// Upload converts files to Markdown and stores them as shared team documents: chunked, embedded, linked
// into the Palace and placed on Library shelves, like a synced Confluence page.
func (k *KnowledgeSync) Upload(ctx context.Context, userID, collection string, files []UploadFile) (UploadResult, error) {
	res := UploadResult{Documents: []UploadedDoc{}}
	if k.Uploads == nil {
		return res, errors.New("uploads are not configured")
	}
	collection = strings.TrimSpace(collection)
	if collection == "" {
		collection = DefaultCollection
	}
	if len(collection) > 80 {
		return res, &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "the collection name is longer than 80 characters"}
	}
	var docs []ports.KnowledgeDoc
	for _, f := range files {
		title, md, err := docconv.Convert(f.Name, f.Data)
		if err == nil && strings.TrimSpace(md) == "" {
			err = errors.New(f.Name + " has no text")
		}
		if err != nil {
			res.Refused = append(res.Refused, map[string]string{"name": f.Name, "error": err.Error()})
			continue
		}
		id := UploadID(collection, f.Name)
		url := "/library/uploads/" + id
		docs = append(docs, ports.KnowledgeDoc{Source: ports.SourceUpload, ExternalID: id, Space: collection, Title: title, URL: url,
			Markdown: md, UploadedBy: userID, UpdatedAt: k.now()})
		res.Documents = append(res.Documents, UploadedDoc{ID: id, Name: f.Name, Title: title, URL: url})
	}
	if len(docs) == 0 {
		return res, nil
	}
	cid, err := k.Uploads.UploadConnector(ctx)
	if err != nil {
		return res, err
	}
	idx, err := k.Docs.MentionIndex(ctx)
	if err != nil {
		return res, err
	}
	a, err := k.Docs.Apply(ctx, cid, docs, idx)
	if err != nil {
		return res, err
	}
	if k.Embed != nil && len(a.Embed) > 0 {
		n, err := k.Embed(ctx, llmgateway.CallMeta{UserID: userID}, a.Embed)
		var sb *ports.SpendBlockedError
		switch {
		case errors.As(err, &sb):
			res.Notes = append(res.Notes, "embedding blocked by the spend guard (the documents are searchable by keyword): "+sb.Error())
		case errors.Is(err, llmgateway.ErrNoRoute):
			res.Notes = append(res.Notes, "no embedding model is set up, so the documents are searchable by keyword only")
		case err != nil:
			return res, err
		}
		res.Embedded = n
	}
	return res, nil
}
