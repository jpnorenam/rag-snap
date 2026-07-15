package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/config"
	"github.com/jpnorenam/rag-snap/pkg/storage"
)

// redactedValue replaces a secret-shaped config value in every read. The key stays
// listed and writable — it is write-only through the API, not hidden.
const redactedValue = "<redacted>"

// secretKeySuffixes marks config keys whose value must never be read back. Matching
// is on the key's final segment, not on the value, so it is deterministic: a key is
// secret because of what it is, not because of what happens to be stored in it today.
// The service credentials (OPENSEARCH_USERNAME/PASSWORD, CHAT_API_KEY) are
// environment variables and cannot appear here at all; this guards the config keys
// that *are* secrets, today gdrive.client.secret.
var secretKeySuffixes = []string{"secret", "password", "token"}

// configEntry is one key in the config listing: its effective value and the layer
// that value comes from. Layer provenance drives the client's "revert to package
// value" affordance, so it is read per-layer rather than inferred by comparing
// values — an override that happens to equal the package value is still an override.
type configEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Layer string `json:"layer"`
	// PackageValue is the value that a revert would restore, present only on an
	// overridden key. A client confirming a revert has to show what the user is
	// trading away and what they get back, and the effective value alone cannot
	// say what is underneath it.
	PackageValue string `json:"package_value,omitempty"`
}

// configListView is the body of GET /1.0/config. Writable tells the client whether to
// render edit affordances at all, so it never shows a control that is certain to fail.
type configListView struct {
	Writable bool          `json:"writable"`
	Keys     []configEntry `json:"keys"`
}

// configSetRequest is the body of PUT /1.0/config/{key}.
type configSetRequest struct {
	Value string `json:"value"`
}

// swagger:route GET /1.0/config config configList
//
// List the configuration.
//
// Returns the effective configuration, sorted by key, each entry naming the layer it
// resolves from ("user" when an override exists, otherwise "package"). Deprecated
// keys are omitted, as they are in the CLI. Secret-shaped values are redacted:
// the key is listed and remains writable, but its value is never returned.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
//	  500: errorResponse
func (s *Server) handleConfigList(w http.ResponseWriter, _ *http.Request) {
	merged, err := s.ctx.Config.GetAll()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "reading config: "+err.Error())
		return
	}

	userLayer, err := s.ctx.Config.GetAllFromLayer(storage.UserConfig)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "reading user config layer: "+err.Error())
		return
	}

	packageLayer, err := s.ctx.Config.GetAllFromLayer(storage.PackageConfig)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "reading package config layer: "+err.Error())
		return
	}

	entries := make([]configEntry, 0, len(merged))
	for key, value := range merged {
		if config.IsDeprecated(key) {
			continue
		}

		entry := configEntry{
			Key:   key,
			Value: displayValue(key, value),
			Layer: string(storage.PackageConfig),
		}
		if _, overridden := userLayer[key]; overridden {
			entry.Layer = string(storage.UserConfig)
			entry.PackageValue = displayValue(key, packageLayer[key])
		}

		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })

	respondSync(w, configListView{Writable: true, Keys: entries})
}

// swagger:route PUT /1.0/config/{key} config configSet
//
// Set a configuration value.
//
// Writes the value to the user layer, which overrides the package value — the same
// semantics as `rag-cli.rag set <key>=<value>`. A key that does not already exist in
// the merged config is rejected: the API cannot create keys, only override them.
// Deprecated keys are read-only and are rejected too.
//
//	Responses:
//	  200: syncResponse
//	  400: errorResponse
//	  403: errorResponse
//	  500: errorResponse
func (s *Server) handleConfigSet(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")

	var req configSetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if config.IsDeprecated(key) {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("%q is read-only", key))
		return
	}

	// Set rejects a key that does not already exist in the merged config. Report that
	// as a client error rather than a 500: the caller named a key that cannot be set.
	if err := s.ctx.Config.Set(key, req.Value, storage.UserConfig); err != nil {
		if strings.Contains(err.Error(), "unknown key") {
			respondError(w, http.StatusBadRequest,
				fmt.Sprintf("unknown key %q; only existing configuration keys can be set", key))
			return
		}
		respondError(w, http.StatusInternalServerError, "setting config: "+err.Error())
		return
	}

	// Echo the package value the key now overrides, so a client can offer the
	// revert immediately without re-listing the whole config.
	entry := configEntry{
		Key:   key,
		Value: displayValue(key, req.Value),
		Layer: string(storage.UserConfig),
	}
	if packageLayer, err := s.ctx.Config.GetAllFromLayer(storage.PackageConfig); err == nil {
		if pkgValue, found := packageLayer[key]; found {
			entry.PackageValue = displayValue(key, pkgValue)
		}
	}

	respondSync(w, entry)
}

// swagger:route DELETE /1.0/config/{key} config configUnset
//
// Revert a configuration value to its package default.
//
// Drops the key's user-layer override so the package value becomes effective again.
// A key with no override cannot be reverted and is rejected.
//
//	Responses:
//	  200: syncResponse
//	  400: errorResponse
//	  403: errorResponse
//	  500: errorResponse
func (s *Server) handleConfigUnset(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")

	userLayer, err := s.ctx.Config.GetAllFromLayer(storage.UserConfig)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "reading user config layer: "+err.Error())
		return
	}
	if _, overridden := userLayer[key]; !overridden {
		respondError(w, http.StatusBadRequest,
			fmt.Sprintf("%q has no user value to revert", key))
		return
	}

	if err := s.ctx.Config.Unset(key, storage.UserConfig); err != nil {
		respondError(w, http.StatusInternalServerError, "reverting config: "+err.Error())
		return
	}

	// Report the value that is now effective: the package value the revert restored.
	merged, err := s.ctx.Config.GetAll()
	if err != nil {
		respondError(w, http.StatusInternalServerError, "reading config: "+err.Error())
		return
	}

	respondSync(w, configEntry{
		Key:   key,
		Value: displayValue(key, merged[key]),
		Layer: string(storage.PackageConfig),
	})
}

// displayValue renders a config value for the API, redacting secret-shaped keys.
// Values arrive from snapctl as strings or numbers, so non-strings are formatted
// rather than dropped. An unset secret reads as empty rather than redacted: the
// install hook seeds gdrive.client.secret empty, and reporting "<redacted>" for a
// secret nobody has set would be a lie the UI would faithfully repeat.
func displayValue(key string, value any) string {
	rendered := ""
	switch v := value.(type) {
	case nil:
		rendered = ""
	case string:
		rendered = v
	default:
		rendered = fmt.Sprintf("%v", v)
	}

	if rendered != "" && isSecretKey(key) {
		return redactedValue
	}
	return rendered
}

// isSecretKey reports whether a key's value must be redacted on read.
func isSecretKey(key string) bool {
	segments := strings.Split(key, ".")
	last := segments[len(segments)-1]
	return slices.Contains(secretKeySuffixes, last)
}
