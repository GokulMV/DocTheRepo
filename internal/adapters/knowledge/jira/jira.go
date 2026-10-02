// Package jira syncs Jira projects (Cloud REST v3 and Data Center REST v2) read-only: issues updated since
// the cursor are found by JQL per project, descriptions and comments are converted to Markdown, and the
// status category tells known-issue import when a bug was fixed upstream (plan § 8.10, § 8.16).
package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/GokulMV/DocTheRepo/internal/adapters/knowledge/atlassian"
	"github.com/GokulMV/DocTheRepo/internal/ports"
)

// Config keys: base_url, projects, email (Cloud; omit for a Data Center personal access token),
// api_version (3 = Cloud, 2 = Data Center; default 3 with an e-mail, else 2), jql (extra filter ANDed into
// every query, e.g. `issuetype in (Bug, Incident)`), lookback_days (first sync, default 365), timezone.
const (
	PageSize       = 100
	MaxLabeled     = 500
	MaxComments    = 20
	defaultLookbck = 365
	fields         = "summary,description,labels,status,updated,issuetype,priority,components,resolution,comment,project"
)

// Source is the Jira adapter.
type Source struct {
	Now func() time.Time
}

// New returns the adapter.
func New() *Source { return &Source{} }

// Type implements ports.KnowledgeSource.
func (*Source) Type() string { return "jira" }

// Streams implements ports.KnowledgeSource: one stream per project key.
func (*Source) Streams(cc ports.ConnectorConfig) ([]string, error) {
	return atlassian.Keys("jira", "projects", cc.Config["projects"])
}

type named struct {
	Name string `json:"name"`
}

type issue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary     string          `json:"summary"`
		Description json.RawMessage `json:"description"`
		Labels      []string        `json:"labels"`
		Status      struct {
			Name           string `json:"name"`
			StatusCategory struct {
				Key string `json:"key"`
			} `json:"statusCategory"`
		} `json:"status"`
		Updated    string  `json:"updated"`
		IssueType  named   `json:"issuetype"`
		Priority   *named  `json:"priority"`
		Resolution *named  `json:"resolution"`
		Components []named `json:"components"`
		Project    struct {
			Key string `json:"key"`
		} `json:"project"`
		Comment struct {
			Comments []struct {
				Author struct {
					DisplayName string `json:"displayName"`
				} `json:"author"`
				Created string          `json:"created"`
				Body    json.RawMessage `json:"body"`
			} `json:"comments"`
		} `json:"comment"`
	} `json:"fields"`
}

type client struct {
	site    *atlassian.Site
	version string
	extra   string
	zone    string
}

func (s *Source) client(cc ports.ConnectorConfig) (*client, error) {
	site, err := atlassian.NewSite("jira", cc)
	if err != nil {
		return nil, err
	}
	v := strings.TrimSpace(cc.Config["api_version"])
	if v == "" {
		v = "2"
		if cc.Config["email"] != "" {
			v = "3"
		}
	}
	if v != "2" && v != "3" {
		return nil, &ports.ValidationError{Code: "INVALID_CONNECTOR", Message: "jira: api_version must be 2 (Data Center) or 3 (Cloud)"}
	}
	return &client{site: site, version: v, extra: strings.TrimSpace(cc.Config["jql"]), zone: cc.Config["timezone"]}, nil
}

func (c *client) api(p string) string { return "/rest/api/" + c.version + p }

// search runs JQL page by page (v3: nextPageToken on /search/jql; v2: startAt on /search).
func (c *client) search(ctx context.Context, jql string, page func([]issue) error) error {
	if c.extra != "" {
		jql = "(" + c.extra + ") AND " + jql
	}
	q := url.Values{"jql": {jql}, "fields": {fields}, "maxResults": {strconv.Itoa(PageSize)}}
	for start := 0; ; {
		var r struct {
			Issues        []issue `json:"issues"`
			NextPageToken string  `json:"nextPageToken"`
			IsLast        *bool   `json:"isLast"`
			Total         int     `json:"total"`
		}
		path := c.api("/search/jql")
		if c.version == "2" {
			path = c.api("/search")
			q.Set("startAt", strconv.Itoa(start))
		}
		if err := c.site.Get(ctx, path+"?"+q.Encode(), &r); err != nil {
			return err
		}
		if err := page(r.Issues); err != nil {
			return err
		}
		if c.version == "3" {
			if r.NextPageToken == "" || (r.IsLast != nil && *r.IsLast) || len(r.Issues) == 0 {
				return nil
			}
			q.Set("nextPageToken", r.NextPageToken)
			continue
		}
		start += len(r.Issues)
		if len(r.Issues) == 0 || start >= r.Total {
			return nil
		}
	}
}

// location is the zone JQL dates are read in: the configured one, else the token user's profile zone.
func (c *client) location(ctx context.Context) *time.Location {
	if loc := atlassian.Zone(c.zone); loc != nil {
		return loc
	}
	var me struct {
		TimeZone string `json:"timeZone"`
	}
	if err := c.site.Get(ctx, c.api("/myself"), &me); err != nil {
		return nil
	}
	return atlassian.Zone(me.TimeZone)
}

const updatedLayout = "2006-01-02T15:04:05.000-0700"

