"""
Trade Executor — thin safe wrapper that calls the PolymarketClient.

All real execution is still gated by the RiskManager having already approved.
Paper mode is enforced at the client level.
"""
from __future__ import annotations

from ..logging import get_logger
from ..polymarket.client import get_client

logger = get_logger(__name__)


class TradeExecutor:
    async def execute_limit_order(
        self,
        *,
        token_id: str,
        side: str,
        price: float,
        size_usd: float,
        market_id: str | None = None,
    ) -> dict[str, any]:
        client = await get_client()
        try:
            result = await client.place_limit_order(
                token_id=token_id,
                side=side,
                price=price,
                size=size_usd,
                market_id=market_id,
            )
            logger.info("executor_order_result", result=result)
            return result
        finally:
            await client.close()
