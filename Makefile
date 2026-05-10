BINARY    := unified-webapp
CMD       := ./cmd/server
CONFIG    := ~/.unified-webapp.json

.PHONY: run build build-rpi test clean init-config

run:
	go run $(CMD) -config $(CONFIG)

build:
	go build -o $(BINARY) $(CMD)

build-rpi:
	GOOS=linux GOARCH=arm64 go build -o $(BINARY)-arm64-linux $(CMD)

test:
	go test -race ./...

init-config:
	go run $(CMD) -init-config -config $(CONFIG)

clean:
	rm -f $(BINARY) $(BINARY)-arm64-linux
