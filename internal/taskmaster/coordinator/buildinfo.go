package coordinator

// BuildTime is stamped at build time via `-ldflags "-X
// .../coordinator.BuildTime=<UTC timestamp>"` (see the Makefile). Exposed
// through GET /api/health so "which build is this server actually running"
// is one glance at the UI instead of a file-mtime forensic exercise — this
// exists because a stale frontend bundle once looked identical to a stale
// backend, and there was no way to tell them apart without digging.
//
// Left as "dev" for `go run`/`go test`/any build that doesn't pass the
// ldflag, which is an honest, obviously-not-a-real-timestamp value.
var BuildTime = "dev"
