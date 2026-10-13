package analysis

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
)

// KnowledgeBaseEntry is one selectable knowledge base as it exists right now.
type KnowledgeBaseEntry struct {
	Name string `json:"name"`
	// Docs is the document count, included because it is the only signal the model
	// has about whether a base is substantial or nearly empty.
	Docs string `json:"docs,omitempty"`
}

// KapaGroupEntry is one selectable hosted-documentation source group. The identifier
// is opaque, which is the whole reason the catalogue exists: a model cannot guess or
// remember one, so it must be given the real values to choose from.
type KapaGroupEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Catalogue is the live set of retrieval targets a manifest may select, read from the
// running services at request time.
//
// It is never assumed or remembered. A name that is not in the catalogue retrieves
// nothing at run time and reports no error, so letting a model invent one produces a
// manifest that looks correct and answers badly.
type Catalogue struct {
	KnowledgeBases []KnowledgeBaseEntry `json:"knowledge_bases"`
	KapaGroups     []KapaGroupEntry     `json:"kapa_source_groups"`
}

// LoadCatalogue reads the catalogue from the running services.
//
// The knowledge store is required: failing here names the unreachable service, which
// is a better outcome than generating a manifest against an empty catalogue that would
// select nothing. Kapa is optional — it is legitimately not configured — so an error
// reading its groups leaves the list empty rather than failing the request.
func LoadCatalogue(ctx context.Context, kc *knowledge.OpenSearchClient, kapa *knowledge.KapaClient) (*Catalogue, error) {
	if kc == nil {
		return nil, fmt.Errorf("the knowledge store is not available, so the list of knowledge bases cannot be read; analysis needs it to choose retrieval targets")
	}

	indexes, err := kc.ListIndexes(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing knowledge bases: %w", err)
	}

	cat := &Catalogue{}
	for _, idx := range indexes {
		name, nameErr := knowledge.KnowledgeBaseNameFromIndex(idx.Name)
		if nameErr != nil {
			continue
		}
		cat.KnowledgeBases = append(cat.KnowledgeBases, KnowledgeBaseEntry{Name: name, Docs: idx.DocsCount})
	}
	sort.Slice(cat.KnowledgeBases, func(i, j int) bool {
		return cat.KnowledgeBases[i].Name < cat.KnowledgeBases[j].Name
	})

	if len(cat.KnowledgeBases) == 0 {
		return nil, fmt.Errorf("no knowledge bases exist yet — create and ingest at least one before running analysis, since a manifest with nothing to retrieve from cannot be answered")
	}

	if kapa != nil {
		if groups, kapaErr := kapa.ListSourceGroups(ctx); kapaErr == nil {
			for _, g := range groups {
				cat.KapaGroups = append(cat.KapaGroups, KapaGroupEntry{ID: g.ID, Name: g.Name})
			}
			sort.Slice(cat.KapaGroups, func(i, j int) bool {
				return cat.KapaGroups[i].Name < cat.KapaGroups[j].Name
			})
		}
	}

	return cat, nil
}

// Prompt renders the catalogue for the system turn.
func (c *Catalogue) Prompt() string {
	var b strings.Builder
	b.WriteString("# Available retrieval targets\n\n")
	b.WriteString("Select `knowledge_bases` and `kapa_source_groups` ONLY from the lists below. ")
	b.WriteString("A value that is not listed retrieves nothing and reports no error, so an invented name silently degrades every answer that relied on it.\n\n")

	b.WriteString("## knowledge_bases — use the name exactly as written\n\n")
	for _, kb := range c.KnowledgeBases {
		if kb.Docs != "" {
			fmt.Fprintf(&b, "- %s (%s documents)\n", kb.Name, kb.Docs)
		} else {
			fmt.Fprintf(&b, "- %s\n", kb.Name)
		}
	}

	b.WriteString("\n## kapa_source_groups — use the identifier, not the name\n\n")
	if len(c.KapaGroups) == 0 {
		b.WriteString("(none available — omit kapa_source_groups entirely)\n")
		return b.String()
	}
	for _, g := range c.KapaGroups {
		fmt.Fprintf(&b, "- %s  (identifier: %s)\n", g.Name, g.ID)
	}
	return b.String()
}

// HasKnowledgeBase reports whether name is in the catalogue, case-insensitively.
func (c *Catalogue) HasKnowledgeBase(name string) bool {
	for _, kb := range c.KnowledgeBases {
		if strings.EqualFold(kb.Name, name) {
			return true
		}
	}
	return false
}

// HasKapaGroup reports whether id is a catalogued source-group identifier. Names are
// accepted too, so a model that returned the human-readable name instead of the
// identifier is corrected rather than rejected.
func (c *Catalogue) HasKapaGroup(id string) bool {
	for _, g := range c.KapaGroups {
		if strings.EqualFold(g.ID, id) || strings.EqualFold(g.Name, id) {
			return true
		}
	}
	return false
}

// CanonicalKapaGroup maps a catalogued name or identifier onto the identifier, which
// is what the run actually sends.
func (c *Catalogue) CanonicalKapaGroup(idOrName string) (string, bool) {
	for _, g := range c.KapaGroups {
		if strings.EqualFold(g.ID, idOrName) || strings.EqualFold(g.Name, idOrName) {
			return g.ID, true
		}
	}
	return "", false
}

// FilterKnowledgeBases keeps only catalogued names, canonicalising their spelling.
// Used on triage suggestions, where an uncatalogued name is noise rather than an error.
func (c *Catalogue) FilterKnowledgeBases(names []string) []string {
	var out []string
	for _, n := range names {
		for _, kb := range c.KnowledgeBases {
			if strings.EqualFold(kb.Name, n) {
				out = append(out, kb.Name)
				break
			}
		}
	}
	return out
}

// FilterKapaGroups keeps only catalogued groups, mapping each to its identifier.
func (c *Catalogue) FilterKapaGroups(ids []string) []string {
	var out []string
	for _, id := range ids {
		if canonical, ok := c.CanonicalKapaGroup(id); ok {
			out = append(out, canonical)
		}
	}
	return out
}

// ValidateSelections checks a generated manifest's retrieval targets against the
// catalogue that was actually sent, returning the values that do not resolve.
//
// This is the local half of the contract: the model was told to choose only from the
// catalogue, and this verifies it did rather than trusting that it did.
func (c *Catalogue) ValidateSelections(knowledgeBases, kapaGroups []string) (badKBs, badGroups []string) {
	for _, kb := range knowledgeBases {
		if !c.HasKnowledgeBase(kb) {
			badKBs = append(badKBs, kb)
		}
	}
	for _, g := range kapaGroups {
		if !c.HasKapaGroup(g) {
			badGroups = append(badGroups, g)
		}
	}
	return badKBs, badGroups
}
