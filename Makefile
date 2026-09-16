BINARY    := unified-webapp
CMD       := ./cmd/server
CONFIG    := ~/.unified-webapp.json

# Stamped into the taskmaster coordinator's BuildTime var, exposed via
# GET /api/health — lets the UI (and `curl .../api/health`) show which build
# is actually running, instead of guessing from file mtimes. `go run` and any
# build not going through this Makefile leave BuildTime at its "dev" default.
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -X cmd184psu/unified-webapp/internal/taskmaster/coordinator.BuildTime=$(BUILD_TIME)

.PHONY: run build build-rpi test clean clean-local-test-db init-config web typecheck web-verify gates test-web check

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
	npm ci
	npm run build

typecheck:
	npm run typecheck

# Every gate below is a script file invoked as ONE process, so its exit code is
# the gate, and no recipe here contains a make substitution. Two reasons, both
# observed: make expands a substitution inside a recipe as a make variable, so
# a shell test wrapped around one sees an empty string and passes whatever the
# tree contains; and a shell-function substitution discards the child's exit
# status, so a crashed generator reads as an empty list. The gate scripts carry
# the details. (docs/PLAN-ui-unification-phase1.md §1 G11, §5 A7.17.)

web-verify:
	node scripts/gates/artifacts.mjs

gates:
	node scripts/check-shared-css.mjs
	node scripts/gates/bundle-shape.mjs
	node scripts/gates/token-overlap.mjs

test-web:
	npm ci
	npm run typecheck
	npm run test:web

check: web-verify test-web gates test
