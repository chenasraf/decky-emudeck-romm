"""Tests for lib/shortcut_namespace.py — the launch-options prefix that
identifies this fork's Steam shortcuts.

The prefix value is contract: changing it orphans existing shortcuts on
users' machines and breaks the frontend reader in
``src/utils/steamShortcuts.ts``. Treat any change to ``EXPECTED_PREFIX``
below as load-bearing.
"""

import re

from lib.shortcut_namespace import (
    EMUDECK_ROMM_SHORTCUT_TAG_PREFIX,
    build_launch_options,
)

EXPECTED_PREFIX = "emudeck-romm"


class TestShortcutNamespaceConstant:
    def test_prefix_is_emudeck_romm(self):
        assert EMUDECK_ROMM_SHORTCUT_TAG_PREFIX == EXPECTED_PREFIX

    def test_prefix_disjoint_from_upstream(self):
        # Upstream uses bare "romm" — ours must not equal it.
        assert EMUDECK_ROMM_SHORTCUT_TAG_PREFIX != "romm"


class TestBuildLaunchOptions:
    def test_happy_path_int(self):
        assert build_launch_options(42) == "emudeck-romm:42"

    def test_happy_path_str(self):
        assert build_launch_options("42") == "emudeck-romm:42"

    def test_matches_expected_pattern(self):
        pattern = re.compile(rf"^{EXPECTED_PREFIX}:\d+$")
        for rom_id in [1, 42, 4409, 99999]:
            assert pattern.match(build_launch_options(rom_id))

    def test_zero_is_valid(self):
        assert build_launch_options(0) == "emudeck-romm:0"
