package settings

import (
	"context"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Export reads the Hub's current settings as a Document. Secrets cannot be read back: each one that is
// set becomes a ${env:…} reference with a suggested variable name, for you to point at your secret store.
func Export(ctx context.Context, api API) (Document, error) {
	st, err := fetchState(ctx, api)
	if err != nil {
		return Document{}, err
	}
	doc := Document{Version: Version}
	exportAuth(ctx, api, &doc)
	names := map[string]string{}
	for _, p := range st.providers {
		names[p.ID] = p.Name
		on, redact := p.Enabled, p.RedactPII
		dp := Provider{Name: p.Name, Kind: p.Kind, BaseURL: p.BaseURL, Extra: emptyNil(p.Extra), APIKey: envRef(p.Name, "API_KEY")}
		if !on {
			dp.Enabled = &on
		}
		if redact {
			dp.RedactPII = &redact
		}
		doc.Providers = append(doc.Providers, dp)
	}
	var full struct {
		Items []struct {
			remoteConnector
			HasCredentials   bool `json:"has_credentials"`
			HasWebhookSecret bool `json:"has_webhook_secret"`
		}
	}
	if err := api.Do(ctx, "GET", "/connectors", nil, &full); err != nil {
		return Document{}, err
	}
	for _, c := range full.Items {
		names[c.ID] = c.Name
		dc := Connector{Name: c.Name, Type: c.Type, Mode: c.Mode, PollSeconds: c.PollSeconds, Config: emptyNil(c.Config)}
		if c.HasCredentials {
			dc.Credentials = envRef(c.Name, "CREDENTIALS")
		}
		if c.HasWebhookSecret {
			dc.WebhookSecret = envRef(c.Name, "WEBHOOK_SECRET")
		}
		if !c.Enabled {
			off := false
			dc.Enabled = &off
		}
		doc.Connectors = append(doc.Connectors, dc)
	}
	for _, r := range st.routes {
		if doc.Routes == nil {
			doc.Routes = map[string]Route{}
		}
		rt := Route{Provider: names[r.ProviderID], Model: r.Model, Effort: r.Effort, MaxOutputTokens: r.MaxOutputTokens,
			ContextTokenBudget: r.ContextTokenBudget, Temperature: r.Temperature}
		if r.FallbackProviderID != nil && *r.FallbackProviderID != "" {
			rt.Fallback = &Fallback{Provider: names[*r.FallbackProviderID], Model: r.FallbackModel}
		}
		doc.Routes[r.Feature] = rt
	}
	for _, r := range st.repos {
		names[r.ID] = r.FullName
		dr := Repo{FullName: r.FullName, Connector: names[r.ConnectorID], TrackedBranch: r.TrackedBranch, DocsPath: r.DocsPath,
			PushMode: r.Push.Mode, OnReject: r.Push.OnReject, Approver: r.Push.Approver, PRConflictStrategy: r.Push.ConflictStrategy,
			ServiceName: r.ServiceName}
		if !r.Enabled {
			off := false
			dr.Enabled = &off
		}
		doc.Repos = append(doc.Repos, dr)
	}
	if len(st.limits) > 0 {
		doc.Spend = &Spend{}
		for _, l := range st.limits {
			key := l.ScopeKey
			if n, ok := names[key]; ok {
				key = n
			}
			doc.Spend.Limits = append(doc.Spend.Limits, SpendLimit{Scope: l.Scope, Key: key, Window: l.Window, MaxTokens: l.MaxTokens,
				MaxCostUSD: l.MaxCostUSD, OnBreach: l.OnBreach, AlertURL: l.AlertURL})
		}
	}
	return doc, nil
}

// Marshal writes doc as YAML.
func Marshal(doc Document) ([]byte, error) {
	b, err := yaml.Marshal(doc)
	if err != nil {
		return nil, err
	}
	header := "# DocTheRepo Hub settings. Apply with `dth apply -f <file>` or Administration → Settings file.\n" +
		"# Secrets are references: ${env:NAME}, ${file:path#key}, ${vault:path#key}, ${gopass:path}, ${awssm:id#key}, ${gcpsm:project/secret}.\n"
	return append([]byte(header), b...), nil
}

var nonWord = regexp.MustCompile(`[^A-Za-z0-9]+`)

func envRef(name, suffix string) Secret {
	v := strings.Trim(strings.ToUpper(nonWord.ReplaceAllString(name, "_")), "_")
	return Secret{Value: "${env:" + v + "_" + suffix + "}", Set: true}
}

func emptyNil(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}
