import asyncio
import json
import os
from pathlib import Path
from unittest.mock import MagicMock

import pytest
from fakes.fake_frontend import FakeFrontend
from fakes.fake_metadata_cache_persister import FakeMetadataCachePersister
from fakes.fake_path_exists_reader import FakePathExistsReader
from fakes.fake_settings_persister import FakeSettingsPersister
from fakes.fake_sgdb_artwork_cache import FakeSgdbArtworkCache
from fakes.fake_state_persister import FakeStatePersister
from fakes.library_peers import FakeArtworkManager, FakeMetadataExtractor
from fakes.system_time import FakeClock, FakeSleeper, FakeUuidGen
from models.state import make_default_plugin_state

from adapters.debug_logger import SettingsAwareDebugLogger
from adapters.persistence import PersistenceAdapter, SettingsPersisterAdapter
from adapters.registry_store import RegistryStoreAdapter
from adapters.steam_config import SteamConfigAdapter

# conftest.py patches decky before this import
from main import Plugin
from services.connection import ConnectionService, ConnectionServiceConfig
from services.library import LibraryService, LibraryServiceConfig
from services.settings import SettingsService, SettingsServiceConfig
from services.startup_healing import StartupHealingService, StartupHealingServiceConfig
from services.steamgrid import SteamGridService, SteamGridServiceConfig


@pytest.fixture
def plugin():
    p = Plugin()
    p.settings = {"romm_url": "", "romm_user": "", "romm_pass": "", "enabled_platforms": {}}
    p._http_adapter = MagicMock()
    p._romm_api = MagicMock()
    p._state = make_default_plugin_state()
    p._metadata_cache = {}
    # Default to "/tmp" so the prune guard sees an existing home in tests that
    # don't override it. Tests exercising the guard rebuild this with a
    # non-existent path or empty string.
    p._frontend = FakeFrontend(
        rom_root=Path("/tmp/roms"),
        bios_root=Path("/tmp/bios"),
        save_root=Path("/tmp/saves"),
        home=Path("/tmp"),
    )
    p._migration_service = MagicMock()

    import decky

    p._debug_logger = SettingsAwareDebugLogger(settings=p.settings, logger=decky.logger)
    steam_config = SteamConfigAdapter(user_home=decky.DECKY_USER_HOME, logger=decky.logger)
    p._steam_config = steam_config

    p._state_persister = FakeStatePersister()
    p._settings_persister = FakeSettingsPersister()
    p._metadata_cache_persister = FakeMetadataCachePersister()
    p._registry_store = RegistryStoreAdapter(state=p._state, logger=decky.logger)

    p._sync_service = LibraryService(
        config=LibraryServiceConfig(
            romm_api=p._romm_api,
            steam_config=steam_config,
            state=p._state,
            settings=p.settings,
            metadata_cache=p._metadata_cache,
            loop=asyncio.get_event_loop(),
            logger=decky.logger,
            plugin_dir=decky.DECKY_PLUGIN_DIR,
            emit=decky.emit,
            clock=FakeClock(),
            uuid_gen=FakeUuidGen(),
            sleeper=FakeSleeper(),
            state_persister=p._state_persister,
            settings_persister=p._settings_persister,
            registry_store=p._registry_store,
            log_debug=p._log_debug,
            metadata_service=FakeMetadataExtractor(),
            artwork=FakeArtworkManager(),
        ),
    )

    p._sgdb_service = SteamGridService(
        config=SteamGridServiceConfig(
            sgdb_api=MagicMock(),
            romm_api=p._romm_api,
            steam_config=steam_config,
            sgdb_artwork_cache=FakeSgdbArtworkCache(cache_root=decky.DECKY_PLUGIN_RUNTIME_DIR),
            state=p._state,
            settings=p.settings,
            loop=asyncio.get_event_loop(),
            logger=decky.logger,
            state_persister=FakeStatePersister(),
            settings_persister=FakeSettingsPersister(),
            registry_store=p._registry_store,
            get_pending_sync=lambda: p._sync_service._pending_sync,
            log_debug=p._log_debug,
        ),
    )

    p._settings_service = SettingsService(
        config=SettingsServiceConfig(
            settings=p.settings,
            state=p._state,
            logger=decky.logger,
            settings_persister=p._settings_persister,
            steam_config=steam_config,
        ),
    )

    p._connection_service = ConnectionService(
        config=ConnectionServiceConfig(
            settings=p.settings,
            romm_api=p._romm_api,
            loop=asyncio.get_event_loop(),
            logger=decky.logger,
            min_required_version=Plugin._MIN_REQUIRED_VERSION,
        ),
    )

    p._startup_healing_service = StartupHealingService(
        config=StartupHealingServiceConfig(
            state=p._state,
            logger=decky.logger,
            state_persister=p._state_persister,
            registry_store=p._registry_store,
            frontend=p._frontend,
            path_probe=FakePathExistsReader(),
        ),
    )
    return p


