package signals

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

// CustomPattern is an operator-defined PII pattern (customer IDs, account numbers). Name becomes the
// placeholder: {Name: "CUSTOMER_ID"} → <CUSTOMER_ID>.
type CustomPattern struct {
	Name  string `json:"name"`
	Regex string `json:"regex"`
}

// Redactor removes personal data. It is applied to model input only when the provider's "redact personal
// data" toggle is on (plan § 8.8): you run the model, so you decide per provider.
type Redactor struct {
	custom []compiledPattern
}

type compiledPattern struct {
	placeholder string
	re          *regexp.Regexp
}

var (
	emailRE = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)
	// Phones: E.164 (+ and 7–15 digits) or grouped national formats with separators, so bare numbers
	// (IDs, counts, timestamps) are left alone.
	phoneRE = regexp.MustCompile(`(?:\+\d{1,3}[\s.-]?)?\(?\b\d{3}\)?[\s.-]\d{3}[\s.-]\d{4}\b|\+\d{7,15}\b`)
	ipv4RE  = regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\.){3}(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\b`)
	// IPv6 candidates are validated with net.ParseIP (times like 12:30:45 are not addresses).
	ipv6RE = regexp.MustCompile(`(?i)(?:[0-9a-f]{0,4}:){2,7}[0-9a-f]{0,4}(?:%[0-9a-z]+)?`)
	// Card candidates: 13–19 digits, optionally grouped by spaces or dashes; kept only if Luhn-valid.
	cardRE = regexp.MustCompile(`\b\d(?:[ -]?\d){12,18}\b`)
)

// NewRedactor compiles operator patterns (RE2 syntax).
func NewRedactor(custom []CustomPattern) (*Redactor, error) {
	r := &Redactor{}
	for _, p := range custom {
		name := strings.ToUpper(strings.TrimSpace(p.Name))
		if name == "" || p.Regex == "" {
			return nil, fmt.Errorf("custom PII pattern needs a name and a regex")
		}
		re, err := regexp.Compile(p.Regex)
		if err != nil {
			return nil, fmt.Errorf("custom PII pattern %s: %w", name, err)
		}
		r.custom = append(r.custom, compiledPattern{placeholder: "<" + name + ">", re: re})
	}
	return r, nil
}

// Redact replaces personal data with typed placeholders. Run Scrub first (secrets are always removed).
func (r *Redactor) Redact(s string) string {
	if s == "" {
		return s
	}
	for _, p := range r.custom {
		s = p.re.ReplaceAllString(s, p.placeholder)
	}
	s = emailRE.ReplaceAllString(s, "<EMAIL>")
	s = cardRE.ReplaceAllStringFunc(s, func(m string) string {
		if luhn(m) {
			return "<CARD>"
		}
		return m
	})
	s = ipv6RE.ReplaceAllStringFunc(s, func(m string) string {
		host := m
		if i := strings.IndexByte(host, '%'); i >= 0 {
			host = host[:i]
		}
		if strings.Count(host, ":") >= 2 && net.ParseIP(host) != nil && strings.ContainsAny(strings.ToLower(host), "abcdef:") && looksV6(host) {
			return "<IP>"
		}
		return m
	})
	s = ipv4RE.ReplaceAllString(s, "<IP>")
	s = phoneRE.ReplaceAllString(s, "<PHONE>")
	return s
}

// looksV6 rejects clock-like strings that ParseIP might accept in compressed form (e.g. "1::2" is an
// address, "12:30:45" is not parsed at all); an address needs "::" or all eight groups.
func looksV6(s string) bool {
	return strings.Contains(s, "::") || strings.Count(s, ":") == 7
}

// luhn reports whether the digits in s (separators ignored) pass the Luhn checksum.
func luhn(s string) bool {
	var digits []int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			digits = append(digits, int(c-'0'))
		}
	}
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	sum := 0
	for i := len(digits) - 1; i >= 0; i-- {
		d := digits[i]
		if (len(digits)-1-i)%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}
