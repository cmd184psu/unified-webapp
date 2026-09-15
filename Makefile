BINARY    := unified-webapp
CMD       := ./cmd/server
CONFIG    := ~/.unified-webapp.json

# Stamped into the taskmaster coordinator's BuildTime var, exposed via
# GET /api/health — lets the UI (and `curl .../api/health`) show which build
# is actually running, instead of guessing from file mtimes. `go run` and any
# build not going through this Makefile leave BuildTime at its "dev" default.
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -X cmd184psu/unified-webapp/internal/taskmaster/coordinator.BuildTime=$(BUILD_TIME)

.PHONY: run build build-rpi test clean clean-local-test-db init-config web typecheck

run:
	go run $(CMD) -config $(CONFIG)

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)
	go build -o taskmasterctl ./cmd/taskmasterctl

build-rpi:
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BINARY)-arm64-linux $(CMD)
	GOOS=linux GOARCH=arm64 go build -o taskmasterctl-arm64-linux ./cmd/taskmasterctl

test:
	go test -race ./...

init-config:
	go run $(CMD) -init-config -config $(CONFIG)

clean:
	rm -f $(BINARY) $(BINARY)-arm64-linux taskmasterctl taskmasterctl-arm64-linux

# Wipes ONLY the local-test taskmaster SQLite DB (not build artifacts —
# deliberately separate from `clean`, so rebuilding never silently discards
# test data). Useful after a hard-killed local-test server leaves stale rows
# (ReconcileOrphanedExecutions fixes the "stuck running forever" case on the
# next boot, but this is here for a full reset when you just want one).
clean-local-test-db:
	rm -f local-test/data/taskmaster/taskmaster.db local-test/data/taskmaster/taskmaster.db-wal local-test/data/taskmaster/taskmaster.db-shm

web:
	@if [ ! -d node_modules ]; then npm install; fi
	npm run build

typecheck:
	npm run typecheck