class TestPersistenceAttributeIsLoud:
    """Regression for #350: dropped the lazy-property fallback.

    Pre-``_main()`` access to ``self._persistence`` must raise
    ``AttributeError`` instead of silently constructing a second
    ``PersistenceAdapter`` instance.
    """

    def test_attribute_missing_on_bare_plugin(self):
        """Direct access to _persistence pre-_main() raises — no lazy fallback."""
        from main import Plugin

        bare = Plugin()

        with pytest.raises(AttributeError, match="_persistence"):
            _ = bare._persistence

    def test_state_persister_missing_on_bare_plugin(self):
        """``_state_persister`` is bound only by ``_main()``; bare access raises."""
        from main import Plugin

        bare = Plugin()

        with pytest.raises(AttributeError, match="_state_persister"):
            _ = bare._state_persister

    def test_settings_persister_missing_on_bare_plugin(self):
        """``_settings_persister`` is bound only by ``_main()``; bare access raises."""
        from main import Plugin

        bare = Plugin()

        with pytest.raises(AttributeError, match="_settings_persister"):
            _ = bare._settings_persister

    def test_metadata_cache_persister_missing_on_bare_plugin(self):
        """``_metadata_cache_persister`` is bound only by ``_main()``; bare access raises."""
        from main import Plugin

        bare = Plugin()

        with pytest.raises(AttributeError, match="_metadata_cache_persister"):
            _ = bare._metadata_cache_persister


class TestSettings:
    @pytest.mark.asyncio
    async def test_get_settings_masks_password(self, plugin):
        plugin.settings["romm_pass"] = "secret123"
        result = await plugin.get_settings()
        assert result["romm_pass_masked"] == "••••"
        assert "secret123" not in str(result)

    @pytest.mark.asyncio
    async def test_get_settings_empty_password(self, plugin):
        plugin.settings["romm_pass"] = ""
        result = await plugin.get_settings()
        assert result["romm_pass_masked"] == ""

    @pytest.mark.asyncio
    async def test_save_settings_skips_masked_password(self, plugin, tmp_path):
        import decky

        plugin._persistence = PersistenceAdapter(str(tmp_path), decky.DECKY_PLUGIN_RUNTIME_DIR, decky.logger)
        plugin.settings["romm_pass"] = "original"
        await plugin.save_settings("http://example.com", "user", "••••")
        assert plugin.settings["romm_pass"] == "original"

    @pytest.mark.asyncio
    async def test_save_settings_updates_real_password(self, plugin, tmp_path):
        import decky

        plugin._persistence = PersistenceAdapter(str(tmp_path), decky.DECKY_PLUGIN_RUNTIME_DIR, decky.logger)
        plugin.settings["romm_pass"] = "old"
        await plugin.save_settings("http://example.com", "user", "newpass")
        assert plugin.settings["romm_pass"] == "newpass"


