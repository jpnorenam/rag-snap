package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/config"
)

// Service states reported by GET /1.0/status. A service is "not configured" when no
// endpoint can be resolved for it, so an empty config reads as such rather than as a
// failure the user is expected to fix by restarting something.
const (
	serviceRunning       = "running"
	serviceUnreachable   = "unreachable"
	serviceNotConfigured = "not configured"
)

// Service keys in the status payload. They are the client's stable identifiers, so
// they are spelled out here rather than reusing the backend URL map keys ("openai").
const (
	serviceOpenSearch = "opensearch"
	serviceInference  = "inference"
	serviceTika       = "tika"
	serviceRagd       = "ragd"
)

// probeTimeout bounds each service probe. Probes run concurrently, so a fully-down
// stack costs one timeout rather than the sum of them.
const probeTimeout = 3 * time.Second

// serviceStatus is one service's entry in the status payload. Detail fields are
// omitted when absent so a degraded service still renders from the fields it does
// have.
type serviceStatus struct {
	State    string `json:"state"`
	Endpoint string `json:"endpoint,omitempty"`
	// Error carries the probe failure, for the diagnostics the page shows next to an
	// unreachable service.
	Error string `json:"error,omitempty"`

	// OpenSearch detail.
	Models         []configuredModel         `json:"models,omitempty"`
	DeployedModels []knowledge.DeployedModel `json:"deployed_models,omitempty"`

	// Inference detail.
	LLMModel string `json:"llm_model,omitempty"`

	// Tika detail.
	Version string `json:"version,omitempty"`

	// ragd detail.
	APIVersion string     `json:"api_version,omitempty"`
	Listeners  *listeners `json:"listeners,omitempty"`
}

