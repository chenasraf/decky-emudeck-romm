"""Supervisor for the Go backend.

Decky runs this module and routes frontend calls to `Plugin`. The real work happens in
`bin/backend-<arch>`, spawned here and spoken to over stdio with line-delimited JSON-RPC 2.0. The
backend exits when its stdin closes, so it can't outlive this process.
"""

import asyncio
import itertools
import json
import os
import platform
import time
from typing import Any, Optional

import decky

# Must match ProtocolVersion in backend/internal/adapters/rpc/schema. A backend reporting anything
# else is refused rather than half-working against this relay.
PROTOCOL_VERSION = 1

CALL_TIMEOUT = 15.0
READY_TIMEOUT = 10.0
STOP_TIMEOUT = 3.0  # Decky SIGKILLs the plugin 5 s after asking it to stop.
BACKOFF_START = 1.0
BACKOFF_MAX = 30.0
CRASH_LIMIT = 5
CRASH_WINDOW = 300.0
MAX_LINE = 16 * 1024 * 1024

ARCHES = {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}


class BackendUnavailable(Exception):
    pass


class RpcError(Exception):
    """A JSON-RPC error from the backend. str() is the error object as JSON, which the frontend
    parses to switch on data.kind."""

    def __init__(self, error: dict):
        super().__init__(json.dumps(error))
        self.error = error


def binary_path() -> str:
    machine = platform.machine().lower()
    arch = ARCHES.get(machine)
    if arch is None:
        raise RuntimeError(f"unsupported architecture: {machine}")
    return os.path.join(decky.DECKY_PLUGIN_DIR, "bin", f"backend-{arch}")