class TestConnection:
    @pytest.mark.asyncio
    async def test_test_connection_sets_version_on_romm_api(self, plugin):
        import decky

        plugin.loop = asyncio.get_event_loop()
        plugin.settings["romm_url"] = "http://romm.local"
        plugin._romm_api.heartbeat.return_value = {"SYSTEM": {"VERSION": "4.8.1"}}
        plugin._romm_api.list_platforms.return_value = [{"id": 1, "slug": "n64"}]
        # Rebuild connection service with the live event loop so executor
        # callbacks dispatch on the same loop the test awaits.
        plugin._connection_service = ConnectionService(
            config=ConnectionServiceConfig(
                settings=plugin.settings,
                romm_api=plugin._romm_api,
                loop=plugin.loop,
                logger=decky.logger,
                min_required_version=Plugin._MIN_REQUIRED_VERSION,
            ),
        )
        result = await plugin.test_connection()
        assert result["success"] is True
        plugin._romm_api.set_version.assert_called_once_with("4.8.1")


class TestLogLevel:
    def test_log_debug_enabled(self, plugin):
        """_log_debug logs when log_level is 'debug'."""
        from unittest.mock import patch

        import decky

        plugin.settings["log_level"] = "debug"
        with patch.object(decky.logger, "info") as mock_info:
            plugin._log_debug("test message")
            mock_info.assert_called_once_with("test message")

    def test_log_debug_disabled_at_warn(self, plugin):
        """_log_debug does not log when log_level is 'warn' (default)."""
        from unittest.mock import patch

        import decky

        plugin.settings["log_level"] = "warn"
        with patch.object(decky.logger, "info") as mock_info:
            plugin._log_debug("test message")
            mock_info.assert_not_called()

    def test_log_debug_disabled_at_info(self, plugin):
        """_log_debug does not log when log_level is 'info'."""
        from unittest.mock import patch

        import decky

        plugin.settings["log_level"] = "info"
        with patch.object(decky.logger, "info") as mock_info:
            plugin._log_debug("test message")
            mock_info.assert_not_called()

    def test_log_debug_disabled_at_error(self, plugin):
        """_log_debug does not log when log_level is 'error'."""
        from unittest.mock import patch

        import decky

        plugin.settings["log_level"] = "error"
        with patch.object(decky.logger, "info") as mock_info:
            plugin._log_debug("test message")
            mock_info.assert_not_called()

    def test_log_debug_missing_setting_defaults_warn(self, plugin):
        """_log_debug does not log when log_level key is missing (defaults to warn)."""
        from unittest.mock import patch

        import decky

        plugin.settings.pop("log_level", None)
        with patch.object(decky.logger, "info") as mock_info:
            plugin._log_debug("test message")
            mock_info.assert_not_called()

    @pytest.mark.asyncio
    async def test_save_log_level_valid(self, plugin, tmp_path):
        import decky

        plugin._persistence = PersistenceAdapter(str(tmp_path), decky.DECKY_PLUGIN_RUNTIME_DIR, decky.logger)
        for level in ("debug", "info", "warn", "error"):
            result = await plugin.save_log_level(level)
            assert result["success"] is True
            assert plugin.settings["log_level"] == level

    @pytest.mark.asyncio
    async def test_save_log_level_invalid(self, plugin, tmp_path):
        import decky

        plugin._persistence = PersistenceAdapter(str(tmp_path), decky.DECKY_PLUGIN_RUNTIME_DIR, decky.logger)
        plugin.settings["log_level"] = "warn"
        result = await plugin.save_log_level("verbose")
        assert result["success"] is False
        assert plugin.settings["log_level"] == "warn"  # unchanged

    @pytest.mark.asyncio
    async def test_get_settings_includes_log_level(self, plugin):
        plugin.settings["log_level"] = "info"
        result = await plugin.get_settings()
        assert result["log_level"] == "info"

    @pytest.mark.asyncio
    async def test_get_settings_defaults_log_level_warn(self, plugin):
        plugin.settings.pop("log_level", None)
        result = await plugin.get_settings()
        assert result["log_level"] == "warn"

    @pytest.mark.asyncio
    async def test_frontend_log_respects_level(self, plugin):
        """frontend_log only logs when message level >= configured level."""
        from unittest.mock import patch

        import decky

        plugin.settings["log_level"] = "warn"
        with (
            patch.object(decky.logger, "info") as mock_info,
            patch.object(decky.logger, "warning") as mock_warning,
            patch.object(decky.logger, "error") as mock_error,
        ):
            await plugin.frontend_log("debug", "debug msg")
            await plugin.frontend_log("info", "info msg")
            await plugin.frontend_log("warn", "warn msg")
            await plugin.frontend_log("error", "error msg")
            mock_info.assert_not_called()
            mock_warning.assert_called_once_with("[FE] warn msg")
            mock_error.assert_called_once_with("[FE] error msg")

    @pytest.mark.asyncio
    async def test_frontend_log_debug_level_logs_all(self, plugin):
        """With log_level=debug, all levels are logged."""
        from unittest.mock import patch

        import decky

        plugin.settings["log_level"] = "debug"
        with (
            patch.object(decky.logger, "info") as mock_info,
            patch.object(decky.logger, "warning") as mock_warning,
            patch.object(decky.logger, "error") as mock_error,
        ):
            await plugin.frontend_log("debug", "d")
            await plugin.frontend_log("info", "i")
            await plugin.frontend_log("warn", "w")
            await plugin.frontend_log("error", "e")
            assert mock_info.call_count == 2  # debug + info both use logger.info
            mock_warning.assert_called_once_with("[FE] w")
            mock_error.assert_called_once_with("[FE] e")

    @pytest.mark.asyncio
    async def test_debug_log_backward_compat(self, plugin):
        """debug_log callable delegates to frontend_log('debug', ...)."""
        from unittest.mock import patch

        import decky

        plugin.settings["log_level"] = "debug"
        with patch.object(decky.logger, "info") as mock_info:
            await plugin.debug_log("test backward compat")
            mock_info.assert_called_once_with("[FE] test backward compat")

    def test_migration_debug_logging_true(self, plugin, tmp_path):
        """Old debug_logging=True migrates to log_level='debug'."""
        import logging

        from adapters.persistence import PersistenceAdapter
        from domain.state_migrations import migrate_settings

        settings_path = os.path.join(str(tmp_path), "settings.json")
        os.makedirs(str(tmp_path), exist_ok=True)
        with open(settings_path, "w") as f:
            json.dump({"debug_logging": True, "romm_url": ""}, f)
        persistence = PersistenceAdapter(str(tmp_path), str(tmp_path), logging.getLogger("test"))
        plugin.settings = migrate_settings(persistence.load_settings())
        assert "debug_logging" not in plugin.settings
        assert plugin.settings["log_level"] == "debug"

    def test_migration_debug_logging_false(self, plugin, tmp_path):
        """Old debug_logging=False migrates to log_level='warn' (default)."""
        import logging

        from adapters.persistence import PersistenceAdapter
        from domain.state_migrations import migrate_settings

        settings_path = os.path.join(str(tmp_path), "settings.json")
        os.makedirs(str(tmp_path), exist_ok=True)
        with open(settings_path, "w") as f:
            json.dump({"debug_logging": False, "romm_url": ""}, f)
        persistence = PersistenceAdapter(str(tmp_path), str(tmp_path), logging.getLogger("test"))
        plugin.settings = migrate_settings(persistence.load_settings())
        assert "debug_logging" not in plugin.settings
        assert plugin.settings["log_level"] == "warn"

    @pytest.mark.asyncio
    async def test_sgdb_artwork_silent_when_debug_off(self, plugin, tmp_path):
        """SGDB artwork info calls should not log when log_level is 'warn'."""
        from unittest.mock import patch

        import decky

        plugin.settings["log_level"] = "warn"
        with patch.object(decky.logger, "info") as mock_info:
            result = await plugin.get_sgdb_artwork_base64(1, 99)
            assert result["base64"] is None
            for call in mock_info.call_args_list:
                assert "SGDB artwork" not in str(call)

    @pytest.mark.asyncio
    async def test_sgdb_artwork_logs_when_debug_enabled(self, plugin, tmp_path):
        """SGDB artwork info calls should log when log_level is 'debug'."""
        from unittest.mock import patch

        import decky

        plugin.settings["log_level"] = "debug"
        plugin.settings["steamgriddb_api_key"] = ""
        plugin._state["shortcut_registry"]["1"] = {"sgdb_id": None, "igdb_id": None}
        with patch.object(decky.logger, "info") as mock_info:
            result = await plugin.get_sgdb_artwork_base64(1, 1)
            assert result["no_api_key"] is True
            logged_msgs = [str(c) for c in mock_info.call_args_list]
            assert any("SGDB artwork" in m for m in logged_msgs)


