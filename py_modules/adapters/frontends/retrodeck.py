"""RetroDECK frontend adapter.

Implements the :class:`services.protocols.frontend.Frontend` Protocol
against a RetroDECK Flatpak install (``net.retrodeck.retrodeck``).
Path getters resolve via ``retrodeck.json`` — RetroDECK's user-facing
configurator output — cached for 30 seconds: long enough to amortize
repeated reads during a sync run, short enough to pick up edits made
via the RetroDECK configurator within a single plugin session.
"""

from __future__ import annotations

import json
import logging
import os
import time
from pathlib import Path
from typing import Any

_RETRODECK_FLATPAK_ID = "net.retrodeck.retrodeck"


class RetroDeckFrontendAdapter:
    """RetroDECK implementation of the ``Frontend`` Protocol."""

    _CACHE_TTL = 30  # seconds

    def __init__(self, *, user_home: str, logger: logging.Logger) -> None:
        self._user_home = user_home
        self._logger = logger
        self._cached_config: dict | None = None
        self._cache_time = 0.0

    # ---- internal config plumbing ------------------------------------

    def _flatpak_root(self) -> str:
        return os.path.join(self._user_home, ".var", "app", _RETRODECK_FLATPAK_ID)

    def _config_path(self) -> str:
        return os.path.join(
            self._flatpak_root(),
            "config",
            "retrodeck",
            "retrodeck.json",
        )

    def _load_config(self) -> dict | None:
        now = time.monotonic()
        if self._cached_config is not None and (now - self._cache_time) < self._CACHE_TTL:
            return self._cached_config
        config_path = self._config_path()
        try:
            with open(config_path) as f:
                config = json.load(f)
            self._cached_config = config
            self._cache_time = now
            return config
        except FileNotFoundError:
            # Missing file is the expected fallback path (fresh install,
            # no RetroDECK yet) — don't spam the log on every read.
            self._cached_config = None
            return None
        except (OSError, json.JSONDecodeError) as exc:
            self._logger.warning(f"Failed to load RetroDECK config at {config_path}: {exc}")
            self._cached_config = None
            self._cache_time = now
            return None

    def _get_path(self, key: str, fallback_subdir: str) -> str:
        config = self._load_config()
        if config:
            path = config.get("paths", {}).get(key, "")
            if path:
                return path
        return os.path.join(self._user_home, "retrodeck", fallback_subdir)

    # ---- Frontend Protocol --------------------------------------------

    def roms(self) -> Path:
        return Path(self._get_path("roms_path", "roms"))

    def saves(self) -> Path:
        return Path(self._get_path("saves_path", "saves"))

    def home(self) -> Path:
        return Path(self._get_path("rd_home_path", ""))

    def rom_root(self, system: str) -> Path:
        return self.roms() / system

    def bios_root(self) -> Path:
        return Path(self._get_path("bios_path", "bios"))

    def save_root(self, system: str) -> Path:
        return self.saves() / system

    def retroarch_config_path(self) -> Path | None:
        return Path(self._flatpak_root()) / "config" / "retroarch" / "retroarch.cfg"

    def retroarch_cores_root(self) -> Path | None:
        return Path(self._flatpak_root()) / "config" / "retroarch" / "cores"

    def launch_command(self, rom: dict[str, Any]) -> str:
        # RetroDECK launches ROMs through its Flatpak entrypoint. The
        # concrete argv shape (extra flags, ES-DE-vs-direct invocation)
        # firms up in Phase 6 when shortcut creation routes through the
        # Frontend Protocol; today nothing consumes this method.
        rom_path = rom.get("file_path") or rom.get("path") or ""
        return f"flatpak run {_RETRODECK_FLATPAK_ID} {rom_path}".rstrip()

    def detect(self) -> bool:
        return os.path.isdir(self._flatpak_root())

    def version(self) -> str | None:
        # RetroDECK does not expose a single canonical version string
        # the plugin currently reads. Returning ``None`` keeps
        # ``compatible()`` permissive — RetroDECK is the upstream
        # default and is not gated this sprint.
        return None

    def compatible(self) -> bool:
        # No version band gates RetroDECK in this sprint; the EmuDeck
        # adapter (B11) is the first to enforce a tested-range probe.
        return True
