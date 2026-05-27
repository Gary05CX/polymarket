"""
Market Scanner — finds candidate markets worth the (expensive) research step.

Extremely selective. Only high-liquidity, reasonably efficient markets with
enough volume and time to resolution for our research to matter.
"""
from __future__ import annotations

from ..config import get_settings
from ..logging import get_logger
from ..polymarket.client import PolymarketClient

logger = get_logger(__name__)


class MarketScanner:
    def __init__(self) -> None:
        self.settings = get_settings()

    async def find_candidates(self, client: PolymarketClient, limit: int = 8) -> list[dict]:
        """Return a small list of promising markets."""
        raw = await client.list_active_markets(limit=30, min_volume=8_000)

        candidates = []
        for m in raw:
            vol = float(m.get("volume_usd", 0))
            liq = float(m.get("liquidity", 0))
            if vol < 12_000 or liq < 3_000:
                continue
            if m.get("category") and m["category"].lower() in self.settings.market_category_blacklist:
                continue
            candidates.append(m)

        # Sort by some naive "interestingness" (volume + liquidity)
        candidates.sort(key=lambda x: float(x.get("volume_usd", 0)) + float(x.get("liquidity", 0)), reverse=True)
        return candidates[:limit]