class TestInsecureSslSetting:
    def test_load_settings_defaults_false(self, plugin, tmp_path):
        import logging

        from adapters.persistence import PersistenceAdapter

        settings_path = os.path.join(str(tmp_path), "settings.json")
        os.makedirs(str(tmp_path), exist_ok=True)
        with open(settings_path, "w") as f:
            json.dump({"romm_url": "https://romm.local"}, f)
        persistence = PersistenceAdapter(str(tmp_path), str(tmp_path), logging.getLogger("test"))
        plugin.settings = persistence.load_settings()
        assert plugin.settings["romm_allow_insecure_ssl"] is False

    @pytest.mark.asyncio
    async def test_get_settings_includes_field(self, plugin):
        plugin.settings["romm_allow_insecure_ssl"] = True
        result = await plugin.get_settings()
        assert result["romm_allow_insecure_ssl"] is True

    @pytest.mark.asyncio
    async def test_get_settings_defaults_false(self, plugin):
        plugin.settings.pop("romm_allow_insecure_ssl", None)
        result = await plugin.get_settings()
        assert result["romm_allow_insecure_ssl"] is False

    @pytest.mark.asyncio
    async def test_save_settings_with_insecure_ssl(self, plugin, tmp_path):
        import decky

        plugin._persistence = PersistenceAdapter(str(tmp_path), decky.DECKY_PLUGIN_RUNTIME_DIR, decky.logger)
        await plugin.save_settings("https://romm.local", "user", "pass", True)
        assert plugin.settings["romm_allow_insecure_ssl"] is True

    @pytest.mark.asyncio
    async def test_save_settings_without_param_preserves(self, plugin, tmp_path):
        import decky

        plugin._persistence = PersistenceAdapter(str(tmp_path), decky.DECKY_PLUGIN_RUNTIME_DIR, decky.logger)
        plugin.settings["romm_allow_insecure_ssl"] = True
        await plugin.save_settings("https://romm.local", "user", "pass")
        assert plugin.settings["romm_allow_insecure_ssl"] is True

    @pytest.mark.asyncio
    async def test_save_settings_explicit_false(self, plugin, tmp_path):
        import decky

        plugin._persistence = PersistenceAdapter(str(tmp_path), decky.DECKY_PLUGIN_RUNTIME_DIR, decky.logger)
        plugin.settings["romm_allow_insecure_ssl"] = True
        await plugin.save_settings("https://romm.local", "user", "pass", False)
        assert plugin.settings["romm_allow_insecure_ssl"] is False


