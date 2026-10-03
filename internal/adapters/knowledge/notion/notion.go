// Package notion syncs Notion pages read-only for Q&A and the Library. The Hub sees exactly the pages
// shared with the Notion integration whose token it holds, so access is decided in Notion. Changed pages
// are found by searching pages newest-edited first; page bodies arrive as blocks and are converted to
// Markdown here.
package notion

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/adapters/httpx"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Version is the Notion API version the adapter speaks.
const Version = "2022-06-28"

// Space is the stream (and space) name of every Notion page: the integration's shared pages.
const Space = "notion"

// Limits keep one sync bounded: nested blocks are read this deep, and at most this many blocks per page.
const (
	maxDepth  = 3
	maxBlocks = 2000
)

// Source is the Notion adapter. Base is the API root (tests point it at a fake server).
type Source struct {
	Base string
	HTTP *httpx.Client
}

// New returns the adapter.
func New() *Source { return &Source{Base: "https://api.notion.com/v1", HTTP: httpx.New("notion")} }

// Type implements ports.KnowledgeSource.
func (*Source) Type() string { return "notion" }

// Streams implements ports.KnowledgeSource: one stream, every page shared with the integration.
func (*Source) Streams(ports.ConnectorConfig) ([]string, error) { return []string{Space}, nil }

func (s *Source) headers(cc ports.ConnectorConfig) (map[string]string, error) {
	tok := strings.TrimSpace(cc.Credentials)
	if tok == "" {
		return nil, &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "notion: the integration token is missing (Notion → Settings → Connections → Develop or manage integrations)"}
	}
	return map[string]string{"Authorization": "Bearer " + tok, "Notion-Version": Version}, nil
}

func (s *Source) call(ctx context.Context, cc ports.ConnectorConfig, method, path string, in, out any) error {
	h, err := s.headers(cc)
	if err != nil {
		return err
	}
	err = s.HTTP.JSON(ctx, method, s.Base+path, h, in, out)
	switch httpx.StatusOf(err) {
	case http.StatusUnauthorized:
		return &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "notion rejected the integration token (HTTP 401)"}
	case http.StatusNotFound:
		return fmt.Errorf("notion: %w (share the page with the integration)", ports.ErrNotFound)
	}
	return err
}

type page struct {
	ID             string              `json:"id"`
	URL            string              `json:"url"`
	LastEditedTime time.Time           `json:"last_edited_time"`
	Archived       bool                `json:"archived"`
	InTrash        bool                `json:"in_trash"`
	Properties     map[string]property `json:"properties"`
}

type property struct {
	Type  string     `json:"type"`
	Title []richText `json:"title"`
}

type richText struct {
	PlainText   string `json:"plain_text"`
	Href        string `json:"href"`
	Annotations struct {
		Bold   bool `json:"bold"`
		Italic bool `json:"italic"`
		Code   bool `json:"code"`
		Strike bool `json:"strikethrough"`
	} `json:"annotations"`
}

func (p page) title() string {
	for _, prop := range p.Properties {
		if prop.Type == "title" {
			return strings.TrimSpace(plain(prop.Title))
		}
	}
	return "Untitled"
}

type searchResp struct {
	Results    []page `json:"results"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor"`
}

// pages lists shared pages, most recently edited first, until stop returns true.
func (s *Source) pages(ctx context.Context, cc ports.ConnectorConfig, stop func(page) bool) ([]page, error) {
	var out []page
	cursor := ""
	for {
		body := map[string]any{"filter": map[string]string{"property": "object", "value": "page"},
			"sort": map[string]string{"direction": "descending", "timestamp": "last_edited_time"}, "page_size": 100}
		if cursor != "" {
			body["start_cursor"] = cursor
		}
		var r searchResp
		if err := s.call(ctx, cc, http.MethodPost, "/search", body, &r); err != nil {
			return nil, err
		}
		for _, p := range r.Results {
			if stop != nil && stop(p) {
				return out, nil
			}
			if !p.Archived && !p.InTrash {
				out = append(out, p)
			}
		}
		if !r.HasMore || r.NextCursor == "" {
			return out, nil
		}
		cursor = r.NextCursor
	}
}

