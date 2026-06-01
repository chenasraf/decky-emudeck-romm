"""Browser-playground replacement for the ``decky`` module that lives
inside the Decky Loader process. Installed into ``sys.modules['decky']``
before ``main.py`` is imported so ``import decky`` works outside Decky.

Paths point at ``playground/dev-settings/`` so the user's real Decky
install at ``~/homebrew/...`` is never touched. ``emit()`` is rebound
by ``app.py`` to push events through the FastAPI WebSocket.
"""

from __future__ import annotations

import logging
import os
import sys
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[2]
_SETTINGS_DIR = _REPO_ROOT / "playground" / "dev-settings"
_RUNTIME_DIR = _SETTINGS_DIR / "runtime"
_LOG_DIR = _SETTINGS_DIR / "logs"

for _p in (_SETTINGS_DIR, _RUNTIME_DIR, _LOG_DIR):
    _p.mkdir(parents=True, exist_ok=True)

DECKY_PLUGIN_DIR = str(_REPO_ROOT)
DECKY_PLUGIN_SETTINGS_DIR = str(_SETTINGS_DIR)
DECKY_PLUGIN_RUNTIME_DIR = str(_RUNTIME_DIR)
DECKY_PLUGIN_LOG_DIR = str(_LOG_DIR)
DECKY_USER_HOME = os.path.expanduser("~")

# Skip the EmuDeck version-band gate so the playground runs on hosts without
# a real EmuDeck install. Never set on a Steam Deck — production should hit
# the strict compat check in py_modules/bootstrap.py.
os.environ.setdefault("DECKY_EMUDECK_ROMM_BYPASS_COMPAT", "1")

logger = logging.getLogger("decky")
logger.setLevel(logging.DEBUG)
if not logger.handlers:
    _h = logging.StreamHandler(sys.stderr)
    _h.setFormatter(logging.Formatter("[%(levelname)s] %(name)s: %(message)s"))
    logger.addHandler(_h)


async def emit(event_name, *args):  # noqa: D401
    """Placeholder — rebound by ``app.py`` to broadcast over the WebSocket."""
    logger.debug("emit(%r, %r) — WS not bound yet", event_name, args)
