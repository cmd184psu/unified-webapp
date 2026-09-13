BINARY    := unified-webapp
CMD       := ./cmd/server
CONFIG    := ~/.unified-webapp.json

.PHONY: run build build-rpi test clean init-config web typecheck

run:
	go run $(CMD) -config $(CONFIG)

build:
	go build -o $(BINARY) $(CMD)
	go build -o taskmasterctl ./cmd/taskmasterctl

build-rpi:
	GOOS=linux GOARCH=arm64 go build -o $(BINARY)-arm64-linux $(CMD)
	GOOS=linux GOARCH=arm64 go build -o taskmasterctl-arm64-linux ./cmd/taskmasterctl

test:
	go test -race ./...

init-config:
	go run $(CMD) -init-config -config $(CONFIG)

clean:
	rm -f $(BINARY) $(BINARY)-arm64-linux taskmasterctl taskmasterctl-arm64-linux

web:
	@if [ ! -d node_modules ]; then npm install; fi
	npm run build

typecheck:
	npm run typecheck
