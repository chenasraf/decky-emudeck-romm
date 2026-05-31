"""EmuDeck frontend adapter.

Implements :class:`services.protocols.frontend.Frontend` against
EmuDeck's filesystem layout (documented in
``docs/architecture/emudeck-layout.md``). Roots resolve via
``~/.config/EmuDeck/settings.sh`` so SD-card installs — where the tree
lives under ``/run/media/deck/<label>/Emulation`` — work without code
changes; ``~/Emulation`` is the fallback when ``settings.sh`` is
missing or unparseable.

Version compatibility is tracked through three EmuDeck-backend
``versions.json`` schema integers (RetroArch, ES-DE, SRM) — the
sub-systems this plugin integrates with most directly. The composite
signature is compared against a tested band; bootstrap (B11) converts
an out-of-band reading into a
:class:`lib.errors.FrontendUnsupportedError`. Per-emulator save-path
resolution (the Flatpak-vs-central-tree exceptions documented in the
inventory) is Phase 4 work — :meth:`save_root` returns the central
``$emulationPath/saves/<system>`` tree only.
"""

from __future__ import annotations

import json
import logging
import os
import re
from pathlib import Path
from typing import Any

# Observed on the 2026-05-24 dev-Deck sample (see docs/architecture/emudeck-layout.md).
# Tight band of one — widen as more samples come in. Composite of
# {tracked_emu}:{schema_version} pairs, sorted, comma-joined.
_OBSERVED_SCHEMA_SIGNATURE = "esde:5,ra:2,srm:9"
_MIN_TESTED_VERSION = _OBSERVED_SCHEMA_SIGNATURE
_MAX_TESTED_VERSION = _OBSERVED_SCHEMA_SIGNATURE

# Tracked emulators whose schema versions form the compatibility
# signature. Adding an emulator here tightens compatibility; the
# inventory doc justifies this trio as the integration surface.
_TRACKED_SCHEMA_KEYS = ("esde", "ra", "srm")

_DEFAULT_EMULATION_DIRNAME = "Emulation"
_RETROARCH_FLATPAK_ID = "org.libretro.RetroArch"

# settings.sh line shape: `emulationPath="/home/deck/Emulation"` or
# `emulationPath="/run/media/deck/512GB"/Emulation` (the literal
# quote-then-slash form EmuDeck emits for SD-card installs).
_EMULATION_PATH_LINE = re.compile(r'^\s*emulationPath="([^"]*)"(/[^\s]+)?\s*$')
_ROMS_PATH_LINE = re.compile(r'^\s*romsPath="([^"]*)"(/[^\s]+)?\s*$')


