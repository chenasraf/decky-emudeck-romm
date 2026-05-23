"""Steam shortcut namespace — single source of truth for the launch-options
prefix that identifies shortcuts created by this fork.

Used to distinguish our shortcuts from any created by upstream
``danielcopper/decky-romm-sync`` (which uses the bare ``romm:`` prefix)
when both plugins happen to be installed side-by-side. The frontend
mirrors this prefix in ``src/utils/steamShortcuts.ts``; the two must
stay in sync.
"""

from __future__ import annotations

EMUDECK_ROMM_SHORTCUT_TAG_PREFIX = "emudeck-romm"


def build_launch_options(rom_id: int | str) -> str:
    """Compose the launch_options string for a RomM shortcut."""
    return f"{EMUDECK_ROMM_SHORTCUT_TAG_PREFIX}:{rom_id}"
