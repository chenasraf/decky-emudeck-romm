import asyncio
from datetime import UTC, datetime
from pathlib import Path
from unittest.mock import AsyncMock, MagicMock

import pytest
from fakes.fake_core_info_provider import FakeCoreInfoProvider
from fakes.fake_firmware_cache_persister import FakeFirmwareCachePersister
from fakes.fake_frontend import FakeFrontend
from fakes.fake_migration_file_store import FakeMigrationFileStore
from fakes.fake_settings_persister import FakeSettingsPersister
from fakes.fake_state_persister import FakeStatePersister
from fakes.library_peers import FakeArtworkManager, FakeMetadataExtractor
from fakes.system_time import FakeClock, FakeSleeper, FakeUuidGen
from models.state import make_default_plugin_state

from adapters.firmware_file import FirmwareFileAdapter
from adapters.migration_file import MigrationFileAdapter
from adapters.persistence import PersistenceAdapter
from adapters.registry_store import RegistryStoreAdapter
from adapters.steam_config import SteamConfigAdapter

# conftest.py patches decky before this import
from main import Plugin
from services.firmware import FirmwareService, FirmwareServiceConfig
from services.library import LibraryService, LibraryServiceConfig
from services.migration import MigrationService, MigrationServiceConfig


def _frontend(
    *, home: str = "/tmp/r", saves: str = "/tmp/s", roms: str = "/tmp/r", bios: str = "/tmp/b"
) -> FakeFrontend:
    """Build a FakeFrontend with the legacy str-based kwargs the test suite was written against."""
    return FakeFrontend(
        rom_root=Path(roms),
        bios_root=Path(bios),
        save_root=Path(saves),
        home=Path(home),
    )


class RecordingEmitter:
    """Append-only emit recorder usable as an ``EventEmitter``."""

    def __init__(self) -> None:
        self.calls: list[tuple[str, tuple[object, ...]]] = []

    async def __call__(self, event: str, /, *args: object) -> None:
        self.calls.append((event, args))


@pytest.fixture
def plugin(tmp_path, fake_romm_api):
    p = Plugin()
    p.settings = {"romm_url": "", "romm_user": "", "romm_pass": "", "enabled_platforms": {}}
    p._http_adapter = MagicMock()
    p._state = make_default_plugin_state()
    p._metadata_cache = {}

    import decky

    p._persistence = PersistenceAdapter(str(tmp_path), str(tmp_path), decky.logger)
    steam_config = SteamConfigAdapter(user_home=decky.DECKY_USER_HOME, logger=decky.logger)
    p._steam_config = steam_config

    p._romm_api = fake_romm_api
    p._state_persister = FakeStatePersister()
    p._settings_persister = FakeSettingsPersister()
    p._firmware_service = FirmwareService(
        config=FirmwareServiceConfig(
            romm_api=fake_romm_api,
            state=p._state,
            loop=asyncio.get_event_loop(),
            logger=decky.logger,
            plugin_dir=decky.DECKY_PLUGIN_DIR,
            clock=FakeClock(now=datetime(2026, 1, 1, tzinfo=UTC)),
            state_persister=FakeStatePersister(),
            firmware_cache_persister=FakeFirmwareCachePersister(),
            firmware_file_store=FirmwareFileAdapter(),
            frontend=_frontend(),
            core_info=FakeCoreInfoProvider(),
        ),
    )
    p._firmware_service.load_bios_registry()

    p._sync_service = LibraryService(
        config=LibraryServiceConfig(
            romm_api=fake_romm_api,
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
            registry_store=RegistryStoreAdapter(state=p._state, logger=decky.logger),
            log_debug=p._log_debug,
            metadata_service=FakeMetadataExtractor(),
            artwork=FakeArtworkManager(),
        ),
    )

    def _no_active_core(system_name: str, rom_filename: str | None = None) -> tuple[str | None, str | None]:
        return (None, None)

    def _no_core_name(core_so: str) -> str | None:
        return None

    def _default_save_sorting() -> tuple[bool, bool]:
        return (True, False)

    p._migration_service = MigrationService(
        config=MigrationServiceConfig(
            migration_file_store=MigrationFileAdapter(),
            state=p._state,
            settings=p.settings,
            loop=asyncio.get_event_loop(),
            logger=decky.logger,
            state_persister=p._state_persister,
            settings_persister=p._settings_persister,
            emit=RecordingEmitter(),
            frontend=_frontend(),
            get_retroarch_save_sorting=_default_save_sorting,
            get_active_core=_no_active_core,
            get_core_name=_no_core_name,
        ),
    )
    return p


@pytest.fixture(autouse=True)
async def _set_event_loop(plugin):
    """Ensure plugin.loop and migration service loop match the running event loop."""
    loop = asyncio.get_event_loop()
    plugin.loop = loop
    plugin._migration_service._loop = loop


