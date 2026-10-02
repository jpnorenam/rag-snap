package api

import (
	"net/http"
)

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
// once. "configured" is false, with an empty "groups" list, when kapa.ai is
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
	groups, err := s.kapa.ListSourceGroups(r.Context())
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
