"""BrowseService — paginated RomM ROM browse for the Library tab.

Wraps the RomM transport's ``browse_roms`` Protocol method so the
Decky callable doesn't reach into the adapter directly. Stays a thin
orchestration layer: input normalisation, error classification, and
the canonical-failure response shape live here; the HTTP work lives
in ``adapters/romm``.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import TYPE_CHECKING

from lib.errors import classify_error

if TYPE_CHECKING:
    import asyncio
    import logging

    from services.protocols import RommRomReader


@dataclass(frozen=True)
class BrowseServiceConfig:
    """Frozen wiring bundle handed to ``BrowseService.__init__``."""

    romm_api: RommRomReader
    loop: asyncio.AbstractEventLoop
    logger: logging.Logger


class BrowseService:
    """Paginated browse over the RomM library."""

    def __init__(self, *, config: BrowseServiceConfig) -> None:
        self._romm_api = config.romm_api
        self._loop = config.loop
        self._logger = config.logger

    async def browse_roms(
        self,
        platform_ids: list[int] | None,
        search: str | None,
        limit: int,
        offset: int,
    ) -> dict:
        try:
            page = await self._loop.run_in_executor(
                None,
                self._romm_api.browse_roms,
                platform_ids or None,
                search or None,
                limit,
                offset,
            )
        except Exception as e:
            self._logger.error(f"browse_roms failed: {e}")
            code, msg = classify_error(e)
            return {"success": False, "message": msg, "error_code": code}

        if not isinstance(page, dict):
            self._logger.error(f"Unexpected browse_roms response type: {type(page).__name__}")
            return {"success": False, "message": "Invalid server response", "error_code": "api_error"}

        items = page.get("items", []) if isinstance(page.get("items"), list) else []
        total = page.get("total", 0) if isinstance(page.get("total"), int) else 0
        return {"success": True, "items": items, "total": total}
