package ports

// RepoConfig is a tracked repository and its docs settings.
type RepoConfig struct {
	ID               string     `json:"id"`
	ConnectorID      string     `json:"connector_id"`
	ConnectorType    string     `json:"connector_type"`
	FullName         string     `json:"full_name"`
	DefaultBranch    string     `json:"default_branch"`
	TrackedBranch    string     `json:"tracked_branch,omitempty"`
	DocsPath         string     `json:"docs_path"`
	LastProcessedSHA string     `json:"last_processed_sha,omitempty"`
	ServiceName      string     `json:"service_name,omitempty"`
	Enabled          bool       `json:"enabled"`
	Push             PushConfig `json:"push"`
}

// Branch is the branch whose pushes are documented (tracked branch, else the default branch).
func (r RepoConfig) Branch() string {
	if r.TrackedBranch != "" {
		return r.TrackedBranch
	}
	return r.DefaultBranch
}

// ConnectorConfig is a connector with decrypted credentials, as adapters are built from it.
type ConnectorConfig struct {
	ID            string
	Type          string
	Name          string
	Mode          string // webhook | poll | both
	PollSeconds   int64
	Config        map[string]string
	Credentials   string
	WebhookSecret string
}
