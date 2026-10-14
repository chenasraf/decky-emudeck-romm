# Development

How the plugin is put together, and how to build, test and run it.

## Layout

- `src/` — the frontend (TypeScript, React), shown in the Quick Access Menu and the full-screen
  library and setup pages.
- `main.py` — supervises the backend binary and relays calls to it
  ([ADR 1](docs/adr/0001-stdio-json-rpc-relay.md)). It also runs the binary's `uninstall` and
  `reset` commands while the backend is stopped.
- `backend/` — the Go backend, a self-contained module built into `bin/backend-amd64` and
  `bin/backend-arm64`. `main.py` picks one by CPU architecture.
- `dev/` — the browser playground: a stand-in for Decky and mocks of `@decky/api` and `@decky/ui`.
- `docs/` — the user guide; `docs/adr/` holds architecture decisions.

## Requirements

Node with pnpm, Go, Docker, [lefthook](https://lefthook.dev) and
[golangci-lint](https://golangci-lint.run). `make package` also needs the decky CLI, and
`make test-live` needs [dotenvx](https://dotenvx.com).

## Commands

```sh
pnpm install
make install-hooks
pnpm dev         # the plugin in a browser at http://localhost:5174, backed by the real main.py and backend
make build       # frontend into dist/, backend binaries into backend/out/
make test
make test-live   # the tests that talk to the RomM server named in .env (ROMM_URL)
make lint
make package     # the installable zip into out/, built like the plugin store does
make deploy      # build, copy onto a Steam Deck over SSH and reload Decky (DECK=deck@deck)
```

The pre-commit hooks run gofmt, go vet, golangci-lint, the Go tests, prettier, eslint, tsc, and
check that the generated TypeScript schema is current.

## Running on a Deck

`make deploy` needs SSH on the Deck, set up once in Desktop Mode: set a password with `passwd`, then
run `sudo systemctl enable --now sshd`. From then on the Deck can stay in Gaming Mode; each deploy
restarts Decky, not Steam. It asks for the Deck's sudo password, unless `DECK_PWD` holds it.

## The browser playground

`pnpm dev` runs `dev/bridge.py`, which loads `main.py` with a stand-in for Decky's `decky` module
(its folders live in `dev/.data/`) and builds the backend for your machine. `@decky/api` and
`@decky/ui` are swapped for browser versions in `dev/mocks/`, so the UI is close to the Quick Access
Menu but not pixel-exact. Editing Go code rebuilds and restarts the backend; editing `main.py`
restarts the bridge. The page's **Kill backend** button shows the supervisor recovering.

`dev/fake-deck.sh` builds a stand-in Deck home under `dev/.data/home`, with EmuDeck and ES-DE laid
out as on a Deck, for the backend to probe. `--no-emudeck`, `--no-roms`, `--gamelist-only` and
`--no-esde` leave parts out, so the setup status page has something to flag.

## The RPC contract

The contract between frontend and backend lives in `backend/internal/adapters/rpc/schema`. After
changing it, run `pnpm generate` to regenerate `src/rpc/schema.gen.ts`, and bump `ProtocolVersion`
there and `PROTOCOL_VERSION` in `main.py` for incompatible changes.

## Where the plugin keeps its data

| What                                   | Where                                          |
| -------------------------------------- | ---------------------------------------------- |
| Settings, server and Device            | `settings.json` in Decky's plugin settings dir |
| RomM token                             | `token` beside it, readable only by its owner  |
| Records of downloaded and linked games | `state.db` in Decky's plugin runtime dir       |
| Library cache and covers               | `cache.db` and `covers/` in the runtime dir    |
| Save backups                           | `backups/<rom id>/` in the runtime dir         |
| Downloads in progress                  | `.romm-staging/` in the Emulation folder       |

`state.db` migrates forward on start, backing itself up as `state.db.bak-<version>` first; a file
from a newer version is refused rather than opened.
