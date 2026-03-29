VERSION ?= dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

.PHONY: build test test-int test-e2e vet lint install clean

build:
	go build $(LDFLAGS) -o sd ./cmd/sd/

test:
	go test ./...

test-int:
	go test -tags integration ./...

test-e2e:
	go test -tags e2e ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

install:
	go install $(LDFLAGS) ./cmd/sd/

clean:
	rm -f sd
