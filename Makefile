.PHONY: all
all: build

.PHONY: install-hooks
install-hooks:
	lefthook install

.PHONY: build
build:
	pnpm build
	$(MAKE) -C backend

.PHONY: generate
generate:
	pnpm generate

.PHONY: test
test:
	$(MAKE) -C backend test

.PHONY: lint
lint:
	pnpm lint
	pnpm format:check
	pnpm typecheck
	$(MAKE) -C backend lint

# Builds the installable zip into out/ the way the plugin store does. Needs Docker and the decky
# CLI on PATH.
.PHONY: package
package:
	decky plugin build -o out -s directory .
