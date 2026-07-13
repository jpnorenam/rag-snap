package api

import (
	"context"
	"log"
	"net"
	"net/url"
	"sync"
	"time"
)

// backendState tracks whether each external service (OpenSearch, the inference
// server, Tika) is currently reachable. Readiness is polled in the background so
// the API listener can start immediately; handlers consult Ready before using a
// backend and return a "backend unavailable" error when it is not yet up.
//
// Phase 1 (skeleton) only establishes reachability by TCP-dialling each
// configured host:port. The typed clients (OpenSearchClient, the openai client,
// the Tika client) are constructed by the feature handlers added in later phases.
type backendState struct {
	mu    sync.RWMutex
	ready map[string]bool
	urls  map[string]string
}

func newBackendState(urls map[string]string) *backendState {
	ready := make(map[string]bool, len(urls))
	for name := range urls {
		ready[name] = false
	}
	return &backendState{ready: ready, urls: urls}
}

// Ready reports whether the named backend (e.g. "opensearch") was reachable at
// the last poll.
func (b *backendState) Ready(name string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.ready[name]
}

// snapshot returns a copy of the current readiness map for diagnostics.
func (b *backendState) snapshot() map[string]bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make(map[string]bool, len(b.ready))
	for k, v := range b.ready {
		out[k] = v
	}
	return out
}

// poll runs a readiness loop until ctx is cancelled, dialling each backend's
// host:port and recording reachability. It never blocks the listener.
func (b *backendState) poll(ctx context.Context, interval time.Duration) {
	check := func() {
		for name, raw := range b.urls {
			reachable := dialable(ctx, raw)
			b.mu.Lock()
			changed := b.ready[name] != reachable
			b.ready[name] = reachable
			b.mu.Unlock()
			if changed {
				log.Printf("backend %q readiness: %v", name, reachable)
			}
		}
	}

	check() // probe once immediately
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

// dialable reports whether the host:port of a service URL accepts a TCP connection.
func dialable(ctx context.Context, raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	host := u.Host
	if u.Port() == "" {
		// Fall back to the scheme default so a missing explicit port still dials.
		switch u.Scheme {
		case "https":
			host = net.JoinHostPort(u.Hostname(), "443")
		default:
			host = net.JoinHostPort(u.Hostname(), "80")
		}
	}

	dialCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(dialCtx, "tcp", host)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
