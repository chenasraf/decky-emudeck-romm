BIN := backend
ARCHES := amd64 arm64

# The plugin version lives in package.json. `decky plugin build` mounts the plugin root at /plugin
# and this folder at /backend, so the parent folder isn't reachable inside the container.
PACKAGE_JSON := $(firstword $(wildcard ../package.json /plugin/package.json))
VERSION ?= $(shell sed -n 's/^ *"version": *"\([^"]*\)".*/\1/p' $(PACKAGE_JSON))
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all
all: $(addprefix out/$(BIN)-,$(ARCHES))

.PHONY: FORCE
out/$(BIN)-%: FORCE
	CGO_ENABLED=0 GOOS=linux GOARCH=$* go build -trimpath -ldflags "$(LDFLAGS)" -o $@ ./cmd/backend

.PHONY: test
test:
	go test ./...

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: generate
generate:
	go generate ./...

.PHONY: clean
clean:
	rm -rf out