// configuredModel pairs a model ID from config with whether OpenSearch actually has
// it deployed. A configured-but-undeployed model is the failure this endpoint exists
// to make visible: retrieval breaks, but nothing else reports it until a search runs.
type configuredModel struct {
	Role     string `json:"role"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Deployed bool   `json:"deployed"`
}

// listeners describes the daemon's own listening surfaces. The localhost token is
// deliberately absent: status is a diagnostics view, not a credential handoff.
type listeners struct {
	Socket   string `json:"socket,omitempty"`
	Loopback string `json:"loopback,omitempty"`
}

// swagger:route GET /1.0/status status statusGet
//
// Report service health and models.
//
// Probes each external service (OpenSearch, the inference server, Tika) over HTTP at
// request time and reports the daemon's own listeners. Services degrade
// independently: an unreachable service is reported as such and never fails the
// request. The OpenSearch entry lists the ML models OpenSearch currently has
// deployed, alongside the configured model IDs, so a configured model that is not
// deployed is visible before a search fails on it.
//
//	Responses:
//	  200: syncResponse
//	  403: errorResponse
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var (
		wg                             sync.WaitGroup
		openSearch, inference, tikaSvc serviceStatus
	)

	wg.Add(3)
	go func() {
		defer wg.Done()
		openSearch = s.probeOpenSearch(ctx)
	}()
	go func() {
		defer wg.Done()
		inference = s.probeInference()
	}()
	go func() {
		defer wg.Done()
		tikaSvc = s.probeTika(ctx)
	}()
	wg.Wait()

	respondSync(w, map[string]serviceStatus{
		serviceOpenSearch: openSearch,
		serviceInference:  inference,
		serviceTika:       tikaSvc,
		serviceRagd:       s.ragdStatus(),
	})
}

// probeOpenSearch reports the knowledge store's health, the configured embedding and
// rerank model IDs, and — when reachable — the models OpenSearch has deployed. The
// configured IDs are reported even when OpenSearch is down: they come from config,
// and hiding them would remove exactly the detail needed to diagnose the outage.
func (s *Server) probeOpenSearch(ctx context.Context) serviceStatus {
	status := serviceStatus{
		Endpoint: s.backends.urls[backendOpenSearch],
		Models:   s.configuredModels(),
	}
	if status.Endpoint == "" {
		status.State = serviceNotConfigured
		return status
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	// Fail fast: a status probe must answer "unreachable" within the timeout rather
	// than sit in the build path's wait-for-ready retry loop.
	client, err := s.clients.openSearchClientNoWait(ctx)
	if err != nil {
		status.State = serviceUnreachable
		status.Error = err.Error()
		return status
	}
	if err := client.Ping(ctx); err != nil {
		status.State = serviceUnreachable
		status.Error = err.Error()
		return status
	}
	status.State = serviceRunning

	deployed, err := client.ListDeployedModels(ctx)
	if err != nil {
		// The store is up but the ML plugin did not answer. That is a detail
		// failure, not an outage: keep the service running and say what was lost.
		status.Error = "listing deployed models: " + err.Error()
		return status
	}
	status.DeployedModels = deployed

	// Mark each configured model deployed or not, now that we know what is deployed.
	for i, model := range status.Models {
		for _, d := range deployed {
			if d.ID == model.ID {
				status.Models[i].Deployed = true
				break
			}
		}
	}

	return status
}

// configuredModels returns the embedding and rerank models named in config, in the
// order the CLI's status command prints them. A model that is not configured is
// omitted rather than reported as an empty ID.
func (s *Server) configuredModels() []configuredModel {
	models := make([]configuredModel, 0, 2)

	for _, m := range []struct {
		role string
		key  string
		name string
	}{
		{"embedding", knowledge.ConfEmbeddingModelID, knowledge.DefaultSentenceTransformerName},
		{"rerank", knowledge.ConfRerankModelID, knowledge.DefaultCrossEncoderName},
	} {
		id, _ := config.GetString(s.ctx.Config, m.key)
		if id == "" {
			continue
		}
		models = append(models, configuredModel{Role: m.role, ID: id, Name: m.name})
	}

	return models
}

// probeInference reports the chat backend's health and the model it serves. The
// model listing doubles as the health check: a server that cannot name a model
// cannot answer a chat turn either. It takes no context because chat.FindModelName —
// the same probe the CLI's status command uses — carries its own.
func (s *Server) probeInference() serviceStatus {
	status := serviceStatus{Endpoint: s.backends.urls[backendOpenAI]}
	if status.Endpoint == "" {
		status.State = serviceNotConfigured
		return status
	}

	name, err := chat.FindModelName(status.Endpoint)
	if err != nil {
		// Some OpenAI-compatible backends (notably AWS Bedrock) don't implement
		// GET /models: it 404s while /chat/completions works. That is a reachable
		// server, not a down one, so health is taken from the explicitly
		// configured chat.model — the same value the daemon's chat and answer
		// paths use when /models is unavailable. With no model configured we can't
		// name one, so we surface actionable guidance rather than a bare 404.
		if chat.ModelListingUnsupported(err) {
			if model, _ := config.GetString(s.ctx.Config, confChatModel); model != "" {
				status.State = serviceRunning
				status.LLMModel = model
				return status
			}
			status.State = serviceUnreachable
			status.Error = fmt.Sprintf(
				"inference server has no model-listing endpoint; set the model name with `sudo rag-cli.rag set --package %s=<model>` (e.g. a Bedrock model id)",
				confChatModel)
			return status
		}
		status.State = serviceUnreachable
		status.Error = err.Error()
		return status
	}

	status.State = serviceRunning
	status.LLMModel = name
	return status
}

// probeTika reports the text-extraction service's health and version. Tika answers
// /version with a bare version string, so a successful read is both the health check
// and the detail.
func (s *Server) probeTika(ctx context.Context) serviceStatus {
	status := serviceStatus{Endpoint: s.backends.urls[backendTika]}
	if status.Endpoint == "" {
		status.State = serviceNotConfigured
		return status
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	version, err := tikaVersion(ctx, status.Endpoint)
	if err != nil {
		status.State = serviceUnreachable
		status.Error = err.Error()
		return status
	}

	status.State = serviceRunning
	status.Version = version
	return status
}

// tikaVersion fetches the Tika server's version string from its /version endpoint.
// The configured Tika URL carries a path component (tika.http.path, "/tika"), but
// /version is served from the server root, so the path is stripped — the same reason
// processing.NewTikaClient reduces the URL to scheme://host:port.
func tikaVersion(ctx context.Context, baseURL string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid tika URL: %w", err)
	}
	versionURL := fmt.Sprintf("%s://%s/version", u.Scheme, u.Host)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, versionURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("version request failed with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

// ragdStatus reports the daemon itself. It is never probed — it is the process
// answering the request — so it is always running.
func (s *Server) ragdStatus() serviceStatus {
	l := &listeners{Socket: s.socket.Path}
	if s.loopback.Enabled {
		l.Loopback = s.loopbackListenAddr
	}

	return serviceStatus{
		State:      serviceRunning,
		APIVersion: apiVersion,
		Listeners:  l,
	}
}