class TestDetectSaveSortChangeThreadSafety:
    """Regression tests for #238 review finding 1: ``detect_save_sort_change``
    is called from a worker thread (via ``SaveService._refresh_save_sort_state``
    → ``run_in_executor``) and must schedule the emit coroutine in a
    thread-safe manner. ``loop.create_task`` is NOT thread-safe — it must
    be ``asyncio.run_coroutine_threadsafe``.
    """

    async def test_detect_save_sort_change_is_thread_safe_when_called_from_executor(self, plugin):
        """detect_save_sort_change must be safe to call from a worker thread (#238)."""
        loop = asyncio.get_event_loop()
        plugin._migration_service._loop = loop

        # Initial state: a populated OLD layout. Detect should observe a
        # change and emit ``save_sort_changed``.
        plugin._state["save_sort_settings"] = {"sort_by_content": True, "sort_by_core": False}
        plugin._migration_service._get_retroarch_save_sorting = lambda: (True, True)

        emit_queue: asyncio.Queue = asyncio.Queue()

        async def fake_emit(event_name: str, payload: dict) -> None:
            await emit_queue.put((event_name, payload))

        plugin._migration_service._emit = fake_emit

        # Run detect_save_sort_change on a worker thread.
        await loop.run_in_executor(None, plugin._migration_service.detect_save_sort_change)

        event = await asyncio.wait_for(emit_queue.get(), timeout=2.0)
        assert event[0] == "save_sort_changed"
        assert event[1]["old_settings"] == {"sort_by_content": True, "sort_by_core": False}
        assert event[1]["new_settings"] == {"sort_by_content": True, "sort_by_core": True}

        # State is updated via the shared dict — visible to whoever holds it.
        assert plugin._state["save_sort_settings_previous"] == {
            "sort_by_content": True,
            "sort_by_core": False,
        }
        assert plugin._state["save_sort_settings"] == {
            "sort_by_content": True,
            "sort_by_core": True,
        }


class TestSaveSortFailureInjection:
    """Adapter-level failure injection tests using FakeMigrationFileStore.

    These tests exercise paths the tmp_path-based integration tests cannot
    reach: simulated ``OSError`` during ``rename`` / ``remove`` must be
    caught by the service, appended to the ``errors`` list, and must not
    abort the rest of the migration loop.
    """

    def _make_service(self, fake_files, **overrides):
        import decky

        defaults: dict = {
            "state": make_default_plugin_state(),
            "settings": {},
            "loop": asyncio.get_event_loop(),
            "logger": decky.logger,
            "state_persister": FakeStatePersister(),
            "settings_persister": FakeSettingsPersister(),
            "emit": RecordingEmitter(),
            "frontend": _frontend(),
            "get_retroarch_save_sorting": lambda: (False, False),
            "get_active_core": lambda system, rom_filename: (None, None),
            "get_core_name": lambda core_so: None,
        }
        defaults.update(overrides)
        return MigrationService(
            config=MigrationServiceConfig(migration_file_store=fake_files, **defaults),
        )

    def test_rename_failure_records_save_sort_error(self):
        """``OSError`` from ``rename`` during save-sort overwrite path is captured."""
        fake = FakeMigrationFileStore()
        old_path = "/saves/old/game.srm"
        new_path = "/saves/new/game.srm"
        fake.files[old_path] = b"new content"
        fake.files[new_path] = b"old content"
        # Source is newer => triggers rename path.
        fake.mtimes[old_path] = 2000.0
        fake.mtimes[new_path] = 1000.0
        fake.rename_failures.add(old_path)

        service = self._make_service(fake)

        counts: dict[str, int] = {}
        errors: list = []
        state_updates: list[str] = []
        service._resolve_save_sort_conflict(
            label="gba/game.srm",
            old_path=old_path,
            new_path=new_path,
            state_updater=lambda: state_updates.append("called"),
            counts=counts,
            count_key="save",
            errors=errors,
        )

        assert len(errors) == 1
        assert "gba/game.srm" in errors[0]
        assert counts.get("save", 0) == 0
        # Failure path must not invoke the state updater.
        assert state_updates == []

    def test_remove_failure_records_save_sort_orphan_cleanup_error(self):
        """``OSError`` from ``remove`` during save-sort newest-wins cleanup is captured."""
        fake = FakeMigrationFileStore()
        old_path = "/saves/old/game.srm"
        new_path = "/saves/new/game.srm"
        fake.files[old_path] = b"stale"
        fake.files[new_path] = b"fresh"
        # Destination is newer => triggers orphan-removal path.
        fake.mtimes[old_path] = 1000.0
        fake.mtimes[new_path] = 2000.0
        fake.remove_failures.add(old_path)

        service = self._make_service(fake)

        counts: dict[str, int] = {}
        errors: list = []
        state_updates: list[str] = []
        service._resolve_save_sort_conflict(
            label="gba/game.srm",
            old_path=old_path,
            new_path=new_path,
            state_updater=lambda: state_updates.append("called"),
            counts=counts,
            count_key="save",
            errors=errors,
        )

        assert len(errors) == 1
        assert "gba/game.srm" in errors[0]
        assert counts.get("save", 0) == 0
        # Failure path must not invoke the state updater.
        assert state_updates == []


