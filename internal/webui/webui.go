// Package webui embeds the built browser UI and serves it over the loopback
// HTTP listener. The static single-page application is compiled into the ragd
// binary via go:embed, so there is no runtime dependency on files on disk
// (design Decision 3, mirroring LXD's uiHTTPDir pattern).
//
// The embedded tree lives under dist/, populated by the build (`make ui` copies
// ui/out into internal/webui/dist). A committed placeholder index.html ensures
// `go build` never fails on a missing embed directory even before the UI is
// built.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// dist holds the built UI assets embedded at compile time.
//
//go:embed all:dist
var dist embed.FS

// Assets returns the embedded UI filesystem rooted at the dist directory.
func Assets() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}

// Handler serves the embedded UI with an index.html SPA fallback. Requests that
// match an embedded asset are served directly; any other path (a client-side
// route, a deep link, a reload) falls back to index.html so the SPA router can
// take over. The handler expects to be mounted such that r.URL.Path is already
// stripped of the /ui/ prefix (see StripPrefix in the server wiring).
func Handler() (http.Handler, error) {
	assets, err := Assets()
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(assets))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Normalise the request path to a clean, slash-rooted asset path.
		upath := strings.TrimPrefix(r.URL.Path, "/")
		if upath == "" {
			upath = "index.html"
		}

		// If the requested path is a real embedded file, serve it. Otherwise
		// fall back to the SPA index so client-side routing works on deep links
		// and reloads. We rewrite to "/" (not "/index.html") because
		// http.FileServer 301-redirects an explicit /index.html to /.
		if !assetExists(assets, upath) {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	}), nil
}

// assetExists reports whether name (slash-rooted, no leading slash) resolves to
// a regular file in the embedded FS. Directories do not count as assets so they
// also fall through to the SPA index.
func assetExists(assets fs.FS, name string) bool {
	f, err := assets.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	return true
}
