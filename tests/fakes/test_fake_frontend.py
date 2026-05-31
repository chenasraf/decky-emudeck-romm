"""Smoke tests for ``FakeFrontend``.

The fake stands in for the Frontend Protocol across every service
test, so a few self-checks keep its behavior pinned and make defaults
easy to spot when they drift.
"""

from __future__ import annotations

from pathlib import Path

from fakes.fake_frontend import FakeFrontend


class TestDefaults:
    def test_fixture_paths_live_under_tmp_path(self, fake_frontend, tmp_path):
        # rom/bios/save roots are tmp_path subdirs by fixture default.
        assert fake_frontend.rom_root("snes") == tmp_path / "roms" / "snes"
        assert fake_frontend.bios_root() == tmp_path / "bios"
        assert fake_frontend.save_root("psx") == tmp_path / "saves" / "psx"

    def test_fixture_reports_detected_and_compatible(self, fake_frontend):
        assert fake_frontend.detect() is True
        assert fake_frontend.compatible() is True
        assert fake_frontend.version() == "fake:1"


class TestOverrides:
    def test_launch_command_uses_rom_id(self, fake_frontend):
        assert fake_frontend.launch_command({"id": 42}) == "fake-launch:42"
        assert fake_frontend.launch_command({}) == "fake-launch:?"

    def test_detect_false_when_constructor_flag_off(self, tmp_path):
        f = FakeFrontend(
            rom_root=tmp_path / "r",
            bios_root=tmp_path / "b",
            save_root=tmp_path / "s",
            detect=False,
        )
        assert f.detect() is False

    def test_compatible_false_when_out_of_band(self, tmp_path):
        f = FakeFrontend(
            rom_root=tmp_path / "r",
            bios_root=tmp_path / "b",
            save_root=tmp_path / "s",
            compatible=False,
        )
        assert f.compatible() is False

    def test_version_none_when_unconfigured(self, tmp_path):
        f = FakeFrontend(
            rom_root=tmp_path / "r",
            bios_root=tmp_path / "b",
            save_root=tmp_path / "s",
            version=None,
        )
        assert f.version() is None

    def test_retroarch_paths_default_to_none(self, tmp_path):
        f = FakeFrontend(
            rom_root=tmp_path / "r",
            bios_root=tmp_path / "b",
            save_root=tmp_path / "s",
        )
        assert f.retroarch_config_path() is None
        assert f.retroarch_cores_root() is None

    def test_retroarch_paths_set_through_constructor(self, tmp_path):
        cfg = tmp_path / "ra" / "retroarch.cfg"
        cores = tmp_path / "ra" / "cores"
        f = FakeFrontend(
            rom_root=tmp_path / "r",
            bios_root=tmp_path / "b",
            save_root=tmp_path / "s",
            retroarch_config_path=cfg,
            retroarch_cores_root=cores,
        )
        assert f.retroarch_config_path() == cfg
        assert f.retroarch_cores_root() == cores


class TestProtocolConformance:
    """basedpyright won't structurally check at runtime, so assert the
    fake satisfies every method the Frontend Protocol declares by
    explicit name lookup. Drift between Protocol and fake surfaces
    here instead of as a cascade of failures in service tests."""

    def test_implements_every_frontend_method(self, fake_frontend):
        for name in (
            "rom_root",
            "bios_root",
            "save_root",
            "retroarch_config_path",
            "retroarch_cores_root",
            "launch_command",
            "detect",
            "version",
            "compatible",
        ):
            assert callable(getattr(fake_frontend, name)), f"missing: {name}"

    def test_path_methods_return_pathlib_paths(self, fake_frontend):
        assert isinstance(fake_frontend.rom_root("x"), Path)
        assert isinstance(fake_frontend.bios_root(), Path)
        assert isinstance(fake_frontend.save_root("x"), Path)
