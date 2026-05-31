"""Selection chain for ``bootstrap._select_frontend``.

Table-driven across ``(setting x emudeck_detect x retrodeck_detect) ->
expected adapter``. The selection rule itself is short enough to read
in source, but the cross-product is large enough that explicit cases
catch silent regressions when the autodetect order or fallback target
changes.
"""

from __future__ import annotations

import logging

import pytest
from bootstrap import _select_frontend

from adapters.frontends.emudeck import EmuDeckFrontendAdapter
from adapters.frontends.retrodeck import RetroDeckFrontendAdapter


def _logger() -> logging.Logger:
    return logging.getLogger("test_frontend_selection")


def _stub_detect(monkeypatch, *, emudeck: bool, retrodeck: bool) -> None:
    """Force both adapters' ``detect()`` to fixed values regardless of host."""
    monkeypatch.setattr(EmuDeckFrontendAdapter, "detect", lambda self: emudeck)
    monkeypatch.setattr(RetroDeckFrontendAdapter, "detect", lambda self: retrodeck)


class TestExplicitSelection:
    """``"emudeck"`` and ``"retrodeck"`` skip the autodetect chain."""

    def test_emudeck_setting_returns_emudeck_even_when_only_retrodeck_detected(
        self, monkeypatch, tmp_path
    ):
        _stub_detect(monkeypatch, emudeck=False, retrodeck=True)
        result = _select_frontend(setting="emudeck", user_home=str(tmp_path), logger=_logger())
        assert isinstance(result, EmuDeckFrontendAdapter)

    def test_retrodeck_setting_returns_retrodeck_even_when_only_emudeck_detected(
        self, monkeypatch, tmp_path
    ):
        _stub_detect(monkeypatch, emudeck=True, retrodeck=False)
        result = _select_frontend(setting="retrodeck", user_home=str(tmp_path), logger=_logger())
        assert isinstance(result, RetroDeckFrontendAdapter)


@pytest.mark.parametrize("setting", ["auto", "custom", "unknown-value"])
class TestAutodetectChain:
    """Implicit / unknown / custom settings fall through to autodetect."""

    def test_emudeck_wins_when_both_detect(self, setting, monkeypatch, tmp_path):
        _stub_detect(monkeypatch, emudeck=True, retrodeck=True)
        result = _select_frontend(setting=setting, user_home=str(tmp_path), logger=_logger())
        assert isinstance(result, EmuDeckFrontendAdapter)

    def test_emudeck_wins_when_only_emudeck_detects(self, setting, monkeypatch, tmp_path):
        _stub_detect(monkeypatch, emudeck=True, retrodeck=False)
        result = _select_frontend(setting=setting, user_home=str(tmp_path), logger=_logger())
        assert isinstance(result, EmuDeckFrontendAdapter)

    def test_retrodeck_chosen_when_only_retrodeck_detects(self, setting, monkeypatch, tmp_path):
        _stub_detect(monkeypatch, emudeck=False, retrodeck=True)
        result = _select_frontend(setting=setting, user_home=str(tmp_path), logger=_logger())
        assert isinstance(result, RetroDeckFrontendAdapter)

    def test_retrodeck_fallback_when_nothing_detects(self, setting, monkeypatch, tmp_path):
        _stub_detect(monkeypatch, emudeck=False, retrodeck=False)
        result = _select_frontend(setting=setting, user_home=str(tmp_path), logger=_logger())
        assert isinstance(result, RetroDeckFrontendAdapter)


class TestCustomSettingLogsWarning:
    """``"custom"`` falls through to autodetect but warns the user."""

    def test_custom_logs_warning(self, monkeypatch, tmp_path, caplog):
        _stub_detect(monkeypatch, emudeck=False, retrodeck=True)
        with caplog.at_level(logging.WARNING):
            _select_frontend(setting="custom", user_home=str(tmp_path), logger=_logger())
        assert any("Custom frontend path overrides" in r.message for r in caplog.records)

    def test_auto_does_not_warn(self, monkeypatch, tmp_path, caplog):
        _stub_detect(monkeypatch, emudeck=False, retrodeck=True)
        with caplog.at_level(logging.WARNING):
            _select_frontend(setting="auto", user_home=str(tmp_path), logger=_logger())
        assert not any("Custom frontend path overrides" in r.message for r in caplog.records)


class TestBootstrapThreadsSelectionThroughSetting:
    """End-to-end: bootstrap reads ``settings["frontend"]`` and selects accordingly."""

    def test_bootstrap_reads_frontend_setting_from_persistence(self, tmp_path, monkeypatch):
        # Seed persisted settings with frontend="emudeck" so bootstrap
        # ends up holding an EmuDeck adapter, not the autodetect default.
        import json

        from bootstrap import bootstrap

        settings_dir = tmp_path / "settings"
        settings_dir.mkdir()
        (settings_dir / "settings.json").write_text(
            json.dumps({"frontend": "emudeck", "version": 1})
        )
        # EmuDeck adapter probes versions.json on construction; stub
        # compatible() to True so _enforce_frontend_compatibility lets
        # bootstrap complete (the explicit setting bypasses detect()
        # but compatible() still runs).
        monkeypatch.setattr(EmuDeckFrontendAdapter, "compatible", lambda self: True)

        result = bootstrap(
            settings_dir=str(settings_dir),
            runtime_dir=str(tmp_path / "runtime"),
            plugin_dir=str(tmp_path / "plugin"),
            user_home=str(tmp_path / "home"),
            logger=_logger(),
        )
        assert isinstance(result.callbacks.frontend, EmuDeckFrontendAdapter)
