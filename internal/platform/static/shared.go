package static

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
)

// sharedPrefix is the URL prefix every /shared/ entry point strips before
// resolving a request against the shared asset tree.
const sharedPrefix = "/shared/"

// sharedHandler serves the shared asset tree rooted at a directory opened
// once, at construction, via os.OpenRoot. Only the dist/ and public/
// subtrees are reachable; anything else -- including a directory that failed
// to open -- is a 404. There is no SPA fallback and no directory listing.
type sharedHandler struct {
	// root is nil when the configured directory could not be opened (e.g.
	// it does not exist yet); every request then 404s rather than the
	// process failing to boot.
	root *os.Root
}

// SharedHandler serves the shared asset tree at dir under the /shared/
// prefix. It strips the prefix itself, so every caller passes unmodified
// request paths. Only the dist/ and public/ subtrees are reachable; anything
// else is 404. There is no SPA fallback and no directory listing: a typo
// under /shared/ is a 404, never someone else's index.html with status 200.
func SharedHandler(dir string) http.Handler {
	root, err := os.OpenRoot(dir)
	if err != nil {
		root = nil
	}
	return &sharedHandler{root: root}
}

func (h *sharedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.root == nil {
		http.NotFound(w, r)
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, sharedPrefix)
	clean := path.Clean("/" + rest)
	rel := strings.TrimPrefix(clean, "/")
	segments := strings.Split(rel, "/")

	// First-segment allowlist: only dist/ and public/ are reachable.
	if segments[0] != "dist" && segments[0] != "public" {
		http.NotFound(w, r)
		return
	}

	// Dotfile refusal: any path segment beginning with "." is a 404, even
	// when it stays inside the root.
	for _, seg := range segments {
		if strings.HasPrefix(seg, ".") {
			http.NotFound(w, r)
			return
		}
	}

	// Root confinement: os.Root.Open refuses any path that leaves the root,
	// symlinked or not, closing the TOCTOU window a lexical-only check
	// leaves open.
	f, err := h.root.Open(rel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if fi.IsDir() {
		http.NotFound(w, r)
		return
	}

	http.ServeContent(w, r, rel, fi.ModTime(), f)
}

// WithShared returns next with /shared/* diverted to SharedHandler(dir).
// dir == "" returns next unchanged.
func WithShared(next http.Handler, dir string) http.Handler {
	if dir == "" {
		return next
	}
	shared := SharedHandler(dir)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, sharedPrefix) {
			shared.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Muxer is the subset of *http.ServeMux that MountShared needs to register
// the shared asset tree.
type Muxer interface {
	Handle(pattern string, h http.Handler)
}

// MountShared registers SharedHandler(dir) at /shared/ on m.
// It is the per-module path, for modules that own a mux and want the
// subtree inside their own middleware. m must be a *http.ServeMux; any
// other Muxer implementation is an error.
func MountShared(m Muxer, dir string) error {
	mux, ok := m.(*http.ServeMux)
	if !ok {
		return fmt.Errorf("static: MountShared requires a *http.ServeMux, got %T", m)
	}
	mux.Handle(sharedPrefix, SharedHandler(dir))
	return nil
}
