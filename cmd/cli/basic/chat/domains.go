package chat

import (
	"fmt"
	"path"
	"strings"
)

// DomainSet is a compiled, validated batch manifest domains list, ready for
// per-question lookup. The zero value and a nil pointer are both usable and
// resolve every question to no domain, so the no-domains path costs one nil
// check.
type DomainSet struct {
	entries []compiledDomain
}

// compiledDomain pairs a manifest entry with the values Resolve needs on every
// lookup: the uppercased pattern (matching is case-insensitive) and the number
// of literal, non-wildcard characters in it, which is the specificity metric.
type compiledDomain struct {
	domain   Domain
	pattern  string
	literals int
}

// CompileDomains validates a manifest's domains list and compiles it for
// lookup. It rejects an entry with no match pattern, an entry carrying neither
// context nor keywords (it would have nothing to contribute), a malformed glob,
// and two entries declaring the same pattern — the last so the pattern recorded
// on a result identifies exactly one entry. Since matching is case-insensitive,
// patterns differing only in case count as duplicates.
//
// A nil or empty list compiles without error to a set that resolves everything
// to no domain.
func CompileDomains(domains []Domain) (*DomainSet, error) {
	if len(domains) == 0 {
		return &DomainSet{}, nil
	}

	set := &DomainSet{entries: make([]compiledDomain, 0, len(domains))}
	seen := make(map[string]int, len(domains))

	for i, d := range domains {
		d.Match = strings.TrimSpace(d.Match)
		if d.Match == "" {
			return nil, fmt.Errorf("domains entry %d has an empty match pattern", i+1)
		}
		if strings.TrimSpace(d.Context) == "" && len(d.Keywords) == 0 {
			return nil, fmt.Errorf("domains entry %d (match %q) has neither context nor keywords", i+1, d.Match)
		}

		pattern := strings.ToUpper(d.Match)
		// path.Match only reports a bad pattern once it reaches the offending
		// syntax, so probe it against a non-empty name.
		if _, err := path.Match(pattern, "X"); err != nil {
			return nil, fmt.Errorf("domains entry %d has an invalid match pattern %q: %w", i+1, d.Match, err)
		}
		if first, exists := seen[pattern]; exists {
			return nil, fmt.Errorf("domains entries %d and %d declare the same match pattern %q", first+1, i+1, d.Match)
		}
		seen[pattern] = i

		set.entries = append(set.entries, compiledDomain{
			domain:   d,
			pattern:  pattern,
			literals: literalCount(pattern),
		})
	}

	return set, nil
}

// Len reports how many entries the set carries.
func (s *DomainSet) Len() int {
	if s == nil {
		return 0
	}
	return len(s.entries)
}

// Resolve returns the domain that applies to a question, or nil if none does.
//
// The question's id is matched first. Where more than one entry matches, the
// entry with the most literal characters in its pattern wins ("J1.*" beats
// "J*"), ties going to the earlier entry in document order. When the id is a
// bare sequence number — which carries no domain prefix — and matched nothing,
// the question's source is tried instead. An id that matched is never
// re-matched against its source.
//
// The returned pointer aliases the set's own copy of the entry; treat it as
// read-only.
func (s *DomainSet) Resolve(id, source string) *Domain {
	if s == nil || len(s.entries) == 0 {
		return nil
	}
	if d := s.match(id); d != nil {
		return d
	}
	if source != "" && isBareSequence(id) {
		return s.match(source)
	}
	return nil
}

// match returns the most specific entry matching key, or nil.
func (s *DomainSet) match(key string) *Domain {
	upper := strings.ToUpper(key)
	best := -1
	for i := range s.entries {
		e := &s.entries[i]
		// A bad pattern was rejected at compile time, so an error here is not
		// reachable; treat one as a non-match rather than panicking mid-batch.
		if ok, err := path.Match(e.pattern, upper); err != nil || !ok {
			continue
		}
		// Strictly greater, so a tie leaves the earlier entry in place.
		if best < 0 || e.literals > s.entries[best].literals {
			best = i
		}
	}
	if best < 0 {
		return nil
	}
	return &s.entries[best].domain
}

// literalCount counts the characters in a glob that are neither of the
// wildcards the schema commits to, "*" and "?". It is the specificity metric
// for picking among matching patterns, so operators do not have to keep the
// list in any particular order.
func literalCount(pattern string) int {
	n := 0
	for _, r := range pattern {
		if r != '*' && r != '?' {
			n++
		}
	}
	return n
}

// isBareSequence reports whether s is a non-empty run of ASCII digits — the
// shape the extractor assigns as an id when a row carries no identifier of its
// own, and the only case where source is consulted for routing.
func isBareSequence(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