class TestSettingsFilePermissions:
    def test_save_settings_creates_file_with_0600(self, plugin, tmp_path):
        import decky

        plugin._persistence = PersistenceAdapter(str(tmp_path), str(tmp_path), decky.logger)
        plugin.settings = {"romm_url": "http://example.com"}
        SettingsPersisterAdapter(plugin._persistence, plugin.settings).save_settings()
        settings_path = tmp_path / "settings.json"
        mode = os.stat(settings_path).st_mode & 0o777
        assert mode == 0o600

    def test_load_settings_fixes_permissions(self, plugin, tmp_path):
        import logging

        from adapters.persistence import PersistenceAdapter

        settings_path = tmp_path / "settings.json"
        import json as _json

        with open(settings_path, "w") as f:
            _json.dump({"romm_url": "http://example.com"}, f)
        os.chmod(settings_path, 0o644)
        assert os.stat(settings_path).st_mode & 0o777 == 0o644
        persistence = PersistenceAdapter(str(tmp_path), str(tmp_path), logging.getLogger("test"))
        persistence.load_settings()
        assert os.stat(settings_path).st_mode & 0o777 == 0o600


class TestAtomicSettingsWrite:
    def test_settings_written_atomically(self, plugin, tmp_path):
        import decky

        plugin._persistence = PersistenceAdapter(str(tmp_path), decky.DECKY_PLUGIN_RUNTIME_DIR, decky.logger)

        plugin.settings = {"romm_url": "http://example.com", "romm_user": "user"}
        SettingsPersisterAdapter(plugin._persistence, plugin.settings).save_settings()

        settings_path = tmp_path / "settings.json"
        with open(settings_path) as f:
            data = json.load(f)
        assert data["romm_url"] == "http://example.com"
        assert data["romm_user"] == "user"

    def test_settings_no_tmp_left_after_write(self, plugin, tmp_path):
        import decky

        plugin._persistence = PersistenceAdapter(str(tmp_path), decky.DECKY_PLUGIN_RUNTIME_DIR, decky.logger)

        plugin.settings = {"romm_url": "http://example.com"}
        SettingsPersisterAdapter(plugin._persistence, plugin.settings).save_settings()

        tmp_file = tmp_path / "settings.json.tmp"
        assert not tmp_file.exists()

    def test_settings_crash_preserves_original(self, plugin, tmp_path):
        from unittest.mock import patch

        import decky

        plugin._persistence = PersistenceAdapter(str(tmp_path), decky.DECKY_PLUGIN_RUNTIME_DIR, decky.logger)

        # Write initial settings
        plugin.settings = {"romm_url": "http://original.com"}
        persister = SettingsPersisterAdapter(plugin._persistence, plugin.settings)
        persister.save_settings()

        # Now simulate a crash during json.dump
        plugin.settings["romm_url"] = "http://corrupted.com"
        with patch("json.dump", side_effect=OSError("disk full")), pytest.raises(OSError):
            persister.save_settings()

        # Original file should still be intact
        settings_path = tmp_path / "settings.json"
        with open(settings_path) as f:
            data = json.load(f)
        assert data["romm_url"] == "http://original.com"


