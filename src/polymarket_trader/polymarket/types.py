"""Domain types specific to Polymarket (normalized from SDK responses)."""
from __future__ import annotations

from dataclasses import dataclass
from typing import Any


@dataclass
class PolymarketMarket:
    id: str
    question: str
    clob_token_ids: list[str]
    tick_size: float = 0.01
    neg_risk: bool = False
    volume: float = 0.0
    liquidity: float = 0.0
