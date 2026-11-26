VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X github.com/samuelbailey123/sonar/internal/version.Version=$(VERSION) \
           -X github.com/samuelbailey123/sonar/internal/version.Commit=$(COMMIT) \
           -X github.com/samuelbailey123/sonar/internal/version.Date=$(DATE)

BIN := sonar

.PHONY: build install test lint clean check release

## build: compile the sonar binary with version metadata
build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) .

## install: install sonar into $GOPATH/bin
install:
	go install -ldflags "$(LDFLAGS)" .

## test: run all tests with race detection
test:
	go test -race -count=1 ./...

## lint: run go vet
lint:
	go vet ./...

## clean: remove build artifacts
clean:
	rm -f $(BIN)

## check: run vet and tests (CI-equivalent local gate)
check:
	go vet ./...
	go test -short -race -count=1 ./...

## release: build a local snapshot release via goreleaser (no publish)
release:
	goreleaser release --snapshot --clean