class TestWhitelistSettings:
    @pytest.mark.asyncio
    async def test_get_whitelist_defaults_empty(self, plugin):
        """Returns empty lists when no whitelist keys exist in settings."""
        plugin.settings.pop("whitelist_disabled_defaults", None)
        plugin.settings.pop("whitelist_custom_names", None)
        result = await plugin.get_whitelist_settings()
        assert result == {"disabled_defaults": [], "custom_names": []}

    @pytest.mark.asyncio
    async def test_update_and_get_whitelist(self, plugin, tmp_path):
        """Round-trip: update then get returns the stored values."""
        import decky

        plugin._persistence = PersistenceAdapter(str(tmp_path), decky.DECKY_PLUGIN_RUNTIME_DIR, decky.logger)
        await plugin.update_whitelist_settings(["chrome"], ["My App"])
        result = await plugin.get_whitelist_settings()
        assert result["disabled_defaults"] == ["chrome"]
        assert result["custom_names"] == ["My App"]

    @pytest.mark.asyncio
    async def test_update_whitelist_validates_disabled_defaults(self, plugin):
        """Rejects non-list disabled_defaults."""
        result = await plugin.update_whitelist_settings("not-a-list", [])
        assert result["success"] is False
        assert "disabled_defaults" in result["message"]

    @pytest.mark.asyncio
    async def test_update_whitelist_validates_custom_names(self, plugin):
        """Rejects non-list custom_names."""
        result = await plugin.update_whitelist_settings([], "not-a-list")
        assert result["success"] is False
        assert "custom_names" in result["message"]

    @pytest.mark.asyncio
    async def test_update_whitelist_validates_inner_types(self, plugin):
        """Rejects lists containing non-string items."""
        result_dd = await plugin.update_whitelist_settings([1, 2], [])
        assert result_dd["success"] is False
        assert "disabled_defaults" in result_dd["message"]

        result_cn = await plugin.update_whitelist_settings([], ["valid", 42])
        assert result_cn["success"] is False
        assert "custom_names" in result_cn["message"]

    @pytest.mark.asyncio
    async def test_update_whitelist_persists(self, plugin, tmp_path):
        """Verifies values are stored in plugin.settings dict after update."""
        import decky

        plugin._persistence = PersistenceAdapter(str(tmp_path), decky.DECKY_PLUGIN_RUNTIME_DIR, decky.logger)
        result = await plugin.update_whitelist_settings(["moonlight"], ["Custom Game"])
        assert result["success"] is True
        assert plugin.settings["whitelist_disabled_defaults"] == ["moonlight"]
        assert plugin.settings["whitelist_custom_names"] == ["Custom Game"]



