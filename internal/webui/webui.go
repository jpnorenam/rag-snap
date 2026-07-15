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
	"path"
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

		if resolved := resolvePath(assets, upath); resolved != upath {
			r = r.Clone(r.Context())
			r.URL.Path = resolved
		}
		fileServer.ServeHTTP(w, r)
	}), nil
}

// resolvePath decides which asset path http.FileServer should serve for a
// requested (slash-trimmed) path, so that statically-exported routes survive a
// deep link or reload:
//   - a real file           → serve it as-is;
//   - a route directory      → serve that directory's index.html (returned with a
//     trailing slash so http.FileServer serves the index without a 301 redirect
//     that would drop the /ui prefix);
//   - anything else          → the SPA root index.html for client-side routing.
//
// The returned value is a slash-rooted URL path (or the input unchanged when the
// file exists as-is). Next.js emits per-route directories like
// knowledge/index.html, which is why a bare directory must not fall straight
// through to the root index.
func resolvePath(assets fs.FS, upath string) string {
	if assetExists(assets, upath) {
		return upath
	}
	dir := strings.TrimSuffix(upath, "/")
	if dir != "" && assetExists(assets, dir+"/index.html") {
		return "/" + dir + "/"
	}
	return "/"
}

// assetExists reports whether name (slash-rooted, no leading slash) resolves to
// a regular file in the embedded FS. Directories do not count as assets: they
// are handled by hasIndex.
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

// hasIndex reports whether name is a directory holding an index.html — an
// exported route page such as status/ or prompts/.
func hasIndex(assets fs.FS, name string) bool {
	return assetExists(assets, path.Join(name, "index.html"))
}
