"""Tests for BrowseService — paginated RomM ROM browse."""

from __future__ import annotations

import asyncio
import logging
from unittest.mock import MagicMock

import pytest

from services.browse import BrowseService, BrowseServiceConfig


def _make_service(romm_api):
    return BrowseService(
        config=BrowseServiceConfig(
            romm_api=romm_api,
            loop=asyncio.get_event_loop(),
            logger=logging.getLogger("test"),
        ),
    )


class TestBrowseRoms:
    @pytest.mark.asyncio
    async def test_returns_paginated_items_and_total(self, fake_romm_api):
        fake_romm_api.roms = {
            1: {"id": 1, "name": "Zelda", "platform_id": 5},
            2: {"id": 2, "name": "Metroid", "platform_id": 5},
            3: {"id": 3, "name": "Mario", "platform_id": 6},
        }
        service = _make_service(fake_romm_api)
        result = await service.browse_roms(None, None, 30, 0)
        assert result["success"] is True
        assert result["total"] == 3
        assert len(result["items"]) == 3

    @pytest.mark.asyncio
    async def test_filters_by_multiple_platform_ids(self, fake_romm_api):
        fake_romm_api.roms = {
            1: {"id": 1, "name": "Zelda", "platform_id": 5},
            2: {"id": 2, "name": "Mario", "platform_id": 6},
            3: {"id": 3, "name": "Sonic", "platform_id": 7},
        }
        service = _make_service(fake_romm_api)
        result = await service.browse_roms([5, 6], None, 30, 0)
        assert result["success"] is True
        assert result["total"] == 2
        ids = sorted(item["id"] for item in result["items"])
        assert ids == [1, 2]

    @pytest.mark.asyncio
    async def test_filters_by_search_substring(self, fake_romm_api):
        fake_romm_api.roms = {
            1: {"id": 1, "name": "Super Mario Bros", "platform_id": 5},
            2: {"id": 2, "name": "Zelda", "platform_id": 5},
        }
        service = _make_service(fake_romm_api)
        result = await service.browse_roms(None, "mario", 30, 0)
        assert result["success"] is True
        assert result["total"] == 1
        assert result["items"][0]["name"] == "Super Mario Bros"

    @pytest.mark.asyncio
    async def test_empty_result_is_success(self, fake_romm_api):
        service = _make_service(fake_romm_api)
        result = await service.browse_roms(None, "nothing", 30, 0)
        assert result["success"] is True
        assert result["total"] == 0
        assert result["items"] == []

    @pytest.mark.asyncio
    async def test_server_unreachable_returns_canonical_failure(self, fake_romm_api):
        from lib.errors import RommConnectionError

        fake_romm_api.browse_roms_side_effect = RommConnectionError("network down")
        service = _make_service(fake_romm_api)
        result = await service.browse_roms(None, None, 30, 0)
        assert result["success"] is False
        assert result["error_code"] == "connection_error"
        assert "Server unreachable" in result["message"]

    @pytest.mark.asyncio
    async def test_invalid_response_shape_returns_api_error(self):
        bad_api = MagicMock()
        bad_api.browse_roms.return_value = "not a dict"
        service = _make_service(bad_api)
        result = await service.browse_roms(None, None, 30, 0)
        assert result["success"] is False
        assert result["error_code"] == "api_error"

    @pytest.mark.asyncio
    async def test_paginates_via_limit_offset(self, fake_romm_api):
        fake_romm_api.roms = {
            i: {"id": i, "name": f"Game {i}", "platform_id": 5} for i in range(1, 11)
        }
        service = _make_service(fake_romm_api)
        page1 = await service.browse_roms(None, None, 4, 0)
        page2 = await service.browse_roms(None, None, 4, 4)
        assert page1["total"] == 10
        assert len(page1["items"]) == 4
        assert len(page2["items"]) == 4
        assert page1["items"][0]["id"] != page2["items"][0]["id"]


class TestGetCoverBase64:
    @pytest.mark.asyncio
    async def test_returns_encoded_bytes_when_cover_available(self, fake_romm_api):
        fake_romm_api.roms = {
            42: {"id": 42, "name": "Zelda", "path_cover_small": "/covers/42-s.jpg"},
        }
        fake_romm_api.download_payloads = {"cover:/covers/42-s.jpg": b"\x89PNG\r\n"}
        service = _make_service(fake_romm_api)
        result = await service.get_cover_base64(42)
        assert result["success"] is True
        assert result["base64"] is not None
        # Round-trip the encoding to confirm the bytes survived
        import base64
        assert base64.b64decode(result["base64"]) == b"\x89PNG\r\n"

    @pytest.mark.asyncio
    async def test_returns_none_when_rom_has_no_cover_path(self, fake_romm_api):
        fake_romm_api.roms = {42: {"id": 42, "name": "No Art"}}
        service = _make_service(fake_romm_api)
        result = await service.get_cover_base64(42)
        assert result["success"] is True
        assert result["base64"] is None

    @pytest.mark.asyncio
    async def test_returns_none_when_download_returns_empty_bytes(self, fake_romm_api):
        fake_romm_api.roms = {42: {"id": 42, "path_cover_small": "/covers/42-s.jpg"}}
        fake_romm_api.download_payloads = {"cover:/covers/42-s.jpg": b""}
        service = _make_service(fake_romm_api)
        result = await service.get_cover_base64(42)
        assert result["success"] is True
        assert result["base64"] is None

    @pytest.mark.asyncio
    async def test_cache_hit_skips_second_fetch(self, fake_romm_api):
        fake_romm_api.roms = {42: {"id": 42, "path_cover_small": "/covers/42-s.jpg"}}
        fake_romm_api.download_payloads = {"cover:/covers/42-s.jpg": b"abc"}
        service = _make_service(fake_romm_api)
        first = await service.get_cover_base64(42)
        second = await service.get_cover_base64(42)
        assert first == second
        # Second call hits the LRU cache → no extra download
        cover_calls = [c for c in fake_romm_api.call_log if c[0] == "download_cover_bytes"]
        assert len(cover_calls) == 1

    @pytest.mark.asyncio
    async def test_server_unreachable_returns_failure_with_null_base64(self, fake_romm_api):
        from lib.errors import RommConnectionError

        fake_romm_api.get_rom_side_effect = RommConnectionError("offline")
        service = _make_service(fake_romm_api)
        result = await service.get_cover_base64(42)
        assert result["success"] is False
        assert result["error_code"] == "connection_error"
        assert result["base64"] is None
