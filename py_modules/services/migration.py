"""MigrationService — RetroArch save-sort migration orchestration.

Owns the runtime decisions for relocating save files when RetroArch
save sorting flips. All raw filesystem I/O is delegated to the
``MigrationFileStore`` Protocol; conflict resolution, state mutations,
and event emission remain the service's responsibility.

Also hosts the one-shot settings-schema migration applied at plugin
start (``apply_settings_schema_migrations``) — it shares the same
state/settings/persistence wiring as the save-sort flow.
"""

from __future__ import annotations

import asyncio
import os
from dataclasses import dataclass
from typing import TYPE_CHECKING

from models.state import InstalledRomEntry, PluginState, SaveSortSettings

from domain.save_extensions import get_save_extensions
from domain.save_path import resolve_save_dir

if TYPE_CHECKING:
    import logging

    from services.protocols import (
        CoreNameProviderFn,
        CoreResolverFn,
        EventEmitter,
        Frontend,
        MigrationFileStore,
        RetroArchSaveSortingProvider,
        SettingsPersister,
        StatePersister,
    )


# Settings schema version that introduced the fetch/apply split (#738).
# Pre-v2 plugin runs may have left the metadata cache corrupted by a
# delta-sync that overwrote populated entries with empty ones. Clearing
# ``last_sync`` on the v1→v2 hop forces the next sync to do a full
# fetch, which re-stamps the cache from real ROMs.
_SETTINGS_VERSION_FETCH_APPLY_SPLIT = 2


@dataclass(frozen=True)
class MigrationServiceConfig:
    """Frozen wiring bundle handed to ``MigrationService.__init__``.

    Holds the Protocol-typed migration-file adapter, the live state
    and settings dicts, runtime infrastructure, persistence callbacks,
    event emitter, and the provider callables MigrationService needs
    at construction time.
    """

    migration_file_store: MigrationFileStore
    state: PluginState
    settings: dict
    loop: asyncio.AbstractEventLoop
    logger: logging.Logger
    state_persister: StatePersister
    settings_persister: SettingsPersister
    emit: EventEmitter
    frontend: Frontend
    get_retroarch_save_sorting: RetroArchSaveSortingProvider
    get_active_core: CoreResolverFn
    get_core_name: CoreNameProviderFn


