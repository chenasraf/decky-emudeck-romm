# 1. The backend speaks JSON-RPC over stdio, relayed by main.py

## Context

The plugin's logic lives in a Go binary. Decky only runs Python (`main.py`) and exposes it to the
frontend through `callable` and `addEventListener`, so something has to carry calls and events
between Python and the binary.

Plugins in the store with native backends use one of three channels: the frontend talks to the
binary over localhost HTTP or WebSocket, `main.py` relays to it over localhost HTTP, or `main.py`
relays over a Unix socket. None use stdio.

Decky's service runs with `KillMode=process`, and a plugin process that doesn't stop within 5 s is
SIGKILLed, so a child the plugin fails to stop keeps running, orphaned.

## Decision

`main.py` spawns the binary and talks JSON-RPC 2.0 to it over stdin/stdout, one message per line.

- The binary exits when stdin reaches EOF. Whenever the plugin process dies, however it dies, the
  binary follows.
- `main.py` exposes one generic `call(method, params)` that forwards without parsing params, plus
  native callables (`backend_status`, `restart_backend`) that work while the binary is down.
- Notifications from the binary are re-emitted to the frontend with `decky.emit`.
- stdout carries only the protocol; the binary logs to stderr, which `main.py` writes to
  `decky.logger`.
- On start the binary sends a `ready` notification with its build version and protocol version.
  `main.py` holds calls until then and refuses a binary whose protocol version differs from its own.

## Consequences

- No ports or socket files: nothing to allocate, collide, clean up after a crash, or expose to other
  local processes.
- No orphaned backend after a crash or a forced plugin stop.
- One reader in `main.py` multiplexes responses and notifications; when the binary exits, every
  in-flight call fails.
- The binary can't be poked with curl; its stdio protocol is exercised by an end-to-end test that
  spawns it.
- Decky has no call timeout, so `main.py` enforces one (15 s) to keep a hung binary from hanging the
  UI.
