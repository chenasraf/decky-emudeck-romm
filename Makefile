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

# Runs the tests that talk to the RomM server named in .env (ROMM_URL).
.PHONY: test-live
test-live:
	dotenvx run -f .env -- go -C backend test -run Live -count=1 -v ./...

.PHONY: lint
lint:
	pnpm lint
	pnpm format:check
	pnpm typecheck
	$(MAKE) -C backend lint

# Copies a build onto a Steam Deck over SSH and restarts Decky's loader, which reloads the plugin
# without leaving Gaming Mode. The plugin folder is root's, and sudo needs a terminal for its
# password, so the files land in a folder of the deck user's first and one ssh -t moves them.
DECK ?= deck@deck
DECK_PLUGIN_DIR := /home/deck/homebrew/plugins/decky-emudeck-romm
DECK_STAGE := .cache/decky-emudeck-romm-deploy

.PHONY: deploy
deploy:
	pnpm build
	$(MAKE) -C backend out/backend-amd64
	ssh $(DECK) mkdir -p $(DECK_STAGE)/bin
	rsync -a main.py plugin.json package.json dist $(DECK):$(DECK_STAGE)/
	rsync -a backend/out/backend-amd64 $(DECK):$(DECK_STAGE)/bin/
	ssh -t $(DECK) 'sudo rsync -a $(DECK_STAGE)/ $(DECK_PLUGIN_DIR)/ && sudo systemctl restart plugin_loader'

# Builds the installable zip into out/ the way the plugin store does. Needs Docker and the decky
# CLI on PATH.
.PHONY: package
package:
	decky plugin build -o out -s directory .
