"""Tests for adapters.frontends.emudeck.EmuDeckFrontendAdapter."""

from __future__ import annotations

import json
import logging
from pathlib import Path

import pytest

from adapters.frontends.emudeck import EmuDeckFrontendAdapter

_OBSERVED_VERSIONS = {
    "ra": {"id": "ra", "version": 2},
    "esde": {"id": "esde", "version": 5},
    "srm": {"id": "srm", "version": 9},
}


def _make_adapter(tmp_path) -> EmuDeckFrontendAdapter:
    return EmuDeckFrontendAdapter(user_home=str(tmp_path), logger=logging.getLogger("test"))


def _write_settings_sh(tmp_path, content: str) -> None:
    config_dir = tmp_path / ".config" / "EmuDeck"
    config_dir.mkdir(parents=True, exist_ok=True)
    (config_dir / "settings.sh").write_text(content)


def _write_versions_json(tmp_path, data: dict) -> None:
    backend_dir = tmp_path / ".config" / "EmuDeck" / "backend"
    backend_dir.mkdir(parents=True, exist_ok=True)
    (backend_dir / "versions.json").write_text(json.dumps(data))


class TestBaseGetters:
    def test_roms_returns_base(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        assert adapter.roms() == tmp_path / "Emulation" / "roms"

    def test_saves_returns_central_tree(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        assert adapter.saves() == tmp_path / "Emulation" / "saves"

    def test_home_returns_emulation_path(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        assert adapter.home() == tmp_path / "Emulation"

    def test_home_follows_sd_card_settings_sh(self, tmp_path):
        _write_settings_sh(tmp_path, 'emulationPath="/run/media/deck/512GB"/Emulation\n')
        adapter = _make_adapter(tmp_path)
        assert adapter.home() == Path("/run/media/deck/512GB/Emulation")


class TestPathsFallback:
    def test_rom_root_falls_back_to_home_emulation(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        assert adapter.rom_root("snes") == tmp_path / "Emulation" / "roms" / "snes"

    def test_bios_root_falls_back_to_home_emulation(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        assert adapter.bios_root() == tmp_path / "Emulation" / "bios"

    def test_save_root_falls_back_to_home_emulation(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        assert adapter.save_root("psx") == tmp_path / "Emulation" / "saves" / "psx"


class TestPathsFromSettingsSh:
    def test_internal_ssd_settings_sh(self, tmp_path):
        _write_settings_sh(
            tmp_path,
            'emulationPath="/home/deck/Emulation"\nromsPath="/home/deck/Emulation/roms"\n',
        )
        adapter = _make_adapter(tmp_path)
        assert adapter.rom_root("snes") == Path("/home/deck/Emulation/roms/snes")
        assert adapter.bios_root() == Path("/home/deck/Emulation/bios")

    def test_sd_card_settings_sh_with_concatenated_suffix(self, tmp_path):
        # EmuDeck emits literal `"<base>"/<suffix>` quoting on SD-card installs.
        _write_settings_sh(
            tmp_path,
            'emulationPath="/run/media/deck/512GB"/Emulation\n'
            'romsPath="/run/media/deck/512GB"/Emulation/roms\n',
        )
        adapter = _make_adapter(tmp_path)
        assert adapter.rom_root("nes") == Path("/run/media/deck/512GB/Emulation/roms/nes")
        assert adapter.bios_root() == Path("/run/media/deck/512GB/Emulation/bios")
        assert adapter.save_root("gba") == Path("/run/media/deck/512GB/Emulation/saves/gba")

    def test_emulation_path_used_when_roms_path_missing(self, tmp_path):
        _write_settings_sh(tmp_path, 'emulationPath="/custom/path"\n')
        adapter = _make_adapter(tmp_path)
        assert adapter.rom_root("gb") == Path("/custom/path/roms/gb")

    def test_unreadable_settings_sh_logs_warning_and_falls_back(self, tmp_path, caplog):
        config_dir = tmp_path / ".config" / "EmuDeck"
        config_dir.mkdir(parents=True)
        # Make settings.sh a directory so open() raises OSError, not FileNotFoundError.
        (config_dir / "settings.sh").mkdir()
        adapter = _make_adapter(tmp_path)
        with caplog.at_level(logging.WARNING):
            result = adapter.rom_root("snes")
        assert result == tmp_path / "Emulation" / "roms" / "snes"
        assert any("Failed to read EmuDeck settings.sh" in r.message for r in caplog.records)


class TestRetroArchPaths:
    def test_retroarch_config_path_uses_flatpak(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        assert (
            adapter.retroarch_config_path()
            == tmp_path / ".var" / "app" / "org.libretro.RetroArch" / "config" / "retroarch" / "retroarch.cfg"
        )

    def test_retroarch_cores_root_uses_flatpak(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        assert (
            adapter.retroarch_cores_root()
            == tmp_path / ".var" / "app" / "org.libretro.RetroArch" / "config" / "retroarch" / "cores"
        )


class TestLaunchCommand:
    def test_launch_command_raises_not_implemented(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        with pytest.raises(NotImplementedError, match="Phase 6"):
            adapter.launch_command({"file_path": "/r/s/g.smc"})


class TestDetect:
    def test_detect_true_when_roms_dir_present(self, tmp_path):
        (tmp_path / "Emulation" / "roms").mkdir(parents=True)
        adapter = _make_adapter(tmp_path)
        assert adapter.detect() is True

    def test_detect_false_when_roms_dir_missing(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        assert adapter.detect() is False

    def test_detect_uses_settings_sh_roms_path(self, tmp_path):
        # SD-card install: $romsPath resolves to a custom location.
        custom_roms = tmp_path / "sd" / "Emulation" / "roms"
        custom_roms.mkdir(parents=True)
        _write_settings_sh(tmp_path, f'romsPath="{custom_roms}"\n')
        # Bare ~/Emulation/roms does not exist; settings.sh override does.
        adapter = _make_adapter(tmp_path)
        assert adapter.detect() is True


class TestVersion:
    def test_version_none_when_versions_json_missing(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        assert adapter.version() is None

    def test_version_composes_tracked_schema_keys(self, tmp_path):
        _write_versions_json(tmp_path, _OBSERVED_VERSIONS)
        adapter = _make_adapter(tmp_path)
        assert adapter.version() == "esde:5,ra:2,srm:9"

    def test_version_none_when_tracked_key_missing(self, tmp_path):
        partial = {"ra": {"version": 2}, "esde": {"version": 5}}  # srm missing
        _write_versions_json(tmp_path, partial)
        adapter = _make_adapter(tmp_path)
        assert adapter.version() is None

    def test_version_none_when_version_not_int(self, tmp_path):
        data = {
            "ra": {"version": "2"},
            "esde": {"version": 5},
            "srm": {"version": 9},
        }
        _write_versions_json(tmp_path, data)
        adapter = _make_adapter(tmp_path)
        assert adapter.version() is None

    def test_version_logs_warning_and_returns_none_on_malformed_json(self, tmp_path, caplog):
        backend_dir = tmp_path / ".config" / "EmuDeck" / "backend"
        backend_dir.mkdir(parents=True)
        (backend_dir / "versions.json").write_text("not valid json")
        adapter = _make_adapter(tmp_path)
        with caplog.at_level(logging.WARNING):
            result = adapter.version()
        assert result is None
        assert any("Failed to read EmuDeck versions.json" in r.message for r in caplog.records)


class TestCompatible:
    def test_compatible_false_when_version_missing(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        assert adapter.compatible() is False

    def test_compatible_true_when_in_band(self, tmp_path):
        _write_versions_json(tmp_path, _OBSERVED_VERSIONS)
        adapter = _make_adapter(tmp_path)
        assert adapter.compatible() is True

    def test_compatible_false_when_out_of_band(self, tmp_path):
        # Mutate srm schema beyond tested max
        synthetic = {
            "ra": {"version": 2},
            "esde": {"version": 5},
            "srm": {"version": 99},
        }
        _write_versions_json(tmp_path, synthetic)
        adapter = _make_adapter(tmp_path)
        assert adapter.compatible() is False


def _write_platform_map(plugin_dir: Path, data: dict) -> None:
    defaults_dir = plugin_dir / "defaults"
    defaults_dir.mkdir(parents=True, exist_ok=True)
    (defaults_dir / "platform_map_emudeck.json").write_text(json.dumps(data))


def _make_adapter_with_map(tmp_path, plugin_dir: Path) -> EmuDeckFrontendAdapter:
    return EmuDeckFrontendAdapter(
        user_home=str(tmp_path),
        logger=logging.getLogger("test"),
        plugin_dir=str(plugin_dir),
    )


class TestSystemSlug:
    def test_identity_fallback_when_no_plugin_dir(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        # plugin_dir=None → empty map → every slug returns unchanged.
        assert adapter.system_slug("snes") == "snes"
        assert adapter.system_slug("totally-unknown") == "totally-unknown"

    def test_rename_via_shipped_map(self, tmp_path):
        plugin_dir = tmp_path / "plugin"
        _write_platform_map(plugin_dir, {"ps": "psx", "3ds": "n3ds"})
        adapter = _make_adapter_with_map(tmp_path, plugin_dir)
        assert adapter.system_slug("ps") == "psx"
        assert adapter.system_slug("3ds") == "n3ds"

    def test_unmapped_slug_falls_back_to_identity(self, tmp_path):
        plugin_dir = tmp_path / "plugin"
        _write_platform_map(plugin_dir, {"ps": "psx"})
        adapter = _make_adapter_with_map(tmp_path, plugin_dir)
        assert adapter.system_slug("nes") == "nes"

    def test_console_id_ignored_in_sprint_4(self, tmp_path):
        # Phase 4's region-aware lookup reserves console_id; Sprint 4
        # ignores it. Asserting current behavior so a future change
        # surfaces here loudly.
        plugin_dir = tmp_path / "plugin"
        _write_platform_map(plugin_dir, {"snes": "snes"})
        adapter = _make_adapter_with_map(tmp_path, plugin_dir)
        assert adapter.system_slug("snes", console_id=42) == "snes"

    def test_comment_key_skipped(self, tmp_path):
        plugin_dir = tmp_path / "plugin"
        _write_platform_map(plugin_dir, {"_comment": "documentation", "ps": "psx"})
        adapter = _make_adapter_with_map(tmp_path, plugin_dir)
        # ``_comment`` key was filtered out → identity fallback.
        assert adapter.system_slug("_comment") == "_comment"

    def test_malformed_json_logs_warning_and_returns_empty_map(self, tmp_path, caplog):
        plugin_dir = tmp_path / "plugin"
        defaults_dir = plugin_dir / "defaults"
        defaults_dir.mkdir(parents=True)
        (defaults_dir / "platform_map_emudeck.json").write_text("not valid json")
        with caplog.at_level(logging.WARNING):
            adapter = _make_adapter_with_map(tmp_path, plugin_dir)
        assert adapter.system_slug("ps") == "ps"  # identity fallback
        assert any("platform_map_emudeck.json" in r.message for r in caplog.records)

    def test_shipped_map_resolves_key_renames(self, tmp_path):
        # End-to-end against the actual shipped map. plugin_dir points
        # at the repo root so the loader finds the real JSON.
        repo_root = Path(__file__).resolve().parents[3]
        assert (repo_root / "defaults" / "platform_map_emudeck.json").is_file(), (
            "shipped platform map missing"
        )
        adapter = EmuDeckFrontendAdapter(
            user_home=str(tmp_path),
            logger=logging.getLogger("test"),
            plugin_dir=str(repo_root),
        )
        assert adapter.system_slug("ps") == "psx"
        assert adapter.system_slug("3ds") == "n3ds"
        assert adapter.system_slug("super-nintendo") == "snes"
        assert adapter.system_slug("mame") == "arcade"
        # Identity for slugs already matching EmuDeck.
        assert adapter.system_slug("snes") == "snes"
        assert adapter.system_slug("n64") == "n64"
        # Unknown slug → identity fallback.
        assert adapter.system_slug("totally-unknown-platform") == "totally-unknown-platform"
