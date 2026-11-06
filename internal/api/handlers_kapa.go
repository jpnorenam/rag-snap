package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
)

// kapaGroupsTTL is how long a source-group listing is reused. Listing walks
// every page of the project's sources upstream, and the groups rarely change,
// while the UI asks for them each time a screen with a picker opens. A config
// change needs a ragd restart (the kapa client is resolved at startup), which
// also clears the cache.
const kapaGroupsTTL = 5 * time.Minute

// kapaGroupsCache holds the last successful source-group listing. Failures are
// not cached, so the next request retries upstream.
type kapaGroupsCache struct {
	mu      sync.Mutex
	groups  []knowledge.SourceGroup
	fetched time.Time
	now     func() time.Time // time.Now; replaced in tests
}

// get returns the cached listing while it is fresh, or fetches a new one.
// Concurrent misses may each fetch; the last one to finish is kept.
func (c *kapaGroupsCache) get(ctx context.Context, fetch func(context.Context) ([]knowledge.SourceGroup, error)) ([]knowledge.SourceGroup, error) {
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	c.mu.Lock()
	if c.groups != nil && now().Sub(c.fetched) < kapaGroupsTTL {
		groups := c.groups
		c.mu.Unlock()
		return groups, nil
	}
	c.mu.Unlock()

	groups, err := fetch(ctx)
	if err != nil {
		return nil, err
	}
	if groups == nil {
		groups = []knowledge.SourceGroup{} // cache "no groups" too
	}
	c.mu.Lock()
	c.groups, c.fetched = groups, now()
	c.mu.Unlock()
	return groups, nil
}

// kapaSourceGroup is the API view of a kapa.ai source group: the id a client
// sends back as a selection (kapa_source_groups) and the name it displays.
type kapaSourceGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// kapaSourceGroupsView is the GET /1.0/kapa/source-groups response. Configured
// is false when kapa.ai is disabled or its project id or API key is missing,
// so a client can show a configuration hint instead of an empty picker; a
// configured project with no groups is configured: true with an empty list.
type kapaSourceGroupsView struct {
	Configured bool              `json:"configured"`
	Groups     []kapaSourceGroup `json:"groups"`
}

// swagger:route GET /1.0/kapa/source-groups kapa kapaSourceGroups
//
// List kapa.ai source groups.
//
// Lists the configured kapa.ai project's source groups, each with its id (the
// value to send in a manifest's "kapa_source_groups") and display name. The
// listing follows the upstream pagination to completion and reports each group
// once. A successful listing is reused for a few minutes, so a group added
// upstream can take that long to appear (or restart ragd). "configured" is false, with an empty "groups" list, when kapa.ai is
// disabled or its project id (kapa.project.id / KAPA_PROJECT_ID) or API key
// (KAPA_API_KEY in the daemon's environment) is missing. A failing upstream
// request (rejected credentials, unreachable host, unexpected status) returns
// a 502 rather than an empty list.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
//	  502: errorResponse
func (s *Server) handleKapaSourceGroups(w http.ResponseWriter, r *http.Request) {
	view := kapaSourceGroupsView{Groups: []kapaSourceGroup{}}
	if s.kapa == nil {
		respondSync(w, view)
		return
	}
	groups, err := s.kapaGroups.get(r.Context(), s.kapa.ListSourceGroups)
	if err != nil {
		respondError(w, http.StatusBadGateway, err.Error())
		return
	}
	view.Configured = true
	for _, g := range groups {
		view.Groups = append(view.Groups, kapaSourceGroup{ID: g.ID, Name: g.Name})
	}
	respondSync(w, view)
}