class Backend:
    """Runs the backend process, restarting it with backoff, and multiplexes calls over stdio."""

    def __init__(self):
        self.state = "starting"  # starting | ready | restarting | down | refused | stopped
        self.reason: Optional[str] = None
        self.version: Optional[str] = None
        self.proc: Optional[asyncio.subprocess.Process] = None
        self.pending: dict[int, asyncio.Future] = {}
        self.ids = itertools.count(1)
        self.ready = asyncio.Event()
        self.wake = asyncio.Event()
        self.crashes: list[float] = []
        self.restart_requested = False
        self.stopping = False

    def status(self) -> dict:
        return {"state": self.state, "version": self.version, "reason": self.reason}

    async def run(self) -> None:
        backoff = BACKOFF_START
        while not self.stopping:
            started = time.monotonic()
            await self._run_once()
            if self.stopping:
                break
            if self.restart_requested:
                self.restart_requested = False
                self.wake.clear()
                backoff = BACKOFF_START
                continue
            if self.state == "refused":
                await self._wait_for_restart()
                backoff = BACKOFF_START
                continue

            now = time.monotonic()
            if now - started > CRASH_WINDOW:
                backoff = BACKOFF_START
            self.crashes = [t for t in self.crashes if now - t < CRASH_WINDOW] + [now]
            if len(self.crashes) >= CRASH_LIMIT:
                decky.logger.error(f"backend crashed {len(self.crashes)} times, giving up")
                await self._set_state("down", "The backend keeps crashing.")
                await self._wait_for_restart()
                backoff = BACKOFF_START
                continue

            await self._set_state("restarting", self.reason)
            decky.logger.warning(f"restarting backend in {backoff:g} s")
            try:
                await asyncio.wait_for(self.wake.wait(), backoff)
            except asyncio.TimeoutError:
                pass
            self.wake.clear()
            backoff = min(backoff * 2, BACKOFF_MAX)

    async def _wait_for_restart(self) -> None:
        await self.wake.wait()
        self.wake.clear()
        self.crashes = []

    async def _run_once(self) -> None:
        self.ready.clear()
        self.version = None
        await self._set_state("starting")
        try:
            path = binary_path()
            try:
                # Decky normally sets this on install; dev deploys copied by hand may not.
                os.chmod(path, 0o755)
            except OSError:
                pass
            env = dict(os.environ)
            # Decky's bundled libraries; anything the backend execs must use the system ones.
            env.pop("LD_LIBRARY_PATH", None)
            self.proc = await asyncio.create_subprocess_exec(
                path,
                stdin=asyncio.subprocess.PIPE,
                stdout=asyncio.subprocess.PIPE,
                stderr=asyncio.subprocess.PIPE,
                env=env,
                limit=MAX_LINE,
            )
        except (OSError, RuntimeError) as e:
            decky.logger.error(f"failed to start backend: {e}")
            self.reason = str(e)
            return

        decky.logger.info(f"backend started, pid {self.proc.pid}")
        stderr = asyncio.create_task(self._pipe_stderr(self.proc.stderr))
        handshake = asyncio.create_task(self._expect_ready(self.proc))
        await self._read_stdout(self.proc.stdout)
        code = await self.proc.wait()
        # Calls made while the backend restarts wait for the next handshake.
        self.ready.clear()
        handshake.cancel()
        await stderr
        self.proc = None
        self._fail_pending("backend exited")

        if not self.stopping and not self.restart_requested and self.state != "refused":
            self.reason = f"Backend exited with code {code}."
            decky.logger.error(f"backend exited with code {code}")

    async def _expect_ready(self, proc: asyncio.subprocess.Process) -> None:
        try:
            await asyncio.wait_for(self.ready.wait(), READY_TIMEOUT)
        except asyncio.TimeoutError:
            decky.logger.error("backend sent no ready handshake, killing it")
            proc.kill()

    async def _read_stdout(self, stream: asyncio.StreamReader) -> None:
        while True:
            try:
                line = await stream.readline()
            except ValueError:
                decky.logger.error("backend sent an oversized line, killing it")
                if self.proc:
                    self.proc.kill()
                return
            if not line:
                return
            try:
                msg = json.loads(line)
            except json.JSONDecodeError:
                decky.logger.error(f"backend wrote a non-JSON line: {line[:200]!r}")
                continue
            if "id" in msg and ("result" in msg or "error" in msg):
                self._resolve(msg)
            elif "method" in msg:
                await self._on_notification(msg["method"], msg.get("params"))

    def _resolve(self, msg: dict) -> None:
        fut = self.pending.pop(msg["id"], None)
        if fut is None or fut.done():
            return
        if "error" in msg:
            fut.set_exception(RpcError(msg["error"]))
        else:
            fut.set_result(msg.get("result"))

    async def _on_notification(self, method: str, params: Any) -> None:
        if method == "ready":
            await self._handshake(params or {})
        else:
            await decky.emit(method, params)

    async def _handshake(self, params: dict) -> None:
        version = params.get("version")
        protocol = params.get("protocol")
        if protocol != PROTOCOL_VERSION:
            decky.logger.error(
                f"refusing backend {version}: protocol {protocol}, this plugin speaks {PROTOCOL_VERSION}"
            )
            await self._set_state(
                "refused",
                f"Backend {version} speaks protocol {protocol}; this plugin needs {PROTOCOL_VERSION}.",
            )
            self._close_stdin()
            return
        self.version = version
        self.reason = None
        decky.logger.info(f"backend {version} ready (protocol {protocol})")
        self.ready.set()
        await self._set_state("ready")

    async def _pipe_stderr(self, stream: asyncio.StreamReader) -> None:
        while line := await stream.readline():
            text = line.decode(errors="replace").rstrip()
            if "level=ERROR" in text:
                decky.logger.error(f"[backend] {text}")
            elif "level=WARN" in text:
                decky.logger.warning(f"[backend] {text}")
            else:
                decky.logger.info(f"[backend] {text}")

    async def _set_state(self, state: str, reason: Optional[str] = None) -> None:
        self.state = state
        self.reason = reason
        await decky.emit("backend_status", self.status())

    def _fail_pending(self, why: str) -> None:
        for fut in self.pending.values():
            if not fut.done():
                fut.set_exception(BackendUnavailable(why))
        self.pending.clear()

    def _close_stdin(self) -> None:
        if self.proc and self.proc.stdin and not self.proc.stdin.is_closing():
            self.proc.stdin.close()

    async def call(self, method: str, params: Any = None) -> Any:
        deadline = time.monotonic() + CALL_TIMEOUT
        if self.state in ("down", "refused", "stopped"):
            raise BackendUnavailable(self.reason or "backend is not running")
        try:
            await asyncio.wait_for(self.ready.wait(), CALL_TIMEOUT)
        except asyncio.TimeoutError:
            raise BackendUnavailable("backend is not ready") from None
        proc = self.proc
        if proc is None or proc.stdin is None:
            raise BackendUnavailable("backend is not running")

        req_id = next(self.ids)
        fut = asyncio.get_running_loop().create_future()
        self.pending[req_id] = fut
        msg = {"jsonrpc": "2.0", "id": req_id, "method": method}
        if params is not None:
            msg["params"] = params
        try:
            proc.stdin.write(json.dumps(msg).encode() + b"\n")
            await proc.stdin.drain()
            return await asyncio.wait_for(fut, max(deadline - time.monotonic(), 0))
        except asyncio.TimeoutError:
            raise BackendUnavailable(f"{method} timed out") from None
        except (BrokenPipeError, ConnectionResetError):
            raise BackendUnavailable("backend exited") from None
        finally:
            self.pending.pop(req_id, None)

    async def restart(self) -> None:
        """User-requested restart: never counted as a crash, and skips any backoff."""
        self.crashes = []
        if self.state in ("down", "refused"):
            # Calls made from here on wait for the new handshake instead of failing.
            self.state = "starting"
            self.wake.set()
        elif self.proc:
            self.restart_requested = True
            self.proc.kill()
        else:
            self.wake.set()

    async def stop(self) -> None:
        self.stopping = True
        self.wake.set()
        proc = self.proc
        if proc is None:
            return
        self._close_stdin()
        try:
            await asyncio.wait_for(proc.wait(), STOP_TIMEOUT)
        except asyncio.TimeoutError:
            decky.logger.warning("backend did not exit after stdin closed, killing it")
            proc.kill()
            await proc.wait()
        self.state = "stopped"


class Plugin:
    def __init__(self):
        # Created here, not in _main: Decky can deliver frontend calls before _main runs, and
        # they wait on the backend's ready handshake.
        self.backend = Backend()
        self.supervisor: Optional[asyncio.Task] = None

    async def _main(self):
        self.supervisor = asyncio.create_task(self.backend.run())

    async def _unload(self):
        await self.backend.stop()
        if self.supervisor:
            await self.supervisor

    async def call(self, method: str, params: Any = None) -> Any:
        """Forwards a call to the backend unchanged; params are never inspected here."""
        return await self.backend.call(method, params)

    async def backend_status(self) -> dict:
        return self.backend.status()

    async def restart_backend(self) -> None:
        await self.backend.restart()