class TestRefreshState:
    """Tests for ``MigrationService.refresh_state``.

    These tests exercise the orchestration contract: ``refresh_state``
    drives ``detect_save_sort_change`` then composes the save-sort
    status output.
    """

    @pytest.mark.asyncio
    async def test_calls_detect_and_returns_save_sort_status(self, plugin):
        mig = plugin._migration_service
        mig.detect_save_sort_change = MagicMock()

        save_sort_status = {"pending": True, "saves_count": 3}
        mig.get_save_sort_migration_status = AsyncMock(return_value=save_sort_status)

        result = await mig.refresh_state()

        mig.detect_save_sort_change.assert_called_once_with()
        assert result == {"save_sort": save_sort_status}

    @pytest.mark.asyncio
    async def test_short_circuits_when_detect_raises(self, plugin):
        mig = plugin._migration_service
        mig.detect_save_sort_change = MagicMock(side_effect=RuntimeError("boom"))
        mig.get_save_sort_migration_status = AsyncMock()

        with pytest.raises(RuntimeError, match="boom"):
            await mig.refresh_state()

        mig.get_save_sort_migration_status.assert_not_called()


class TestBadPathDismissSaveSortMigration:
    """Coverage for the ``dismiss_save_sort_migration`` callable."""

    def test_dismiss_save_sort_migration_clears_state_and_persists(self, plugin):
        """User dismissing the warning pops the marker and persists state once."""
        plugin._state["save_sort_settings_previous"] = {
            "sort_by_content": True,
            "sort_by_core": False,
        }
        persister = plugin._migration_service._state_persister
        saves_before = persister.save_count

        result = plugin._migration_service.dismiss_save_sort_migration()

        assert result == {"success": True}
        assert "save_sort_settings_previous" not in plugin._state
        assert persister.save_count == saves_before + 1


class TestApplySettingsSchemaMigrations:
    """Tests for apply_settings_schema_migrations() — settings-schema bumps (#738)."""

    def test_v0_clears_last_sync(self, plugin):
        """Pre-versioning settings (version=0) trigger the v1→v2 migration: clear last_sync."""
        plugin.settings["version"] = 0
        plugin._state["last_sync"] = "2025-01-01T00:00:00Z"

        plugin._migration_service.apply_settings_schema_migrations()

        assert plugin._state["last_sync"] is None

    def test_v1_clears_last_sync(self, plugin):
        """Version 1 settings (pre-fetch-apply-split) trigger the migration."""
        plugin.settings["version"] = 1
        plugin._state["last_sync"] = "2025-01-01T00:00:00Z"

        plugin._migration_service.apply_settings_schema_migrations()

        assert plugin._state["last_sync"] is None

    def test_v2_is_noop(self, plugin):
        """Version 2 settings (post-fetch-apply-split) are already migrated — preserve last_sync."""
        plugin.settings["version"] = 2
        plugin._state["last_sync"] = "2025-01-01T00:00:00Z"

        plugin._migration_service.apply_settings_schema_migrations()

        assert plugin._state["last_sync"] == "2025-01-01T00:00:00Z"

    def test_persists_state_after_migration(self, plugin):
        """The state-persister is invoked so the cleared last_sync lands on disk."""
        plugin.settings["version"] = 1
        plugin._state["last_sync"] = "2025-01-01T00:00:00Z"

        save_count_before = plugin._state_persister.save_count
        plugin._migration_service.apply_settings_schema_migrations()
        assert plugin._state_persister.save_count > save_count_before

    def test_persists_settings_after_migration(self, plugin):
        """The settings-persister is invoked so the version-bump stamp lands on disk."""
        plugin.settings["version"] = 1
        plugin._state["last_sync"] = "2025-01-01T00:00:00Z"

        save_count_before = plugin._settings_persister.save_count
        plugin._migration_service.apply_settings_schema_migrations()
        assert plugin._settings_persister.save_count > save_count_before

    def test_v2_does_not_persist(self, plugin):
        """When already at v2, no persistence calls are made (avoid spurious disk writes)."""
        plugin.settings["version"] = 2

        state_save_before = plugin._state_persister.save_count
        settings_save_before = plugin._settings_persister.save_count

        plugin._migration_service.apply_settings_schema_migrations()

        assert plugin._state_persister.save_count == state_save_before
        assert plugin._settings_persister.save_count == settings_save_before

    def test_missing_version_treated_as_zero(self, plugin):
        """Settings without a ``version`` field are treated as v0 (oldest possible)."""
        plugin.settings.pop("version", None)
        plugin._state["last_sync"] = "2025-01-01T00:00:00Z"

        plugin._migration_service.apply_settings_schema_migrations()

        assert plugin._state["last_sync"] is None