func parseTime(s string) time.Time {
	for _, l := range []string{updatedLayout, time.RFC3339Nano} {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (c *client) doc(it issue) ports.KnowledgeDoc {
	f := it.Fields
	d := ports.KnowledgeDoc{Source: ports.SourceJira, ExternalID: it.Key, Space: f.Project.Key, Title: it.Key + ": " + f.Summary,
		URL: c.site.Base + "/browse/" + it.Key, Status: f.Status.Name, Done: f.Status.StatusCategory.Key == "done",
		UpdatedAt: parseTime(f.Updated)}
	if d.Space == "" {
		d.Space, _, _ = strings.Cut(it.Key, "-")
	}
	for _, l := range f.Labels {
		d.Labels = append(d.Labels, strings.ToLower(l))
	}
	var meta []string
	add := func(k, v string) {
		if v != "" {
			meta = append(meta, "**"+k+":** "+v)
		}
	}
	add("Type", f.IssueType.Name)
	add("Status", f.Status.Name)
	if f.Priority != nil {
		add("Priority", f.Priority.Name)
	}
	if f.Resolution != nil {
		add("Resolution", f.Resolution.Name)
	}
	var comps []string
	for _, c := range f.Components {
		comps = append(comps, c.Name)
	}
	add("Components", strings.Join(comps, ", "))
	add("Labels", strings.Join(f.Labels, ", "))
	var b strings.Builder
	b.WriteString("# " + d.Title + "\n\n")
	if len(meta) > 0 {
		b.WriteString(strings.Join(meta, " · ") + "\n\n")
	}
	if desc := RichText(f.Description); desc != "" {
		b.WriteString("## Description\n\n" + desc + "\n\n")
	}
	cs := f.Comment.Comments
	if len(cs) > MaxComments {
		cs = cs[len(cs)-MaxComments:]
	}
	if len(cs) > 0 {
		b.WriteString("## Comments\n\n")
		for _, cm := range cs {
			body := RichText(cm.Body)
			if body == "" {
				continue
			}
			who := cm.Author.DisplayName
			if who == "" {
				who = "Someone"
			}
			when := ""
			if t := parseTime(cm.Created); !t.IsZero() {
				when = " (" + t.UTC().Format("2006-01-02") + ")"
			}
			b.WriteString("**" + who + "**" + when + ": " + body + "\n\n")
		}
	}
	d.Markdown = strings.TrimSpace(b.String())
	return d
}

func (s *Source) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Changed implements ports.KnowledgeSource. The first sync reads lookback_days of history.
func (s *Source) Changed(ctx context.Context, cc ports.ConnectorConfig, project, cursor string, emit func([]ports.KnowledgeDoc, string) error) error {
	c, err := s.client(cc)
	if err != nil {
		return err
	}
	since := atlassian.ParseCursor(cursor)
	if since.IsZero() {
		days := defaultLookbck
		if n, err := strconv.Atoi(cc.Config["lookback_days"]); err == nil && n > 0 {
			days = n
		}
		since = s.now().AddDate(0, 0, -days)
	}
	jql := fmt.Sprintf("project = %s AND updated >= %s ORDER BY updated ASC, key ASC", atlassian.Quote(project),
		atlassian.Quote(atlassian.QueryTime(since, c.location(ctx))))
	newest := since
	return c.search(ctx, jql, func(is []issue) error {
		docs := make([]ports.KnowledgeDoc, len(is))
		for i, it := range is {
			docs[i] = c.doc(it)
			if docs[i].UpdatedAt.After(newest) {
				newest = docs[i].UpdatedAt
			}
		}
		return emit(docs, atlassian.FormatCursor(newest))
	})
}

var errStop = fmt.Errorf("stop")

// Labeled implements ports.KnowledgeSource (open and done issues alike: Done is what flips a rule).
func (s *Source) Labeled(ctx context.Context, cc ports.ConnectorConfig, label string) ([]ports.KnowledgeDoc, error) {
	c, err := s.client(cc)
	if err != nil {
		return nil, err
	}
	projects, err := s.Streams(cc)
	if err != nil {
		return nil, err
	}
	jql := fmt.Sprintf("labels = %s AND project in (%s) ORDER BY updated DESC", atlassian.Quote(label), atlassian.QuoteAll(projects))
	var out []ports.KnowledgeDoc
	err = c.search(ctx, jql, func(is []issue) error {
		for _, it := range is {
			if len(out) < MaxLabeled {
				out = append(out, c.doc(it))
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

var keyRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]+-\d+$`)

// IssueKey extracts an issue key from a Jira URL (/browse/ENG-12 or ?selectedIssue=ENG-12).
func IssueKey(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	if k := strings.ToUpper(u.Query().Get("selectedIssue")); keyRE.MatchString(k) {
		return k
	}
	if _, rest, ok := strings.Cut(u.Path, "/browse/"); ok {
		k, _, _ := strings.Cut(rest, "/")
		if k = strings.ToUpper(k); keyRE.MatchString(k) {
			return k
		}
	}
	return ""
}

// Owns implements ports.KnowledgeSource.
func (s *Source) Owns(cc ports.ConnectorConfig, raw string) bool {
	site, err := atlassian.NewSite("jira", cc)
	return err == nil && site.Owns(raw) && IssueKey(raw) != ""
}

// Fetch implements ports.KnowledgeSource.
func (s *Source) Fetch(ctx context.Context, cc ports.ConnectorConfig, raw string) (ports.KnowledgeDoc, error) {
	c, err := s.client(cc)
	if err != nil {
		return ports.KnowledgeDoc{}, err
	}
	key := IssueKey(raw)
	if key == "" {
		return ports.KnowledgeDoc{}, &ports.ValidationError{Code: "INVALID_URL", Message: "not a Jira issue URL"}
	}
	var it issue
	if err := c.site.Get(ctx, c.api("/issue/"+key)+"?fields="+url.QueryEscape(fields), &it); err != nil {
		return ports.KnowledgeDoc{}, err
	}
	return c.doc(it), nil
}
