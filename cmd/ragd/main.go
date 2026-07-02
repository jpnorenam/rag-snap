// Command ragd is the rag-cli daemon. It serves the REST API over a local unix
// socket, owning the long-lived backend clients and (in later phases) the async
// operations registry and chat sessions. Configuration is read from the
// snapctl-backed store at startup and re-read on SIGHUP; secrets come from the
// environment (OPENSEARCH_USERNAME/PASSWORD, CHAT_API_KEY).
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/jpnorenam/rag-snap/internal/api"
	"github.com/jpnorenam/rag-snap/pkg/storage"
)

func main() {
	log.SetFlags(0)

	if err := run(); err != nil {
		log.Fatalf("ragd: %v", err)
	}
}

func run() error {
	// Cancel the root context on SIGTERM/SIGINT for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Reload config on SIGHUP. We surface the signal as a context the server can
	// watch; phase 1 logs and re-resolves on the next start cycle.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)

	appCtx := &common.Context{Config: storage.NewConfig()}

	for {
		if err := serveOnce(ctx, hup, appCtx); err != nil {
			return err
		}
		// serveOnce returns nil with ctx still live only on a reload request.
		if ctx.Err() != nil {
			return nil
		}
		log.Println("reloading configuration")
	}
}

// serveOnce resolves config, builds the server, and serves until either the root
// context is cancelled (shutdown) or a SIGHUP is received (reload). On SIGHUP it
// cancels the current server and returns nil so the caller can re-resolve config.
func serveOnce(ctx context.Context, hup <-chan os.Signal, appCtx *common.Context) error {
	backendURLs, err := api.ResolveBackendURLs(appCtx)
	if err != nil {
		return err
	}
	socket := api.ResolveSocketConfig(appCtx)
	loopback := api.ResolveLoopbackConfig(appCtx)

	srv := api.New(api.Options{
		Context:     appCtx,
		Socket:      socket,
		Loopback:    loopback,
		BackendURLs: backendURLs,
	})

	// runCtx is cancelled either by shutdown (parent ctx) or by a reload (SIGHUP).
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-hup:
			cancel()
		case <-runCtx.Done():
		}
	}()

	log.Printf("serving API on %s (group=%s, mode=%o)", socket.Path, socket.Group, socket.Mode)
	if loopback.Enabled {
		// The resolved port (when api.loopback.address uses :0) is logged by the
		// server once the listener is bound; here we log the configured target.
		log.Printf("loopback API enabled on %s", loopback.Address)
	}
	return srv.Serve(runCtx)
}
