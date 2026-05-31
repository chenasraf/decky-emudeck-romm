"""Tests for adapters.frontends.retrodeck.RetroDeckFrontendAdapter."""

from __future__ import annotations

import json
import logging
import os
import time
from pathlib import Path
from unittest.mock import patch

from adapters.frontends.retrodeck import RetroDeckFrontendAdapter


def _make_adapter(tmp_path, config: dict | None = None) -> RetroDeckFrontendAdapter:
    """Create adapter with optional retrodeck.json config."""
    user_home = str(tmp_path)
    if config is not None:
        config_dir = tmp_path / ".var" / "app" / "net.retrodeck.retrodeck" / "config" / "retrodeck"
        config_dir.mkdir(parents=True, exist_ok=True)
        (config_dir / "retrodeck.json").write_text(json.dumps(config))
    return RetroDeckFrontendAdapter(user_home=user_home, logger=logging.getLogger("test"))


class TestPathResolution:
    def test_bios_path_from_config(self, tmp_path):
        adapter = _make_adapter(tmp_path, {"paths": {"bios_path": "/custom/bios"}})
        assert adapter.bios_root() == Path("/custom/bios")

    def test_bios_path_fallback(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        assert adapter.bios_root() == Path(os.path.join(str(tmp_path), "retrodeck", "bios"))

    def test_roms_path_from_config(self, tmp_path):
        adapter = _make_adapter(tmp_path, {"paths": {"roms_path": "/custom/roms"}})
        assert adapter.roms() == Path("/custom/roms")

    def test_roms_path_fallback(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        assert adapter.roms() == Path(os.path.join(str(tmp_path), "retrodeck", "roms"))

    def test_saves_path_from_config(self, tmp_path):
        adapter = _make_adapter(tmp_path, {"paths": {"saves_path": "/custom/saves"}})
        assert adapter.saves() == Path("/custom/saves")

    def test_saves_path_fallback(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        assert adapter.saves() == Path(os.path.join(str(tmp_path), "retrodeck", "saves"))

    def test_retrodeck_home_from_config(self, tmp_path):
        adapter = _make_adapter(tmp_path, {"paths": {"rd_home_path": "/custom/home"}})
        assert adapter.home() == Path("/custom/home")

    def test_retrodeck_home_fallback(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        # Fallback is an empty subpath under ``retrodeck/`` — ``Path`` normalises
        # the trailing empty component away.
        assert adapter.home() == Path(os.path.join(str(tmp_path), "retrodeck"))

    def test_empty_path_uses_fallback(self, tmp_path):
        adapter = _make_adapter(tmp_path, {"paths": {"roms_path": ""}})
        assert adapter.roms() == Path(os.path.join(str(tmp_path), "retrodeck", "roms"))

    def test_missing_paths_key_uses_fallback(self, tmp_path):
        adapter = _make_adapter(tmp_path, {"other": "data"})
        assert adapter.roms() == Path(os.path.join(str(tmp_path), "retrodeck", "roms"))

    def test_malformed_json_uses_fallback(self, tmp_path):
        config_dir = tmp_path / ".var" / "app" / "net.retrodeck.retrodeck" / "config" / "retrodeck"
        config_dir.mkdir(parents=True, exist_ok=True)
        (config_dir / "retrodeck.json").write_text("not valid json")
        adapter = RetroDeckFrontendAdapter(user_home=str(tmp_path), logger=logging.getLogger("test"))
        assert adapter.bios_root() == Path(os.path.join(str(tmp_path), "retrodeck", "bios"))


class TestTTLCache:
    def test_cache_returns_same_value(self, tmp_path):
        adapter = _make_adapter(tmp_path, {"paths": {"bios_path": "/first"}})
        assert adapter.bios_root() == Path("/first")
        # Overwrite config — should still return cached value
        config_dir = tmp_path / ".var" / "app" / "net.retrodeck.retrodeck" / "config" / "retrodeck"
        (config_dir / "retrodeck.json").write_text(json.dumps({"paths": {"bios_path": "/second"}}))
        assert adapter.bios_root() == Path("/first")

    def test_cache_expires_after_ttl(self, tmp_path):
        adapter = _make_adapter(tmp_path, {"paths": {"bios_path": "/first"}})
        assert adapter.bios_root() == Path("/first")
        # Force cache expiry
        adapter._cache_time = time.monotonic() - 31
        config_dir = tmp_path / ".var" / "app" / "net.retrodeck.retrodeck" / "config" / "retrodeck"
        (config_dir / "retrodeck.json").write_text(json.dumps({"paths": {"bios_path": "/second"}}))
        assert adapter.bios_root() == Path("/second")

    def test_failed_load_is_retried(self, tmp_path):
        """A failed load is not cached — later successful loads are picked up immediately.

        The TTL cache only stores positive results. When ``_load_config``
        returns None the cache stays empty, so the next call re-reads
        the file. This lets the adapter recover automatically when a
        missing ``retrodeck.json`` is created at runtime, without
        waiting for the 30-second TTL.
        """
        adapter = _make_adapter(tmp_path)  # no config — returns fallback
        fallback = Path(os.path.join(str(tmp_path), "retrodeck", "bios"))
        assert adapter.bios_root() == fallback

        # Drop a valid config file
        config_dir = tmp_path / ".var" / "app" / "net.retrodeck.retrodeck" / "config" / "retrodeck"
        config_dir.mkdir(parents=True, exist_ok=True)
        (config_dir / "retrodeck.json").write_text(json.dumps({"paths": {"bios_path": "/picked/up"}}))

        # Picked up on the next call — no need to wait out the TTL.
        assert adapter.bios_root() == Path("/picked/up")


class TestLoadConfigLogging:
    def test_load_config_logs_warning_on_json_error(self, tmp_path, caplog):
        """Invalid JSON triggers a warning log and falls back to the
        defaults — the failure is no longer silently swallowed."""
        config_dir = tmp_path / ".var" / "app" / "net.retrodeck.retrodeck" / "config" / "retrodeck"
        config_dir.mkdir(parents=True, exist_ok=True)
        (config_dir / "retrodeck.json").write_text("not valid json")
        adapter = RetroDeckFrontendAdapter(user_home=str(tmp_path), logger=logging.getLogger("test"))

        with caplog.at_level(logging.WARNING):
            result = adapter.bios_root()

        assert result == Path(os.path.join(str(tmp_path), "retrodeck", "bios"))
        assert any("Failed to load RetroDECK config" in rec.message for rec in caplog.records)

    def test_load_config_logs_warning_on_permission_error(self, tmp_path, caplog):
        """PermissionError on the config file triggers a warning log and
        falls back to the defaults."""
        config_dir = tmp_path / ".var" / "app" / "net.retrodeck.retrodeck" / "config" / "retrodeck"
        config_dir.mkdir(parents=True, exist_ok=True)
        config_file = config_dir / "retrodeck.json"
        config_file.write_text(json.dumps({"paths": {"bios_path": "/should/not/be/read"}}))

        real_open = open

        def fake_open(path, *args, **kwargs):
            if str(path) == str(config_file):
                raise PermissionError(f"denied: {path}")
            return real_open(path, *args, **kwargs)

        adapter = RetroDeckFrontendAdapter(user_home=str(tmp_path), logger=logging.getLogger("test"))
        with patch("builtins.open", side_effect=fake_open), caplog.at_level(logging.WARNING):
            result = adapter.bios_root()

        assert result == Path(os.path.join(str(tmp_path), "retrodeck", "bios"))
        assert any("Failed to load RetroDECK config" in rec.message for rec in caplog.records)

    def test_load_config_does_not_log_on_missing_file(self, tmp_path, caplog):
        """A missing ``retrodeck.json`` is the expected fresh-install
        fallback path and must NOT spam the log on every read."""
        adapter = RetroDeckFrontendAdapter(user_home=str(tmp_path), logger=logging.getLogger("test"))

        with caplog.at_level(logging.WARNING):
            result = adapter.bios_root()

        assert result == Path(os.path.join(str(tmp_path), "retrodeck", "bios"))
        assert not any("Failed to load RetroDECK config" in rec.message for rec in caplog.records)


class TestFrontendProtocolMethods:
    def test_roms_returns_base(self, tmp_path):
        adapter = _make_adapter(tmp_path, {"paths": {"roms_path": "/r"}})
        assert adapter.roms() == Path("/r")

    def test_saves_returns_base(self, tmp_path):
        adapter = _make_adapter(tmp_path, {"paths": {"saves_path": "/s"}})
        assert adapter.saves() == Path("/s")

    def test_home_returns_retrodeck_home(self, tmp_path):
        adapter = _make_adapter(tmp_path, {"paths": {"rd_home_path": "/h"}})
        assert adapter.home() == Path("/h")

    def test_rom_root_joins_system(self, tmp_path):
        adapter = _make_adapter(tmp_path, {"paths": {"roms_path": "/r"}})
        assert adapter.rom_root("snes") == Path("/r/snes")

    def test_bios_root_wraps_bios_path(self, tmp_path):
        adapter = _make_adapter(tmp_path, {"paths": {"bios_path": "/b"}})
        assert adapter.bios_root() == Path("/b")

    def test_save_root_joins_system(self, tmp_path):
        adapter = _make_adapter(tmp_path, {"paths": {"saves_path": "/s"}})
        assert adapter.save_root("psx") == Path("/s/psx")

    def test_retroarch_config_path_uses_flatpak_layout(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        expected = (
            tmp_path
            / ".var"
            / "app"
            / "net.retrodeck.retrodeck"
            / "config"
            / "retroarch"
            / "retroarch.cfg"
        )
        assert adapter.retroarch_config_path() == expected

    def test_retroarch_cores_root_uses_flatpak_layout(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        expected = (
            tmp_path / ".var" / "app" / "net.retrodeck.retrodeck" / "config" / "retroarch" / "cores"
        )
        assert adapter.retroarch_cores_root() == expected

    def test_launch_command_uses_file_path(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        cmd = adapter.launch_command({"file_path": "/roms/snes/game.smc"})
        assert cmd == "flatpak run net.retrodeck.retrodeck /roms/snes/game.smc"

    def test_launch_command_falls_back_to_path_key(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        cmd = adapter.launch_command({"path": "/roms/snes/game.smc"})
        assert cmd == "flatpak run net.retrodeck.retrodeck /roms/snes/game.smc"

    def test_launch_command_handles_missing_path(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        # No file_path/path key — command still parses but no ROM argv.
        assert adapter.launch_command({}) == "flatpak run net.retrodeck.retrodeck"

    def test_detect_true_when_flatpak_dir_exists(self, tmp_path):
        (tmp_path / ".var" / "app" / "net.retrodeck.retrodeck").mkdir(parents=True)
        adapter = _make_adapter(tmp_path)
        assert adapter.detect() is True

    def test_detect_false_when_flatpak_dir_missing(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        assert adapter.detect() is False

    def test_version_returns_none(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        assert adapter.version() is None

    def test_compatible_is_true(self, tmp_path):
        adapter = _make_adapter(tmp_path)
        assert adapter.compatible() is True
