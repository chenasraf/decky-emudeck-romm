"""Frontend Protocol — the host emulator-frontend abstraction.

Every service that needs to talk about *where* something lives on disk
for a given emulator frontend (RetroDECK, EmuDeck, hypothetical future
peers) depends on this Protocol instead of a concrete adapter. The
Protocol covers four concerns:

- **Path getters** for the trees the plugin reads from or writes to
  (ROMs, BIOS, saves, RetroArch config + cores). Per-emulator save
  resolution beyond the system-level ``save_root`` belongs to the
  Phase 4 ``domain/save_locations.py`` resolver, not this Protocol.
- **Launch shape** for Steam-shortcut wiring — what command should
  Steam invoke to play a ROM through this frontend. The concrete
  argv shape is frontend-specific and will firm up in Phase 6; the
  signature here is a stub.
- **Detection + version** so the composition root can autodetect the
  installed frontend and gate startup on a tested version band.
- **Compatibility verdict** — ``compatible()`` returns False when the
  detected version sits outside the adapter's tested band; bootstrap
  converts that into a :class:`lib.errors.FrontendUnsupportedError`.

Anything stateful, anything that touches the filesystem, anything that
parses a config file — belongs in the implementing adapter, not here.
The Protocol is a *shape*, not a behavior.
"""

from __future__ import annotations

from pathlib import Path
from typing import Any, Protocol


class Frontend(Protocol):
    """Host emulator-frontend (RetroDECK, EmuDeck, …) seam.

    Services hold a ``Frontend`` reference instead of a concrete adapter
    so the same code path runs under any supported frontend. Adapters
    implement this Protocol; the composition root picks one at startup
    based on the user setting + autodetect + fallback chain.
    """

    def roms(self) -> Path:
        """Base directory holding every per-system ROM subfolder."""
        ...

    def saves(self) -> Path:
        """Base directory holding every per-system save subfolder."""
        ...

    def home(self) -> Path:
        """Root the frontend stores its own files under.

        RetroDECK: the Flatpak ``rd_home_path`` (where the bundled
        ES-DE config lives). EmuDeck: the ``emulationPath`` parsed
        from ``settings.sh``. Used by services that detect
        path-change migrations (the user switching internal SSD ↔
        SD card) and by ES-DE override writes — Phase 6 will
        firm up ES-DE-specific resolution; today the call sites
        treat ``home()`` as an opaque sentinel for change detection.
        """
        ...

    def system_slug(self, romm_slug: str, console_id: int | None = None) -> str:
        """Translate a RomM platform slug to this frontend's native system folder name.

        Most slugs match identity (RomM ``snes`` → frontend ``snes``).
        Some differ — EmuDeck calls PlayStation ``psx`` where RomM
        uses ``ps``, and EmuDeck splits Nintendo 3DS as ``n3ds`` where
        RomM uses ``3ds``. Implementations look up against a frontend-
        specific map and fall back to identity when the slug is
        unmapped.

        ``console_id`` is a forward-compat hook for region-disambiguation
        (e.g. RomM's per-region PlayStation IDs that map to different
        EmuDeck folders). Sprint 4 ignores it; Phase 4's emulator-
        picker UX will wire it through.
        """
        ...

    def rom_root(self, system: str) -> Path:
        """Directory the frontend expects ROMs for ``system`` to live in.

        Convenience wrapper over :meth:`roms` — equivalent to
        ``self.roms() / system``. Callers that hold a raw RomM platform
        slug should compose :meth:`system_slug` first to translate to
        the frontend's native folder name.
        """
        ...

    def bios_root(self) -> Path:
        """Directory the frontend reads BIOS files from."""
        ...

    def save_root(self, system: str) -> Path:
        """Base save directory for ``system``.

        This is the *frontend-level* base — for emulators that write
        outside the central save tree (e.g. Flatpak emulators with
        sandboxed save dirs) the Phase 4 ``domain/save_locations.py``
        resolver layers a per-emulator override on top.
        """
        ...

    def retroarch_config_path(self) -> Path | None:
        """Path to ``retroarch.cfg``, or None when this frontend doesn't ship RetroArch."""
        ...

    def retroarch_cores_root(self) -> Path | None:
        """Directory holding RetroArch ``.so`` cores, or None when N/A."""
        ...

    def launch_command(self, rom: dict[str, Any]) -> str:
        """Command Steam should invoke to launch ``rom`` through this frontend.

        Signature is a stub — the concrete argv shape (extra flags,
        per-system launcher scripts, etc.) is frontend-specific and
        will be refined when Phase 6 wires shortcut creation through
        the Protocol. Implementations may raise ``NotImplementedError``
        in earlier phases where no live consumer exists yet.
        """
        ...

    def detect(self) -> bool:
        """True if this frontend looks installed on the running system.

        Used by the bootstrap autodetect chain. Implementations check
        a cheap on-disk marker (a well-known directory or Flatpak app
        ID), not a full version probe.
        """
        ...

    def version(self) -> str | None:
        """Installed frontend version, or None when unknown or not present.

        A None return is *not* a failure — it means "this frontend
        doesn't expose a version string we can read, treat it as
        compatible by default". See :meth:`compatible`.
        """
        ...

    def compatible(self) -> bool:
        """True when :meth:`version` sits inside the adapter's tested band.

        Returns True when ``version()`` is None (we can't check, so we
        don't refuse to run). Returns False when a version is present
        but falls outside ``[_MIN_TESTED_VERSION, _MAX_TESTED_VERSION]``.
        The bootstrap caller converts a False here into a
        :class:`lib.errors.FrontendUnsupportedError`.
        """
        ...
