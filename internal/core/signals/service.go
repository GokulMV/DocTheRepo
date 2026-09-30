package signals

import (
	"path"
	"strings"
)

// Unknown is the service of events nothing maps.
const Unknown = "unknown"

// serviceTags are explicit service attributes, most specific first (plan § 8.8).
var serviceTags = []string{"service", "service.name", "dd.service", "k8s.label.app", "kubernetes.labels.app", "app.kubernetes.io/name", "app", "job"}

// ServiceRule maps signal attributes to a service (the service_map table's source_patterns): each key is an
// attribute (log_group, gcp.resource.labels.service_name, k8s.namespace, consumer_group, queue, …) and
// each value a glob (path.Match syntax, case-insensitive) that must match it.
type ServiceRule struct {
	Service  string
	Patterns map[string]string
}

// ResolveService returns the event's service: an explicit value, then an explicit tag, then the first
// service_map rule whose every pattern matches, else Unknown.
func ResolveService(explicit string, attrs map[string]string, rules []ServiceRule) string {
	if s := strings.TrimSpace(explicit); s != "" && s != Unknown {
		return s
	}
	for _, k := range serviceTags {
		if v := strings.TrimSpace(attrs[k]); v != "" {
			return v
		}
	}
	for _, r := range rules {
		if len(r.Patterns) == 0 {
			continue
		}
		ok := true
		for k, pat := range r.Patterns {
			v, present := attrs[k]
			if !present {
				ok = false
				break
			}
			if m, err := path.Match(strings.ToLower(pat), strings.ToLower(v)); err != nil || !m {
				ok = false
				break
			}
		}
		if ok {
			return r.Service
		}
	}
	return Unknown
}
