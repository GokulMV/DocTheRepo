package settings

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// CheckOptions controls Check.
type CheckOptions struct {
	// Resolver, when set, resolves every secret reference to prove it exists; values are discarded.
	Resolver *Resolver
	// RequireOwner fails a file that names no owner, so a fresh Hub is never claimed by whoever signs in first.
	RequireOwner bool
}

// CheckReport is what Check found. It holds reference names and emails, never secret values.
type CheckReport struct {
	References []string `json:"references"`
	Owners     []string `json:"owners"`
	Admins     []string `json:"admins"`
	SSO        bool     `json:"sso"`
	Password   *bool    `json:"password,omitempty"`
	Resolved   bool     `json:"resolved"`
	Problems   []string `json:"problems"`
}

// OK reports whether Check found no problems.
func (r CheckReport) OK() bool { return len(r.Problems) == 0 }

// Check validates settings files without contacting a Hub: syntax, unknown keys, required fields, and
// whether the file brings its own owner. With a Resolver it also resolves every secret reference, so a CI
// job can fail before deploying a Hub whose secrets are missing. Parse errors are returned as an error;
// everything else is a problem in the report.
func Check(ctx context.Context, srcs []Source, opts CheckOptions) (CheckReport, error) {
	rep := CheckReport{References: []string{}, Owners: []string{}, Admins: []string{}, Problems: []string{}}
	doc, err := Load(ctx, srcs, nil)
	if err != nil {
		return rep, err
	}
	seen := map[string]bool{}
	for _, s := range srcs {
		var root yaml.Node
		if err := yaml.Unmarshal(s.Data, &root); err != nil {
			return rep, fmt.Errorf("%s: %w", s.Name, err)
		}
		walkValues(&root, func(v string) { // values only: a reference in a comment is not one
			for _, m := range refRE.FindAllString(v, -1) {
				if !strings.HasPrefix(m, "$$") && !seen[m] {
					seen[m] = true
					rep.References = append(rep.References, m)
				}
			}
		})
	}
	sort.Strings(rep.References)
	if opts.Resolver != nil {
		rep.Resolved = true
		for _, ref := range rep.References {
			if _, err := opts.Resolver.Expand(ctx, ref); err != nil {
				rep.Problems = append(rep.Problems, err.Error())
			}
		}
	}

	var domains []string
	if doc.Auth != nil {
		rep.Password = doc.Auth.Password
		if doc.Auth.SSO != nil {
			rep.SSO = true
			for _, d := range doc.Auth.SSO.AllowedDomains {
				domains = append(domains, strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "@")))
			}
		}
	}
	for _, u := range doc.Users {
		email := strings.ToLower(strings.TrimSpace(u.Email))
		if u.Disabled != nil && *u.Disabled {
			continue
		}
		switch u.Role {
		case "owner":
			rep.Owners = append(rep.Owners, email)
		case "admin":
			rep.Admins = append(rep.Admins, email)
		default:
			continue
		}
		if !strings.Contains(email, "@") {
			rep.Problems = append(rep.Problems, fmt.Sprintf("users: %q is not an email address", u.Email))
			continue
		}
		if len(domains) > 0 && !slices.Contains(domains, email[strings.LastIndexByte(email, '@')+1:]) {
			rep.Problems = append(rep.Problems, fmt.Sprintf("users: %s (%s) is outside auth.sso.allowed_domains %v, so single sign-on will refuse them",
				email, u.Role, domains))
		}
	}
	if opts.RequireOwner && len(rep.Owners) == 0 {
		rep.Problems = append(rep.Problems, "users: no owner listed; add one with role: owner, "+
			"or the first person from the allowed domains to sign in owns the Hub")
	}
	return rep, nil
}

// walkValues calls fn with every scalar value (not mapping keys) under n.
func walkValues(n *yaml.Node, fn func(string)) {
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			walkValues(c, fn)
		}
	case yaml.MappingNode:
		for i := 1; i < len(n.Content); i += 2 {
			walkValues(n.Content[i], fn)
		}
	case yaml.ScalarNode:
		fn(n.Value)
	}
}