class EmuDeckFrontendAdapter:
    """EmuDeck implementation of the ``Frontend`` Protocol."""

    # Exposed as class attributes so bootstrap can include them in the
    # ``FrontendUnsupportedError`` payload without reaching into module
    # globals via ``getattr``-on-the-instance falling through.
    _MIN_TESTED_VERSION = _OBSERVED_SCHEMA_SIGNATURE
    _MAX_TESTED_VERSION = _OBSERVED_SCHEMA_SIGNATURE

    def __init__(self, *, user_home: str, logger: logging.Logger) -> None:
        self._user_home = user_home
        self._logger = logger

    # ---- path resolution ---------------------------------------------

    def _settings_sh_path(self) -> str:
        return os.path.join(self._user_home, ".config", "EmuDeck", "settings.sh")

    def _parse_settings_sh(self) -> dict[str, str]:
        path = self._settings_sh_path()
        try:
            with open(path) as f:
                lines = f.readlines()
        except FileNotFoundError:
            return {}
        except OSError as exc:
            self._logger.warning(f"Failed to read EmuDeck settings.sh at {path}: {exc}")
            return {}
        parsed: dict[str, str] = {}
        for raw in lines:
            m = _EMULATION_PATH_LINE.match(raw)
            if m:
                base = m.group(1)
                suffix = m.group(2) or ""
                parsed["emulationPath"] = base + suffix
                continue
            m = _ROMS_PATH_LINE.match(raw)
            if m:
                base = m.group(1)
                suffix = m.group(2) or ""
                parsed["romsPath"] = base + suffix
        return parsed

    def _emulation_path(self) -> str:
        settings = self._parse_settings_sh()
        if "emulationPath" in settings:
            return settings["emulationPath"]
        return os.path.join(self._user_home, _DEFAULT_EMULATION_DIRNAME)

    def _roms_path(self) -> str:
        settings = self._parse_settings_sh()
        if "romsPath" in settings:
            return settings["romsPath"]
        return os.path.join(self._emulation_path(), "roms")

    def _retroarch_flatpak_root(self) -> Path:
        return Path(self._user_home) / ".var" / "app" / _RETROARCH_FLATPAK_ID

    # ---- Frontend Protocol -------------------------------------------

    def roms(self) -> Path:
        return Path(self._roms_path())

    def saves(self) -> Path:
        return Path(self._emulation_path()) / "saves"

    def home(self) -> Path:
        # ``emulationPath`` is the closest analog to RetroDECK's
        # ``rd_home_path``. ES-DE-specific path resolution is Phase 6
        # work — EmuDeck stores ES-DE config under ``~/ES-DE/`` rather
        # than under the emulation root, so callers that read ES-DE
        # config off ``home()`` will diverge between frontends until
        # Phase 6 lands.
        return Path(self._emulation_path())

    def rom_root(self, system: str) -> Path:
        return self.roms() / system

    def bios_root(self) -> Path:
        return Path(self._emulation_path()) / "bios"

    def save_root(self, system: str) -> Path:
        # Central save tree per docs/architecture/emudeck-layout.md.
        # Per-emulator overrides (Flatpak exceptions) land in Phase 4.
        return self.saves() / system

    def retroarch_config_path(self) -> Path | None:
        return self._retroarch_flatpak_root() / "config" / "retroarch" / "retroarch.cfg"

    def retroarch_cores_root(self) -> Path | None:
        return self._retroarch_flatpak_root() / "config" / "retroarch" / "cores"

    def launch_command(self, rom: dict[str, Any]) -> str:
        raise NotImplementedError(
            "EmuDeck launch_command lands in Phase 6 when shortcut creation "
            "routes through the Frontend Protocol."
        )

    def detect(self) -> bool:
        # autodetect rule: presence of $romsPath. We
        # resolve $romsPath through settings.sh when available so SD-card
        # installs still autodetect — the bare ~/Emulation check would
        # miss them.
        return os.path.isdir(self._roms_path())

    def version(self) -> str | None:
        path = os.path.join(
            self._user_home, ".config", "EmuDeck", "backend", "versions.json"
        )
        try:
            with open(path) as f:
                data = json.load(f)
        except FileNotFoundError:
            return None
        except (OSError, json.JSONDecodeError) as exc:
            self._logger.warning(
                f"Failed to read EmuDeck versions.json at {path}: {exc}"
            )
            return None
        pairs: list[str] = []
        for key in _TRACKED_SCHEMA_KEYS:
            entry = data.get(key)
            if not isinstance(entry, dict):
                # Missing a tracked emulator entry == unknown signature.
                # Better to return None than fabricate a partial one.
                return None
            ver = entry.get("version")
            if not isinstance(ver, int):
                return None
            pairs.append(f"{key}:{ver}")
        return ",".join(pairs)

    def compatible(self) -> bool:
        # Strict: EmuDeck must expose a readable schema signature inside
        # the tested band. A missing versions.json is treated as
        # incompatible (B11 raises FrontendUnsupportedError). RetroDECK's
        # adapter encodes the opposite policy because it has no version
        # file by design.
        v = self.version()
        if v is None:
            return False
        return _MIN_TESTED_VERSION <= v <= _MAX_TESTED_VERSION

