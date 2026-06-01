"""In-memory ``Frontend`` Protocol implementation for service tests.

Tests parameterize over this fake rather than building per-suite
hand-rolled stubs. Every Protocol method is observable + overridable:
path getters live on mutable attributes, ``detect`` / ``compatible``
flip via constructor flags, ``version`` returns whatever string the
test seeded, and ``launch_command`` echoes a deterministic placeholder
so assertions can compare against a literal.

The defaults are deliberately permissive — ``detect=True``,
``compatible=True``, an in-band synthetic ``version`` — so the typical
service test never has to think about the frontend probe; the few
tests that exercise the unsupported-version path explicitly opt in.
"""

from __future__ import annotations

from pathlib import Path
from typing import Any


class FakeFrontend:
    """Test double satisfying ``services.protocols.frontend.Frontend``."""

    def __init__(
        self,
        *,
        rom_root: Path,
        bios_root: Path,
        save_root: Path,
        home: Path | None = None,
        retroarch_config_path: Path | None = None,
        retroarch_cores_root: Path | None = None,
        detect: bool = True,
        version: str | None = "fake:1",
        compatible: bool = True,
        slug_overrides: dict[str, str] | None = None,
    ) -> None:
        # ``rom_root`` / ``save_root`` are the BASE trees; per-system
        # methods append the slug. ``home`` defaults to the parent of
        # ``rom_root`` so tests rarely have to set it explicitly.
        self._rom_root = rom_root
        self._bios_root = bios_root
        self._save_root = save_root
        self._home = home if home is not None else rom_root.parent
        self._retroarch_config_path = retroarch_config_path
        self._retroarch_cores_root = retroarch_cores_root
        self._detect = detect
        self._version = version
        self._compatible = compatible
        # ``slug_overrides`` is the test-time stand-in for
        # ``defaults/platform_map_emudeck.json``: anything in the dict
        # wins over identity. Tests that need to assert per-slug
        # translation (``ps`` → ``psx``, etc.) seed this directly.
        self._slug_overrides = slug_overrides or {}

    def system_slug(self, romm_slug: str, console_id: int | None = None) -> str:
        return self._slug_overrides.get(romm_slug, romm_slug)

    def roms(self) -> Path:
        return self._rom_root

    def saves(self) -> Path:
        return self._save_root

    def home(self) -> Path:
        return self._home

    def rom_root(self, system: str) -> Path:
        return self._rom_root / system

    def bios_root(self) -> Path:
        return self._bios_root

    def save_root(self, system: str) -> Path:
        return self._save_root / system

    def retroarch_config_path(self) -> Path | None:
        return self._retroarch_config_path

    def retroarch_cores_root(self) -> Path | None:
        return self._retroarch_cores_root

    def launch_command(self, rom: dict[str, Any]) -> str:
        return f"fake-launch:{rom.get('id', '?')}"

    def detect(self) -> bool:
        return self._detect

    def version(self) -> str | None:
        return self._version

    def compatible(self) -> bool:
        return self._compatible
