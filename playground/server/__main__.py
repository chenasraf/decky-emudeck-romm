"""Run the playground bridge: ``python -m playground.server``."""

from __future__ import annotations

import uvicorn

if __name__ == "__main__":
    uvicorn.run(
        "playground.server.app:app",
        host="127.0.0.1",
        port=5175,
        reload=False,
        log_level="info",
    )