class MigrationService:
    """Handles RetroArch save-sort change detection and file migration."""

    def __init__(self, *, config: MigrationServiceConfig) -> None:
        self._migration_file_store = config.migration_file_store
        self._state = config.state
        self._settings = config.settings
        self._loop = config.loop
        self._logger = config.logger
        self._state_persister = config.state_persister
        self._settings_persister = config.settings_persister
        self._emit = config.emit
        self._frontend = config.frontend
        self._get_retroarch_save_sorting = config.get_retroarch_save_sorting
        self._get_active_core = config.get_active_core
        self._get_core_name = config.get_core_name
        # Strong refs to in-flight background tasks. ``loop.create_task``
        # alone is not enough — without a strong ref, the loop is free to
        # garbage-collect the task before it completes. ``add_done_callback``
        # prunes finished entries to keep the set bounded.
        self._background_tasks: set[asyncio.Task] = set()

    def _spawn_background_task(self, coro) -> asyncio.Task:
        """Schedule ``coro`` on the plugin loop and track the task for shutdown."""
        task = self._loop.create_task(coro)
        self._background_tasks.add(task)
        task.add_done_callback(self._background_tasks.discard)
        return task

    async def shutdown(self) -> None:
        """Cancel any in-flight background tasks and await their completion."""
        for task in self._background_tasks:
            task.cancel()
        if self._background_tasks:
            await asyncio.gather(*self._background_tasks, return_exceptions=True)

    # ---------------------------------------------------------------------------
    # Settings-schema migrations (#738)
    # ---------------------------------------------------------------------------

    def apply_settings_schema_migrations(self) -> None:
        """Apply one-shot settings-schema migrations on plugin start.

        Plugin runs older than v2 may have left the metadata cache
        corrupted by a delta sync that overwrote populated entries with
        empty ones (#738). The migration clears ``last_sync`` so the
        next sync does a full re-fetch, which re-stamps every cache
        entry from real ROMs. The settings file's ``version`` field is
        stamped on the next ``save_settings`` write (handled by the
        persistence adapter), but we persist immediately here so the
        bump lands even if the user makes no settings changes.
        """
        version = self._settings.get("version", 0)
        if version < _SETTINGS_VERSION_FETCH_APPLY_SPLIT:
            self._state["last_sync"] = None
            self._state_persister.save_state()
            self._settings_persister.save_settings()
            self._logger.info(
                "Settings schema migration v1→v2: cleared last_sync to force full resync (fixes #738 cache corruption)"
            )

    # ---------------------------------------------------------------------------
    # Save-sort change detection and migration
    # ---------------------------------------------------------------------------

    def detect_save_sort_change(self) -> None:
        """Check if RetroArch save sorting settings changed since last run.

        May be called from a worker thread (via
        ``SaveService._refresh_save_sort_state`` → ``run_in_executor``) or
        from the loop thread. Use ``asyncio.run_coroutine_threadsafe`` to
        schedule the emit coroutine: it is explicitly thread-safe and
        also works correctly when invoked from the loop thread itself.
        ``loop.create_task`` is NOT thread-safe and races with loop
        internals on CPython (#238 review).
        """
        sort_by_content, sort_by_core = self._get_retroarch_save_sorting()
        current: SaveSortSettings = {"sort_by_content": sort_by_content, "sort_by_core": sort_by_core}
        stored = self._state.get("save_sort_settings")
        if stored is None:
            self._state["save_sort_settings"] = current
            self._state_persister.save_state()
            return
        if stored == current:
            return
        self._state["save_sort_settings_previous"] = stored
        self._state["save_sort_settings"] = current
        self._state_persister.save_state()
        self._logger.warning(f"RetroArch save sorting changed: {stored} -> {current}")
        # Fire-and-forget: thread-safe schedule of the emit coroutine on
        # the plugin event loop. We deliberately do not await or .result()
        # the future — this mirrors the previous create_task semantics.
        asyncio.run_coroutine_threadsafe(
            self._emit(
                "save_sort_changed",
                {"old_settings": stored, "new_settings": current},
            ),
            self._loop,
        )

    async def refresh_state(self) -> dict:
        """Run the save-sort detection pass and return current migration state.

        Detects any RetroArch save-sort change, then returns the current
        status payload. The session-finalize round-trip consumes this so
        the QAM badge updates without a separate callable.
        """
        self.detect_save_sort_change()
        return {
            "save_sort": await self.get_save_sort_migration_status(),
        }

    def _resolve_retroarch_corename(self, system: str, rom_filename: str) -> tuple[str | None, str | None]:
        """Resolve the RetroArch save subdirectory name for a system/ROM.

        Asks ES-DE (via ``get_active_core``) **which** core is active,
        then asks the RetroArch ``.info`` parser (via ``get_core_name``)
        **what** RetroArch calls that core in its own subsystem — which
        is what ``sort_savefiles_enable`` uses when naming save
        subdirectories.

        Returns a ``(corename, core_so)`` tuple. ``corename`` is ``None``
        (fail loud, no ES-DE label fallback) when the providers cannot
        resolve a core for this system/ROM. ``core_so`` is the underlying
        ES-DE core ``.so`` basename when known (useful for diagnostics
        when ``corename`` is ``None``), otherwise ``None``.
        """
        core_so, _label = self._get_active_core(system, rom_filename)
        if not core_so:
            return (None, None)
        corename = self._get_core_name(core_so)
        return (corename or None, core_so)

    def _collect_save_sorting_items(self, old_settings: SaveSortSettings, new_settings: SaveSortSettings) -> list:
        """Collect save files that need migration due to sort setting change."""
        saves_base = str(self._frontend.saves())
        roms_base = str(self._frontend.roms())
        need_core = bool(old_settings.get("sort_by_core") or new_settings.get("sort_by_core"))
        items: list[tuple[str, str, str, object, str]] = []
        for entry in self._state.get("installed_roms", {}).values():
            self._collect_rom_sort_items(
                entry,
                saves_base,
                roms_base,
                old_settings,
                new_settings,
                need_core,
                items,
            )
        return items

    def _collect_rom_sort_items(
        self,
        entry: InstalledRomEntry,
        saves_base: str,
        roms_base: str,
        old_settings: SaveSortSettings,
        new_settings: SaveSortSettings,
        need_core: bool,
        items: list,
    ) -> None:
        """Collect migration items for a single ROM's save files."""
        system = entry.get("system", "")
        file_path = entry.get("file_path", "")
        platform_slug = entry.get("platform_slug", "")
        if not system or not file_path:
            return
        core_name: str | None = None
        if need_core:
            core_name, core_so = self._resolve_retroarch_corename(system, os.path.basename(file_path))
            if core_name is None:
                # Fail loud — cannot resolve the RetroArch corename for this ROM's
                # active core, so we can't build the correct sort-by-core path.
                # Skip this item and warn the user rather than silently corrupting
                # the migration with the wrong destination directory.
                self._logger.warning(
                    "Skipping save sort migration for %s/%s: unable to resolve "
                    "RetroArch corename from .info (core_so=%s)",
                    system,
                    os.path.basename(file_path),
                    core_so,
                )
                return
        old_dir = resolve_save_dir(
            file_path,
            saves_base,
            system,
            roms_base=roms_base,
            sort_by_content=old_settings["sort_by_content"],
            sort_by_core=old_settings["sort_by_core"],
            core_name=core_name,
        )
        new_dir = resolve_save_dir(
            file_path,
            saves_base,
            system,
            roms_base=roms_base,
            sort_by_content=new_settings["sort_by_content"],
            sort_by_core=new_settings["sort_by_core"],
            core_name=core_name,
        )
        if old_dir == new_dir:
            return
        rom_name = os.path.splitext(os.path.basename(file_path))[0]
        for ext in get_save_extensions(platform_slug):
            filename = rom_name + ext
            old_file = os.path.join(old_dir, filename)
            new_file = os.path.join(new_dir, filename)
            if self._migration_file_store.exists(old_file):
                items.append((filename, old_file, new_file, lambda: None, "save"))

    def _get_save_sort_migration_status_io(
        self, old_settings: SaveSortSettings, new_settings: SaveSortSettings
    ) -> dict:
        items = self._collect_save_sorting_items(old_settings, new_settings)
        return {
            "pending": True,
            "old_settings": old_settings,
            "new_settings": new_settings,
            "saves_count": len(items),
        }

    def dismiss_save_sort_migration(self) -> dict:
        """Dismiss the save sort migration warning without migrating files."""
        self._state.pop("save_sort_settings_previous", None)
        self._state_persister.save_state()
        return {"success": True}

    async def get_save_sort_migration_status(self) -> dict:
        old = self._state.get("save_sort_settings_previous")
        new = self._state.get("save_sort_settings")
        if not old or not new or old == new:
            return {"pending": False}
        return await self._loop.run_in_executor(None, self._get_save_sort_migration_status_io, old, new)

    def _resolve_save_sort_conflict(
        self,
        label: str,
        old_path: str,
        new_path: str,
        state_updater,
        counts: dict,
        count_key: str,
        errors: list,
    ) -> None:
        """Newest-wins resolution for a save-sort conflict.

        RetroArch does not migrate saves when its sort setting changes. If a
        user flips ``sort_savefiles_enable`` mid-game via the Quick Menu and
        then saves in-game, the new progress is written to the new layout
        while the old location still holds pre-change content. The file at
        the newer mtime contains actual user progress; the older one is
        stale and must be cleaned up. Save-sync has already uploaded the
        newest version to RomM before this runs, so even if local migration
        fails the server still holds the authoritative copy.
        """
        try:
            old_mtime = self._migration_file_store.get_mtime(old_path)
            new_mtime = self._migration_file_store.get_mtime(new_path)
        except OSError as e:
            errors.append(f"{label}: {e}")
            self._logger.error(f"Save-sort conflict mtime read failed: {old_path}: {e}")
            return

        if new_mtime >= old_mtime:
            # Destination is newer — keep it, delete the stale orphan at old_path.
            try:
                self._migration_file_store.remove_file(old_path)
                state_updater()
                counts[count_key] = counts.get(count_key, 0) + 1
                self._logger.info(f"Save-sort conflict: kept newer {new_path}, removed stale {old_path}")
            except OSError as e:
                errors.append(f"{label}: {e}")
                self._logger.error(f"Save-sort orphan cleanup failed: {old_path}: {e}")
            return

        # Source is newer — atomically overwrite destination.
        try:
            self._migration_file_store.make_dirs(os.path.dirname(new_path))
            self._migration_file_store.rename(old_path, new_path)
            state_updater()
            counts[count_key] = counts.get(count_key, 0) + 1
            self._logger.info(f"Save-sort conflict: moved newer {old_path} -> {new_path}")
        except OSError as e:
            errors.append(f"{label}: {e}")
            self._logger.error(f"Save-sort overwrite failed: {old_path}: {e}")

    def _migrate_save_sort_item(
        self,
        label: str,
        old_path: str,
        new_path: str,
        state_updater,
        counts: dict,
        errors: list,
    ) -> None:
        """Move a single save file when no conflict exists at the destination.

        Save-sort conflicts (both source and destination exist) are pre-routed
        to ``_resolve_save_sort_conflict``; this helper handles the simple
        no-conflict move and the source-missing recovery case where the file
        already lives at the new location.
        """
        if not self._migration_file_store.exists(old_path):
            if self._migration_file_store.exists(new_path):
                state_updater()
                counts["save"] = counts.get("save", 0) + 1
            return

        try:
            self._migration_file_store.make_dirs(os.path.dirname(new_path))
            self._migration_file_store.move(old_path, new_path)
            state_updater()
            counts["save"] = counts.get("save", 0) + 1
            self._logger.info(f"Migrated save: {old_path} -> {new_path}")
        except OSError as e:
            errors.append(f"{label}: {e}")
            self._logger.error(f"Migration failed: {old_path}: {e}")

    @staticmethod
    def _build_save_sort_result(counts: dict, errors: list) -> dict:
        """Build the result dict from save-sort migration counts and errors."""
        moved = counts.get("save", 0)
        msg = f"Migrated {moved} save(s)" if moved else "No files to migrate"
        if errors:
            msg += f" ({len(errors)} error(s))"
        return {
            "success": len(errors) == 0,
            "message": msg,
            "saves_moved": moved,
            "errors": errors,
        }

    def _migrate_save_sort_files_io(
        self, old_settings: SaveSortSettings, new_settings: SaveSortSettings, conflict_strategy: str | None
    ) -> dict:
        # conflict_strategy is retained for backwards-compatibility with the
        # callable signature but is unused for save-sort migration — conflicts
        # are resolved in place via newest-wins (see _resolve_save_sort_conflict).
        del conflict_strategy
        items = self._collect_save_sorting_items(old_settings, new_settings)
        if not items:
            self._state.pop("save_sort_settings_previous", None)
            self._state_persister.save_state()
            return {"success": True, "message": "No save files to migrate", "saves_moved": 0}
        counts: dict[str, int] = {"save": 0}
        errors: list[str] = []
        for label, old_path, new_path, updater, _kind in items:
            if self._migration_file_store.exists(old_path) and self._migration_file_store.exists(new_path):
                self._resolve_save_sort_conflict(label, old_path, new_path, updater, counts, "save", errors)
            else:
                self._migrate_save_sort_item(label, old_path, new_path, updater, counts, errors)
        if not errors:
            self._state.pop("save_sort_settings_previous", None)
            self._state_persister.save_state()
        return self._build_save_sort_result(counts, errors)

    async def migrate_save_sort_files(self, conflict_strategy: str | None = None) -> dict:
        old = self._state.get("save_sort_settings_previous")
        new = self._state.get("save_sort_settings")
        if not old or not new or old == new:
            return {"success": False, "message": "No save sorting migration needed"}
        return await self._loop.run_in_executor(None, self._migrate_save_sort_files_io, old, new, conflict_strategy)
