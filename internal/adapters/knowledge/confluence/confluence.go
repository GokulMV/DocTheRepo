// Package confluence syncs Confluence spaces (Cloud and Data Center) read-only for Q&A, decode runbooks,
// the Library, and known-issue import (plan § 8.10, § 8.16). Changed pages are found with a CQL search on
// lastmodified per space; bodies arrive in storage format and are converted to Markdown here.
package confluence

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/adapters/knowledge/atlassian"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Config keys: base_url (Cloud: https://<site>.atlassian.net/wiki), spaces, email (Cloud; omit for a Data
// Center personal access token), timezone (the token user's Confluence time zone; optional).
const (
	PageSize = 50
	// MaxLabeled caps the known-issue label query.
	MaxLabeled = 500
	expand     = "body.storage,version,metadata.labels,space"
)

// Source is the Confluence adapter.
type Source struct{}

// New returns the adapter.
func New() *Source { return &Source{} }

// Type implements ports.KnowledgeSource.
func (*Source) Type() string { return "confluence" }

// Streams implements ports.KnowledgeSource: one stream per space key.
func (*Source) Streams(cc ports.ConnectorConfig) ([]string, error) {
	return atlassian.Keys("confluence", "spaces", cc.Config["spaces"])
}

type content struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Title  string `json:"title"`
	Space  struct {
		Key string `json:"key"`
	} `json:"space"`
	Version struct {
		When   time.Time `json:"when"`
		Number int       `json:"number"`
	} `json:"version"`
	Body struct {
		Storage struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`
	Metadata struct {
		Labels struct {
			Results []struct {
				Name string `json:"name"`
			} `json:"results"`
		} `json:"labels"`
	} `json:"metadata"`
	Links struct {
		WebUI string `json:"webui"`
	} `json:"_links"`
}

type searchResp struct {
	Results []content `json:"results"`
	Links   struct {
		Next string `json:"next"`
	} `json:"_links"`
}

func (s *Source) site(cc ports.ConnectorConfig) (*atlassian.Site, error) {
	return atlassian.NewSite("confluence", cc)
}

// search runs a CQL query page by page; the next link must stay on the configured site.
func search(ctx context.Context, site *atlassian.Site, cql string, withBody bool, limit int, page func([]content) error) error {
	q := url.Values{"cql": {cql}, "limit": {fmt.Sprint(limit)}}
	if withBody {
		q.Set("expand", expand)
	}
	next := "/rest/api/content/search?" + q.Encode()
	for next != "" {
		var r searchResp
		if err := site.Get(ctx, next, &r); err != nil {
			return err
		}
		if err := page(r.Results); err != nil {
			return err
		}
		next = relativeNext(site, r.Links.Next)
	}
	return nil
}

// relativeNext keeps a pagination link on the site: Cloud returns it relative to the /wiki base.
func relativeNext(site *atlassian.Site, next string) string {
	if next == "" {
		return ""
	}
	if u, err := url.Parse(next); err == nil && u.IsAbs() {
		if !site.Owns(next) {
			return ""
		}
		base, _ := url.Parse(site.Base)
		next = strings.TrimPrefix(u.RequestURI(), strings.TrimRight(base.Path, "/"))
	}
	if !strings.HasPrefix(next, "/") {
		return ""
	}
	return next
}

func (s *Source) doc(site *atlassian.Site, c content) ports.KnowledgeDoc {
	d := ports.KnowledgeDoc{Source: ports.SourceConfluence, ExternalID: c.ID, Space: c.Space.Key, Title: c.Title,
		Markdown: ToMarkdown(c.Body.Storage.Value), Status: c.Status, UpdatedAt: c.Version.When}
	if c.Links.WebUI != "" {
		d.URL = site.Base + c.Links.WebUI
	}
	for _, l := range c.Metadata.Labels.Results {
		d.Labels = append(d.Labels, strings.ToLower(l.Name))
	}
	return d
}

// Changed implements ports.KnowledgeSource. The cursor is the newest lastmodified seen; the query starts
// at that minute (inclusive), so a page edited twice in one minute is never missed.
func (s *Source) Changed(ctx context.Context, cc ports.ConnectorConfig, space, cursor string, emit func([]ports.KnowledgeDoc, string) error) error {
	site, err := s.site(cc)
	if err != nil {
		return err
	}
	cql := fmt.Sprintf("space = %s AND type = page", atlassian.Quote(space))
	since := atlassian.ParseCursor(cursor)
	if !since.IsZero() {
		cql += fmt.Sprintf(" AND lastmodified >= %s", atlassian.Quote(atlassian.QueryTime(since, atlassian.Zone(cc.Config["timezone"]))))
	}
	cql += " ORDER BY lastmodified ASC"
	newest := since
	return search(ctx, site, cql, true, PageSize, func(cs []content) error {
		docs := make([]ports.KnowledgeDoc, 0, len(cs))
		for _, c := range cs {
			if c.Type != "" && c.Type != "page" {
				continue
			}
			docs = append(docs, s.doc(site, c))
			if c.Version.When.After(newest) {
				newest = c.Version.When
			}
		}
		next := ""
		if !newest.IsZero() {
			next = atlassian.FormatCursor(newest)
		}
		return emit(docs, next)
	})
}

// Labeled implements ports.KnowledgeSource.
func (s *Source) Labeled(ctx context.Context, cc ports.ConnectorConfig, label string) ([]ports.KnowledgeDoc, error) {
	site, err := s.site(cc)
	if err != nil {
		return nil, err
	}
	spaces, err := s.Streams(cc)
	if err != nil {
		return nil, err
	}
	cql := fmt.Sprintf("label = %s AND space in (%s) AND type = page ORDER BY lastmodified DESC", atlassian.Quote(label), atlassian.QuoteAll(spaces))
	var out []ports.KnowledgeDoc
	err = search(ctx, site, cql, true, PageSize, func(cs []content) error {
		for _, c := range cs {
			if len(out) < MaxLabeled {
				out = append(out, s.doc(site, c))
			}
		}
		if len(out) >= MaxLabeled {
			return errStop
		}
		return nil
	})
	if err == errStop {
		err = nil
	}
	return out, err
}

var errStop = fmt.Errorf("stop")

// IDs implements ports.KnowledgeReconciler: every live page ID in a space (no bodies).
func (s *Source) IDs(ctx context.Context, cc ports.ConnectorConfig, space string) ([]string, error) {
	site, err := s.site(cc)
	if err != nil {
		return nil, err
	}
	var ids []string
	err = search(ctx, site, fmt.Sprintf("space = %s AND type = page", atlassian.Quote(space)), false, 200, func(cs []content) error {
		for _, c := range cs {
			ids = append(ids, c.ID)
		}
		return nil
	})
	return ids, err
}

// Owns implements ports.KnowledgeSource.
func (s *Source) Owns(cc ports.ConnectorConfig, raw string) bool {
	site, err := s.site(cc)
	return err == nil && site.Owns(raw) && PageID(raw) != ""
}

var (
	pagePathRE = regexp.MustCompile(`/pages/(?:edit-v2/|edit/)?(\d+)(?:/|$)`)
	pageIDRE   = regexp.MustCompile(`^\d+$`)
)

// PageID extracts a page ID from a Confluence URL (/spaces/KEY/pages/123/Title or ?pageId=123).
func PageID(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	if id := u.Query().Get("pageId"); pageIDRE.MatchString(id) {
		return id
	}
	if m := pagePathRE.FindStringSubmatch(u.Path); m != nil {
		return m[1]
	}
	return ""
}

// Fetch implements ports.KnowledgeSource.
func (s *Source) Fetch(ctx context.Context, cc ports.ConnectorConfig, raw string) (ports.KnowledgeDoc, error) {
	site, err := s.site(cc)
	if err != nil {
		return ports.KnowledgeDoc{}, err
	}
	id := PageID(raw)
	if id == "" {
		return ports.KnowledgeDoc{}, &ports.ValidationError{Code: "INVALID_URL", Message: "not a Confluence page URL"}
	}
	var c content
	if err := site.Get(ctx, "/rest/api/content/"+id+"?expand="+url.QueryEscape(expand), &c); err != nil {
		return ports.KnowledgeDoc{}, err
	}
	return s.doc(site, c), nil
}