// Changed implements ports.KnowledgeSource. The cursor is the last edit time already synced.
func (s *Source) Changed(ctx context.Context, cc ports.ConnectorConfig, _, cursor string, emit func([]ports.KnowledgeDoc, string) error) error {
	var since time.Time
	if cursor != "" {
		t, err := time.Parse(time.RFC3339Nano, cursor)
		if err != nil {
			return ports.Permanent(fmt.Errorf("notion: bad cursor %q", cursor))
		}
		since = t
	}
	ps, err := s.pages(ctx, cc, func(p page) bool { return !since.IsZero() && !p.LastEditedTime.After(since) })
	if err != nil {
		return err
	}
	// Oldest first, committed in pages of 20 so a crash repeats little.
	sort.Slice(ps, func(i, j int) bool { return ps[i].LastEditedTime.Before(ps[j].LastEditedTime) })
	for start := 0; start < len(ps); start += 20 {
		batch := ps[start:min(start+20, len(ps))]
		docs := make([]ports.KnowledgeDoc, 0, len(batch))
		for _, p := range batch {
			d, err := s.doc(ctx, cc, p)
			if err != nil {
				return err
			}
			docs = append(docs, d)
		}
		if err := emit(docs, batch[len(batch)-1].LastEditedTime.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return nil
}

// IDs implements ports.KnowledgeReconciler: pages no longer shared (or deleted) drop out of the index.
func (s *Source) IDs(ctx context.Context, cc ports.ConnectorConfig, _ string) ([]string, error) {
	ps, err := s.pages(ctx, cc, nil)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(ps))
	for i, p := range ps {
		ids[i] = p.ID
	}
	return ids, nil
}

// Labeled implements ports.KnowledgeSource. Notion pages carry no labels the Hub imports.
func (*Source) Labeled(context.Context, ports.ConnectorConfig, string) ([]ports.KnowledgeDoc, error) {
	return nil, nil
}

var idRE = regexp.MustCompile(`([0-9a-f]{32}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(?:[?#].*)?$`)

// Owns implements ports.KnowledgeSource: notion.so and notion.site links.
func (*Source) Owns(_ ports.ConnectorConfig, raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "notion.so" || h == "www.notion.so" || strings.HasSuffix(h, ".notion.site")
}

// Fetch implements ports.KnowledgeSource: the page a browser link points to.
func (s *Source) Fetch(ctx context.Context, cc ports.ConnectorConfig, raw string) (ports.KnowledgeDoc, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return ports.KnowledgeDoc{}, &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "not a Notion link"}
	}
	m := idRE.FindStringSubmatch(strings.ToLower(u.Path))
	if m == nil {
		return ports.KnowledgeDoc{}, &ports.ValidationError{Code: "VALIDATION_FAILED", Message: "the Notion link has no page ID"}
	}
	var p page
	if err := s.call(ctx, cc, http.MethodGet, "/pages/"+m[1], nil, &p); err != nil {
		return ports.KnowledgeDoc{}, err
	}
	return s.doc(ctx, cc, p)
}

func (s *Source) doc(ctx context.Context, cc ports.ConnectorConfig, p page) (ports.KnowledgeDoc, error) {
	var b strings.Builder
	n := 0
	if err := s.blocks(ctx, cc, p.ID, 0, &b, &n); err != nil {
		return ports.KnowledgeDoc{}, err
	}
	return ports.KnowledgeDoc{Source: ports.SourceNotion, ExternalID: p.ID, Space: Space, Title: p.title(), URL: p.URL,
		Markdown: strings.TrimSpace(b.String()), UpdatedAt: p.LastEditedTime}, nil
}

type blocksResp struct {
	Results    []map[string]any `json:"results"`
	HasMore    bool             `json:"has_more"`
	NextCursor string           `json:"next_cursor"`
}

// blocks writes a block's children as Markdown, depth levels deep.
func (s *Source) blocks(ctx context.Context, cc ports.ConnectorConfig, id string, depth int, b *strings.Builder, n *int) error {
	cursor := ""
	num := 0
	for {
		path := "/blocks/" + id + "/children?page_size=100"
		if cursor != "" {
			path += "&start_cursor=" + url.QueryEscape(cursor)
		}
		var r blocksResp
		if err := s.call(ctx, cc, http.MethodGet, path, nil, &r); err != nil {
			return err
		}
		for _, raw := range r.Results {
			if *n >= maxBlocks {
				return nil
			}
			*n++
			typ, _ := raw["type"].(string)
			if typ == "numbered_list_item" {
				num++
			} else {
				num = 0
			}
			writeBlock(b, typ, raw, depth, num)
			if has, _ := raw["has_children"].(bool); has && depth+1 < maxDepth && typ != "child_page" && typ != "child_database" {
				bid, _ := raw["id"].(string)
				if err := s.blocks(ctx, cc, bid, depth+1, b, n); err != nil {
					return err
				}
			}
		}
		if !r.HasMore || r.NextCursor == "" {
			return nil
		}
		cursor = r.NextCursor
	}
}

