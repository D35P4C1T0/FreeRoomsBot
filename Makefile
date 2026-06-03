GOCACHE ?= /tmp/go-cache
GOMODCACHE ?= /tmp/go-mod

.PHONY: test race vet build docker compose-config

test:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test ./...

race:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test -race ./...

vet:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go vet ./...

build:
	mkdir -p bin
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go build -buildvcs=false -o bin/free-classrooms-bot ./cmd/locuspocusbot

docker:
	docker build --network=host .

compose-config:
	docker compose config
