// Package contract holds the versioned JSON contracts between the Hub and pluggable engines — DocGen v2
// (external agent CLIs), issue decoding, and cited answers — with their JSON Schemas and a small
// validator. It is importable by third-party adapter authors without vendoring Hub internals.
package contract

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// Schema is a JSON Schema document (the subset this package validates: type, properties, required,
// additionalProperties:false, items, enum, const, min/maxLength, min/maxItems, minimum, pattern).
type Schema = map[string]any

// Validate checks a JSON document against schema and returns every problem, each prefixed with its
// JSON path ("$.docs[0].path"). A nil result means valid. Unknown fields are allowed unless the schema
// sets additionalProperties:false — the contracts evolve additively and ignore unknown fields.
func Validate(schema Schema, doc []byte) []string {
	var v any
	dec := json.NewDecoder(strings.NewReader(string(doc)))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return []string{"$: not valid JSON: " + err.Error()}
	}
	if dec.More() {
		return []string{"$: trailing content after the JSON document"}
	}
	var probs []string
	validate(schema, v, "$", &probs)
	return probs
}

func validate(s Schema, v any, path string, probs *[]string) {
	add := func(format string, args ...any) { *probs = append(*probs, path+": "+fmt.Sprintf(format, args...)) }
	if t, ok := s["type"]; ok && !typeMatches(t, v) {
		add("expected %v, got %s", t, typeName(v))
		return
	}
	if c, ok := s["const"]; ok && fmt.Sprint(norm(v)) != fmt.Sprint(c) {
		add("must equal %v", c)
	}
	if e, ok := s["enum"].([]any); ok {
		found := false
		for _, x := range e {
			if fmt.Sprint(norm(v)) == fmt.Sprint(x) {
				found = true
			}
		}
		if !found {
			add("must be one of %v", e)
		}
	}
	switch x := v.(type) {
	case string:
		if n, ok := num(s["minLength"]); ok && float64(len([]rune(x))) < n {
			add("must be at least %v characters", n)
		}
		if n, ok := num(s["maxLength"]); ok && float64(len([]rune(x))) > n {
			add("must be at most %v characters", n)
		}
		if p, ok := s["pattern"].(string); ok {
			if re, err := regexp.Compile(p); err == nil && !re.MatchString(x) {
				add("must match pattern %s", p)
			}
		}
	case json.Number:
		f, _ := x.Float64()
		if n, ok := num(s["minimum"]); ok && f < n {
			add("must be >= %v", n)
		}
		if n, ok := num(s["maximum"]); ok && f > n {
			add("must be <= %v", n)
		}
	case []any:
		if n, ok := num(s["minItems"]); ok && float64(len(x)) < n {
			add("must have at least %v items", n)
		}
		if n, ok := num(s["maxItems"]); ok && float64(len(x)) > n {
			add("must have at most %v items", n)
		}
		if items, ok := s["items"].(Schema); ok {
			for i, it := range x {
				validate(items, it, fmt.Sprintf("%s[%d]", path, i), probs)
			}
		}
	case map[string]any:
		if req, ok := s["required"].([]any); ok {
			for _, r := range req {
				if _, present := x[fmt.Sprint(r)]; !present {
					add("missing required field %q", r)
				}
			}
		}
		props, _ := s["properties"].(Schema)
		if ap, ok := s["additionalProperties"].(bool); ok && !ap {
			var extra []string
			for k := range x {
				if _, known := props[k]; !known {
					extra = append(extra, k)
				}
			}
			sort.Strings(extra)
			for _, k := range extra {
				add("unexpected field %q", k)
			}
		}
		keys := make([]string, 0, len(props))
		for k := range props {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if sub, ok := props[k].(Schema); ok {
				if val, present := x[k]; present {
					validate(sub, val, path+"."+k, probs)
				}
			}
		}
	}
}

func typeMatches(t any, v any) bool {
	switch tt := t.(type) {
	case string:
		return typeIs(tt, v)
	case []any:
		for _, x := range tt {
			if typeIs(fmt.Sprint(x), v) {
				return true
			}
		}
	case []string:
		for _, x := range tt {
			if typeIs(x, v) {
				return true
			}
		}
	}
	return false
}

func typeIs(t string, v any) bool {
	switch t {
	case "null":
		return v == nil
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "number":
		_, ok := v.(json.Number)
		return ok
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		f, err := n.Float64()
		return err == nil && f == math.Trunc(f)
	case "array":
		_, ok := v.([]any)
		return ok
	case "object":
		_, ok := v.(map[string]any)
		return ok
	}
	return false
}

func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case json.Number:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}

func norm(v any) any {
	if n, ok := v.(json.Number); ok {
		if f, err := n.Float64(); err == nil {
			return f
		}
	}
	return v
}

func num(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

// ExtractJSON returns the JSON object in a model reply, tolerating a surrounding ```json fence or prose
// before/after the object. Only the outermost {...} is returned.
func ExtractJSON(text string) []byte {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "```") {
		if i := strings.IndexByte(t, '\n'); i >= 0 {
			t = t[i+1:]
		}
		t = strings.TrimSuffix(strings.TrimSpace(t), "```")
	}
	start := strings.IndexByte(t, '{')
	end := strings.LastIndexByte(t, '}')
	if start < 0 || end < start {
		return []byte(t)
	}
	return []byte(t[start : end+1])
}