// writeBlock renders one block (num is its position in a numbered list).
func writeBlock(b *strings.Builder, typ string, raw map[string]any, depth, num int) {
	body, _ := raw[typ].(map[string]any)
	text := markdown(richTexts(body["rich_text"]))
	indent := strings.Repeat("  ", depth)
	switch typ {
	case "heading_1":
		fmt.Fprintf(b, "\n## %s\n\n", text) // the page title is the only level-one heading
	case "heading_2":
		fmt.Fprintf(b, "\n### %s\n\n", text)
	case "heading_3":
		fmt.Fprintf(b, "\n#### %s\n\n", text)
	case "bulleted_list_item", "toggle":
		fmt.Fprintf(b, "%s- %s\n", indent, text)
	case "numbered_list_item":
		fmt.Fprintf(b, "%s%d. %s\n", indent, num, text)
	case "to_do":
		box := " "
		if c, _ := body["checked"].(bool); c {
			box = "x"
		}
		fmt.Fprintf(b, "%s- [%s] %s\n", indent, box, text)
	case "quote", "callout":
		fmt.Fprintf(b, "\n> %s\n\n", text)
	case "code":
		lang, _ := body["language"].(string)
		if lang == "plain text" {
			lang = ""
		}
		fmt.Fprintf(b, "\n```%s\n%s\n```\n\n", lang, plain(richTexts(body["rich_text"])))
	case "divider":
		b.WriteString("\n---\n\n")
	case "child_page":
		if t, _ := body["title"].(string); t != "" {
			fmt.Fprintf(b, "%s- Subpage: %s\n", indent, t)
		}
	case "table_row":
		cells, _ := body["cells"].([]any)
		parts := make([]string, 0, len(cells))
		for _, c := range cells {
			parts = append(parts, strings.ReplaceAll(markdown(richTexts(c)), "|", `\|`))
		}
		fmt.Fprintf(b, "| %s |\n", strings.Join(parts, " | "))
	default: // paragraph and anything with text
		if text != "" {
			fmt.Fprintf(b, "%s%s\n\n", indent, text)
		}
	}
}

func richTexts(v any) []richText {
	items, _ := v.([]any)
	out := make([]richText, 0, len(items))
	for _, it := range items {
		m, _ := it.(map[string]any)
		var rt richText
		rt.PlainText, _ = m["plain_text"].(string)
		rt.Href, _ = m["href"].(string)
		if a, ok := m["annotations"].(map[string]any); ok {
			rt.Annotations.Bold, _ = a["bold"].(bool)
			rt.Annotations.Italic, _ = a["italic"].(bool)
			rt.Annotations.Code, _ = a["code"].(bool)
			rt.Annotations.Strike, _ = a["strikethrough"].(bool)
		}
		out = append(out, rt)
	}
	return out
}

func plain(rts []richText) string {
	var b strings.Builder
	for _, r := range rts {
		b.WriteString(r.PlainText)
	}
	return b.String()
}

func markdown(rts []richText) string {
	var b strings.Builder
	for _, r := range rts {
		t := r.PlainText
		if strings.TrimSpace(t) == "" {
			b.WriteString(t)
			continue
		}
		switch {
		case r.Annotations.Code:
			t = "`" + t + "`"
		case r.Annotations.Bold && r.Annotations.Italic:
			t = "***" + t + "***"
		case r.Annotations.Bold:
			t = "**" + t + "**"
		case r.Annotations.Italic:
			t = "*" + t + "*"
		}
		if r.Annotations.Strike {
			t = "~~" + t + "~~"
		}
		if r.Href != "" {
			t = "[" + t + "](" + r.Href + ")"
		}
		b.WriteString(t)
	}
	return b.String()
}
