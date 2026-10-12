# EmuDeck RomM Sync

A [Decky Loader](https://decky.xyz) plugin that turns a [RomM](https://romm.app) server into a
library for an [EmuDeck](https://www.emudeck.com) install: it places ROMs and BIOS in EmuDeck's
folders and keeps saves in sync with RomM.

## Layout

- `src/` — the frontend (TypeScript, React), shown in the Quick Access Menu.
- `main.py` — supervises the backend binary and relays calls to it
  ([ADR 1](docs/adr/0001-stdio-json-rpc-relay.md)).
- `backend/` — the Go backend, a self-contained module built into `bin/backend-amd64` and
  `bin/backend-arm64`. `main.py` picks one by CPU architecture.

## Development

Requirements: Node with pnpm, Go, Docker, [lefthook](https://lefthook.dev) and
[golangci-lint](https://golangci-lint.run).

```sh
pnpm install
make install-hooks
make build       # frontend into dist/, backend binaries into backend/out/
make test
make lint
make package     # the installable zip into out/, built like the plugin store does (needs the decky CLI)
```

The RPC contract lives in `backend/internal/adapters/rpc/schema`. After changing it, run
`pnpm generate` to regenerate `src/rpc/schema.gen.ts`, and bump `ProtocolVersion` there and
`PROTOCOL_VERSION` in `main.py` for incompatible changes.

## License

[GPL-3.0](LICENSE)
