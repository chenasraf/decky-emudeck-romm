"""FastAPI bridge that exposes every public async method on ``Plugin``
as ``POST /api/<method>`` and broadcasts ``decky.emit()`` calls over
``WS /events``. Browser playground at :5174 proxies to us at :5175.
"""

from __future__ import annotations

import asyncio
import json
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(REPO_ROOT))
sys.path.insert(0, str(REPO_ROOT / "py_modules"))

# Decky shim must land in sys.modules BEFORE main.py is imported.
from playground.server import decky_shim  # noqa: E402

sys.modules["decky"] = decky_shim

import main as plugin_main  # noqa: E402
from fastapi import FastAPI, WebSocket, WebSocketDisconnect  # noqa: E402
from fastapi.middleware.cors import CORSMiddleware  # noqa: E402
from pydantic import BaseModel  # noqa: E402

app = FastAPI(title="decky-emudeck-romm playground bridge")
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)

_plugin: plugin_main.Plugin | None = None
_ws_clients: set[WebSocket] = set()


async def _emit(event_name, *args):
    decky_shim.logger.debug("emit(%r, args=%d)", event_name, len(args))
    payload = json.dumps({"event": event_name, "args": list(args)}, default=str)
    dead: list[WebSocket] = []
    for ws in list(_ws_clients):
        try:
            await ws.send_text(payload)
        except Exception:
            dead.append(ws)
    for ws in dead:
        _ws_clients.discard(ws)


decky_shim.emit = _emit


@app.on_event("startup")
async def _startup():
    global _plugin
    _plugin = plugin_main.Plugin()
    await _plugin._main()
    decky_shim.logger.info("Plugin booted; %d callables available", _callable_count())


@app.on_event("shutdown")
async def _shutdown():
    if _plugin is not None:
        await _plugin._unload()


def _is_callable(name: str, attr: object) -> bool:
    if name.startswith("_"):
        return False
    return asyncio.iscoroutinefunction(attr)


def _callable_count() -> int:
    if _plugin is None:
        return 0
    return sum(1 for n in dir(_plugin) if _is_callable(n, getattr(_plugin, n, None)))


class InvokeBody(BaseModel):
    args: list = []


@app.get("/api/_callables")
async def list_callables():
    if _plugin is None:
        return {"callables": []}
    return {"callables": sorted(n for n in dir(_plugin) if _is_callable(n, getattr(_plugin, n, None)))}


@app.post("/api/{method}")
async def invoke(method: str, body: InvokeBody):
    if _plugin is None:
        return {"error": "plugin not initialized"}
    attr = getattr(_plugin, method, None)
    if attr is None or not _is_callable(method, attr):
        return {"error": f"unknown callable: {method!r}"}
    try:
        result = await attr(*body.args)
    except Exception as e:  # noqa: BLE001 — surface backend errors to browser
        decky_shim.logger.exception("callable %r raised", method)
        return {"error": f"{type(e).__name__}: {e}"}
    return {"result": result}


@app.websocket("/events")
async def events(ws: WebSocket):
    await ws.accept()
    _ws_clients.add(ws)
    try:
        while True:
            await ws.receive_text()
    except WebSocketDisconnect:
        pass
    finally:
        _ws_clients.discard(ws)
