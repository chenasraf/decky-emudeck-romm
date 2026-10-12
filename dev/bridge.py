"""Runs the real main.py outside Decky so the frontend can be developed in a browser.

A stub `decky` module stands in for the loader, with every Decky dir under dev/.data. The Go backend
is built for this machine and spawned by main.py exactly as on a Deck. Frontend calls arrive as
`POST /api/<callable>` with `{"args": [...]}`; `decky.emit` events stream out on `GET /events` as
Server-Sent Events. `pnpm dev` starts this through Vite, which proxies both paths.

Dev-only endpoints: `POST /dev/rebuild` (rebuild the backend and restart it) and `POST /dev/kill`
(SIGKILL the backend, to watch the supervisor recover).
"""

import asyncio
import inspect
import json
import logging
import os
import platform
import signal
import sys
import types
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parent.parent
DATA = ROOT / "dev" / ".data"
PORT = int(os.environ.get("DEV_BRIDGE_PORT", "5175"))

clients: set[asyncio.StreamWriter] = set()


async def emit(event: str, *args: Any) -> None:
    frame = f"data: {json.dumps({'event': event, 'args': list(args)}, default=str)}\n\n".encode()
    for writer in list(clients):
        try:
            writer.write(frame)
            await writer.drain()
        except (ConnectionError, RuntimeError):
            clients.discard(writer)


def install_decky_stub() -> types.ModuleType:
    dirs = {
        "DECKY_PLUGIN_DIR": DATA / "plugin",
        "DECKY_PLUGIN_SETTINGS_DIR": DATA / "settings",
        "DECKY_PLUGIN_RUNTIME_DIR": DATA / "runtime",
        "DECKY_PLUGIN_LOG_DIR": DATA / "logs",
    }
    decky = types.ModuleType("decky")
    for name, path in dirs.items():
        path.mkdir(parents=True, exist_ok=True)
        setattr(decky, name, str(path))
        # The loader exports these to the plugin process, so the backend inherits them.
        os.environ[name] = str(path)
    decky.DECKY_USER_HOME = os.environ["HOME"]
    decky.DECKY_PLUGIN_VERSION = json.loads((ROOT / "package.json").read_text())["version"]
    logging.basicConfig(level=logging.INFO, format="%(levelname)-7s %(message)s", stream=sys.stdout)
    decky.logger = logging.getLogger("plugin")
    decky.emit = emit
    sys.modules["decky"] = decky
    return decky


decky = install_decky_stub()
sys.path.insert(0, str(ROOT))
import main  # noqa: E402

log = decky.logger
plugin = main.Plugin()


def backend_binary() -> Path:
    arch = main.ARCHES[platform.machine().lower()]
    return DATA / "plugin" / "bin" / f"backend-{arch}"


async def build_backend() -> bool:
    log.info("building backend")
    proc = await asyncio.create_subprocess_exec(
        "go", "build", "-ldflags", "-X main.version=dev", "-o", str(backend_binary()), "./cmd/backend",
        cwd=ROOT / "backend",
    )
    ok = await proc.wait() == 0
    if not ok:
        log.error("backend build failed; keeping the previous binary")
    return ok


def callable_named(name: str):
    attr = getattr(plugin, name, None)
    if name.startswith("_") or not inspect.iscoroutinefunction(attr):
        return None
    return attr


async def handle(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
    try:
        request = await reader.readline()
        method, path, _ = request.decode().split(" ", 2)
        headers = {}
        while (line := await reader.readline()) not in (b"\r\n", b"\n", b""):
            key, _, value = line.decode().partition(":")
            headers[key.strip().lower()] = value.strip()
        body = await reader.readexactly(int(headers.get("content-length", 0)))
    except (ValueError, asyncio.IncompleteReadError):
        writer.close()
        return

    if method == "GET" and path == "/events":
        writer.write(b"HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nCache-Control: no-cache\r\n\r\n")
        await writer.drain()
        clients.add(writer)
        return

    if method != "POST":
        return await respond(writer, 405, {"error": "method not allowed"})

    if path == "/dev/rebuild":
        if await build_backend():
            await plugin.restart_backend()
        return await respond(writer, 200, {"result": None})
    if path == "/dev/kill":
        if plugin.backend.proc:
            plugin.backend.proc.kill()
        return await respond(writer, 200, {"result": None})

    name = path.removeprefix("/api/")
    fn = callable_named(name)
    if fn is None:
        return await respond(writer, 404, {"error": f"unknown callable: {name}"})
    args = json.loads(body or b"{}").get("args", [])
    try:
        result = await fn(*args)
    except Exception as e:  # Decky turns any exception into a rejected call with str(e).
        log.warning(f"{name} raised {type(e).__name__}: {e}")
        return await respond(writer, 200, {"error": str(e)})
    await respond(writer, 200, {"result": result})


async def respond(writer: asyncio.StreamWriter, status: int, payload: dict) -> None:
    data = json.dumps(payload, default=str).encode()
    writer.write(
        f"HTTP/1.1 {status} OK\r\nContent-Type: application/json\r\nContent-Length: {len(data)}\r\n"
        "Connection: close\r\n\r\n".encode() + data
    )
    await writer.drain()
    writer.close()


async def stop_when_orphaned(stop: asyncio.Event) -> None:
    # Vite doesn't always get to signal us (Node skips exit handlers on SIGTERM), so notice being
    # reparented instead of outliving it.
    parent = os.getppid()
    while os.getppid() == parent:
        await asyncio.sleep(1)
    stop.set()


async def run() -> None:
    await build_backend()
    server = await asyncio.start_server(handle, "127.0.0.1", PORT)
    await plugin._main()
    log.info(f"dev bridge listening on http://127.0.0.1:{PORT}")

    stop = asyncio.Event()
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, stop.set)
    watchdog = asyncio.create_task(stop_when_orphaned(stop))
    await stop.wait()
    watchdog.cancel()

    log.info("stopping")
    server.close()
    for writer in clients:
        writer.close()
    await plugin._unload()


if __name__ == "__main__":
    asyncio.run(run())