class TestFrontendUnsupportedSurfacing:
    """``bootstrap`` may refuse to wire when the host frontend is out of
    band. ``Plugin._main`` catches the typed error, parks the payload on
    ``_frontend_unsupported``, and the ``test_connection`` callable
    short-circuits with the discriminated-status response shape so the
    frontend UI can render an unsupported-version banner.
    """

    @pytest.mark.asyncio
    async def test_main_catches_frontend_unsupported_and_skips_wiring(self):
        from unittest.mock import patch

        from lib.errors import FrontendUnsupportedError
        from main import Plugin

        plugin = Plugin()
        plugin.loop = asyncio.get_event_loop()
        exc = FrontendUnsupportedError(
            frontend="EmuDeck",
            detected="esde:5,ra:2,srm:99",
            expected_min="esde:5,ra:2,srm:9",
            expected_max="esde:5,ra:2,srm:9",
        )
        from unittest.mock import AsyncMock

        with (
            patch("main.bootstrap", side_effect=exc),
            patch("main.wire_services") as wire,
            patch("main.decky.emit", new=AsyncMock()) as emit,
        ):
            await plugin._main()

        wire.assert_not_called()
        emit.assert_awaited_once()
        assert emit.call_args.args[0] == "frontend_unsupported"
        assert plugin._frontend_unsupported == {
            "frontend": "EmuDeck",
            "detected": "esde:5,ra:2,srm:99",
            "expected_min": "esde:5,ra:2,srm:9",
            "expected_max": "esde:5,ra:2,srm:9",
        }

    @pytest.mark.asyncio
    async def test_test_connection_short_circuits_when_frontend_unsupported(self, plugin):
        plugin._frontend_unsupported = {
            "frontend": "EmuDeck",
            "detected": "esde:5,ra:2,srm:99",
            "expected_min": "esde:5,ra:2,srm:9",
            "expected_max": "esde:5,ra:2,srm:9",
        }
        # Underlying connection service must NOT be called when the
        # frontend is unsupported — wire it to a sentinel that would
        # fail the test if invoked.
        plugin._connection_service = MagicMock()
        plugin._connection_service.test_connection = MagicMock(
            side_effect=AssertionError("connection service must not be queried")
        )

        result = await plugin.test_connection()

        assert result["success"] is False
        assert result["error_code"] == "version_unsupported"
        assert result["version_unsupported"] == plugin._frontend_unsupported
        assert "EmuDeck" in result["message"]
        assert "esde:5,ra:2,srm:99" in result["message"]

    @pytest.mark.asyncio
    async def test_test_connection_passes_through_when_frontend_supported(self, plugin):
        from unittest.mock import AsyncMock

        plugin._frontend_unsupported = None
        plugin._connection_service = MagicMock()
        plugin._connection_service.test_connection = AsyncMock(return_value={"success": True})
        result = await plugin.test_connection()
        assert result == {"success": True}
        plugin._connection_service.test_connection.assert_awaited_once()
