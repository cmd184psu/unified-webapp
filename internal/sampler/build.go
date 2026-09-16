// Package sampler implements the sampler module -- a shared-theme and
// shared-component demo page (FR-10). It has no data, no auth logic of its
// own, and no background work; Build only wires static serving.
package sampler

import (
	"net/http"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/static"
)

// Build returns a ready-to-use http.Handler for the sampler module.
//
// This is the documented per-module static.MountShared example
// (docs/adding-a-module.md): the dispatcher-level static.WithShared from
// cmd/server/main.go already covers /shared/ for every host including
// sampler's, so this mount is redundant at runtime -- deliberately. It keeps
// MountShared exercised by a real module and is the migration path for any
// module that later wants the /shared/ subtree inside its own middleware.
func Build(cfg config.SamplerConfig) (http.Handler, error) {
	mux := http.NewServeMux()
	if err := static.MountShared(mux, cfg.SharedStaticDir); err != nil {
		return nil, err
	}
	mux.Handle("/", static.NewHandler(cfg.StaticDir))

	return mux, nil
}
