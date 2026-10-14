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
# without leaving Gaming Mode. The plugin folder is root's, so the files land in a folder of the
# deck user's first and one sudo moves them. DECK_PWD, when set, is the Deck's sudo password, fed
# to sudo on stdin; without it, sudo asks in an ssh -t terminal. It goes through the environment so
# make never prints it. dev/deck-install.sh does the install and restart.
DECK ?= deck@deck
DECK_STAGE := .cache/decky-emudeck-romm-deploy
DECK_INSTALL := sh .cache/deck-install.sh $(DECK_STAGE)
export DECK_PWD

.PHONY: deploy
deploy:
	pnpm build
	$(MAKE) -C backend out/backend-amd64
	ssh $(DECK) mkdir -p $(DECK_STAGE)/bin
	rsync -a main.py plugin.json package.json dist $(DECK):$(DECK_STAGE)/
	rsync -a backend/out/backend-amd64 $(DECK):$(DECK_STAGE)/bin/
	rsync -a dev/deck-install.sh $(DECK):.cache/
	if [ -n "$$DECK_PWD" ]; then \
		printf '%s\n' "$$DECK_PWD" | ssh $(DECK) 'sudo -S -p "" sh -c "$(DECK_INSTALL)"'; \
	else \
		ssh -t $(DECK) 'sudo sh -c "$(DECK_INSTALL)"'; \
	fi

# Builds the installable zip into out/ the way the plugin store does. Needs Docker and the decky
# CLI on PATH.
.PHONY: package
package:
	decky plugin build -o out -s directory .
