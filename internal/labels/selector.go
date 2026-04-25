// Package labels parses Kubernetes-style equality-only label selectors of
// the shape "k=v[,k=v...]". Inequality, set-based, and existence operators
// are deliberately not supported — phase-1 only needs equality.
package labels

import (
	"fmt"
	"strings"
)

type Requirement struct {
	Key   string
	Value string
}

type Selector []Requirement

// ParseSelector accepts an empty string (matches everything) or a
// comma-separated list of "k=v" requirements. Whitespace around tokens is
// trimmed; empty tokens (trailing comma) are ignored.
func ParseSelector(s string) (Selector, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make(Selector, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		eq := strings.IndexByte(p, '=')
		if eq <= 0 || eq == len(p)-1 {
			return nil, fmt.Errorf("invalid requirement %q (want k=v)", p)
		}
		k := strings.TrimSpace(p[:eq])
		v := strings.TrimSpace(p[eq+1:])
		if k == "" || v == "" {
			return nil, fmt.Errorf("invalid requirement %q (empty key or value)", p)
		}
		if _, dup := seen[k]; dup {
			return nil, fmt.Errorf("duplicate key %q in selector", k)
		}
		seen[k] = struct{}{}
		out = append(out, Requirement{Key: k, Value: v})
	}
	return out, nil
}
