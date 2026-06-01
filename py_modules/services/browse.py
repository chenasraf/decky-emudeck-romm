"""BrowseService — paginated RomM ROM browse for the Library tab.

Wraps the RomM transport's ``browse_roms`` Protocol method so the
Decky callable doesn't reach into the adapter directly. Stays a thin
orchestration layer: input normalisation, error classification, and
the canonical-failure response shape live here; the HTTP work lives
in ``adapters/romm``.
"""

from __future__ import annotations

import base64
from collections import OrderedDict
from dataclasses import dataclass
from typing import TYPE_CHECKING

from lib.errors import classify_error

if TYPE_CHECKING:
    import asyncio
    import logging

    from services.protocols import RommRomReader


_COVER_CACHE_LIMIT = 120


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
        self._cover_cache: OrderedDict[int, str] = OrderedDict()

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

    async def get_cover_base64(self, rom_id: int) -> dict:
        """Fetch a ROM's RomM cover and return it base64-encoded.

        LRU-cached in-memory so re-renders of the Library grid don't
        re-hit the RomM server. Returns ``{base64: None}`` when the ROM
        has no cover path; RomM transport errors surface as the canonical
        failure shape so the frontend can render a placeholder.
        """
        cached = self._cover_cache.get(rom_id)
        if cached is not None:
            self._cover_cache.move_to_end(rom_id)
            return {"success": True, "base64": cached, "mime": _sniff_mime(_b64_head(cached))}

        try:
            rom = await self._loop.run_in_executor(None, self._romm_api.get_rom, rom_id)
        except Exception as e:
            self._logger.warning(f"get_rom failed for cover lookup rom_id={rom_id}: {e}")
            code, msg = classify_error(e)
            return {"success": False, "message": msg, "error_code": code, "base64": None}

        cover_url = (
            (rom or {}).get("path_cover_small")
            or (rom or {}).get("path_cover_large")
            or (rom or {}).get("url_cover")
        )
        if not cover_url:
            self._logger.info(f"rom_id={rom_id} has no cover path; skipping")
            return {"success": True, "base64": None}

        try:
            raw = await self._loop.run_in_executor(None, self._romm_api.download_cover_bytes, cover_url)
        except Exception as e:
            self._logger.warning(f"download_cover_bytes failed for rom_id={rom_id} url={cover_url!r}: {e}")
            code, msg = classify_error(e)
            return {"success": False, "message": msg, "error_code": code, "base64": None}

        if not raw:
            self._logger.info(f"rom_id={rom_id} cover download returned 0 bytes (url={cover_url!r})")
            return {"success": True, "base64": None}

        mime = _sniff_mime(raw[:16])
        encoded = base64.b64encode(raw).decode("ascii")
        self._cover_cache[rom_id] = encoded
        if len(self._cover_cache) > _COVER_CACHE_LIMIT:
            self._cover_cache.popitem(last=False)
        return {"success": True, "base64": encoded, "mime": mime}


def _sniff_mime(head: bytes) -> str:
    """Return the image MIME type for the first few bytes of a payload.

    Recognises JPEG / PNG / GIF / WebP — the realistic spread for RomM
    covers. Falls back to ``image/jpeg`` (the dominant cover type) so the
    frontend ``data:`` URL still renders for unknown formats.
    """
    if head.startswith(b"\x89PNG"):
        return "image/png"
    if head.startswith(b"\xff\xd8"):
        return "image/jpeg"
    if head.startswith(b"GIF87a") or head.startswith(b"GIF89a"):
        return "image/gif"
    if head[:4] == b"RIFF" and head[8:12] == b"WEBP":
        return "image/webp"
    return "image/jpeg"


def _b64_head(encoded: str) -> bytes:
    """Decode just the first few bytes of a base64 string for MIME sniffing."""
    try:
        return base64.b64decode(encoded[:24])
    except Exception:
        return b""
