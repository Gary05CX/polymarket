"""
Thin, safe wrapper around the official Polymarket unified SDK (polymarket-client).

Responsibilities:
- Lazy initialization of PublicClient and (when needed) SecureClient
- Dry-run / paper trading mode that never touches real funds
- Basic rate limiting and retry helpers
- Consistent error handling and logging

Reference: https://github.com/Polymarket/py-sdk
"""
from __future__ import annotations

import asyncio
from typing import Any

from ..config import get_settings
from ..logging import get_logger

logger = get_logger(__name__)


class PolymarketClient:
    """
    Unified client for both public market data and authenticated trading.

    In paper_trading mode (default), all trading methods become no-ops that
    return realistic fake responses so the rest of the agent can be tested safely.
    """

    def __init__(self, paper_trading: bool | None = None) -> None:
        settings = get_settings()
        self.paper_trading = paper_trading if paper_trading is not None else settings.is_paper_trading
        self._public_client: Any = None
        self._secure_client: Any = None
        self._rate_limiter = asyncio.Semaphore(8)  # crude but effective

    async def _get_public(self) -> Any:
        if self._public_client is None:
            try:
                from polymarket import PublicClient  # type: ignore

                self._public_client = PublicClient()
            except Exception as exc:  # pragma: no cover
                logger.error("failed_to_import_polymarket_public", error=str(exc))
                raise
        return self._public_client

    async def _get_secure(self) -> Any:
        if self._secure_client is None:
            if self.paper_trading:
                # In paper mode we never actually create the authenticated client
                return None
            settings = get_settings()
            try:
                from polymarket import AsyncSecureClient  # type: ignore

                self._secure_client = await AsyncSecureClient.create(
                    private_key=settings.polymarket_private_key,
                    wallet=settings.polymarket_deposit_wallet_address,
                )
            except Exception as exc:
                logger.error("failed_to_create_secure_client", error=str(exc))
                raise
        return self._secure_client

    # ------------------------------------------------------------------ #
    # Public market data (always safe)
    # ------------------------------------------------------------------ #

    async def list_active_markets(
        self, limit: int = 50, min_volume: float = 5_000
    ) -> list[dict[str, Any]]:
        """Fetch active markets via Gamma (public)."""
        async with self._rate_limiter:
            client = await self._get_public()
            # The exact API shape depends on the current beta SDK version.
            # This is intentionally defensive.
            try:
                # New SDK returns a Paginator (synchronous), not an awaitable.
                paginator = client.list_markets(
                    closed=False,
                    page_size=min(limit, 100),
                )

                result = []
                for m in paginator.items():
                    metrics = getattr(m, "metrics", None)
                    vol = float(getattr(metrics, "volume_num", 0) or 0) if metrics else 0
                    liq = float(getattr(metrics, "liquidity_num", 0) or 0) if metrics else 0

                    if vol >= min_volume:
                        outcomes = getattr(m, "outcomes", None) or []
                        implied = 0.5
                        price_source = "fallback"
                        try:
                            # Common Gamma shape: outcomes is list of objects/dicts with "price" for Yes (first or by name)
                            if isinstance(outcomes, (list, tuple)) and outcomes:
                                first = outcomes[0]
                                p = None
                                if isinstance(first, dict):
                                    p = first.get("price") or first.get("current_price")
                                else:
                                    p = getattr(first, "price", None) or getattr(first, "current_price", None)
                                if p is not None:
                                    implied = float(p)
                                    price_source = "outcomes_yes"
                            # Fallback: sometimes the market itself carries a price or last_trade_price
                            if implied == 0.5:
                                for attr in ("price", "last_trade_price", "implied_prob", "yes_price"):
                                    val = getattr(m, attr, None)
                                    if val is not None:
                                        implied = float(val)
                                        price_source = f"market_{attr}"
                                        break
                        except Exception:
                            implied = 0.5
                            price_source = "fallback_error"

                        result.append(
                            {
                                "id": getattr(m, "id", None) or getattr(m, "condition_id", None),
                                "question": getattr(m, "question", ""),
                                "slug": getattr(m, "slug", ""),
                                "volume_usd": vol,
                                "liquidity": liq,
                                "category": getattr(m, "category", None),
                                "end_date": getattr(m, "end_date", None),
                                "clob_token_ids": getattr(m, "clob_token_ids", []),
                                "outcomes": outcomes,
                                "implied_prob": round(implied, 4),
                                "price_source": price_source,
                            }
                        )

                    if len(result) >= limit:
                        break

                return result
            except Exception as exc:
                logger.warning("market_list_fallback", error=str(exc))
                # Return empty on any issue — the agent must be resilient
                return []

    async def get_market(self, market_id: str) -> dict[str, Any] | None:
        """Fetch a single market (public)."""
        async with self._rate_limiter:
            client = await self._get_public()
            try:
                m = await client.get_market(id=market_id)
                outcomes = getattr(m, "outcomes", None) or []
                implied = 0.5
                price_source = "fallback"
                try:
                    if isinstance(outcomes, (list, tuple)) and outcomes:
                        first = outcomes[0]
                        p = None
                        if isinstance(first, dict):
                            p = first.get("price") or first.get("current_price")
                        else:
                            p = getattr(first, "price", None) or getattr(first, "current_price", None)
                        if p is not None:
                            implied = float(p)
                            price_source = "outcomes_yes"
                except Exception:
                    pass
                return {
                    "id": getattr(m, "id", market_id),
                    "question": getattr(m, "question", ""),
                    "clob_token_ids": getattr(m, "clob_token_ids", []),
                    "minimum_tick_size": getattr(m, "minimum_tick_size", 0.01),
                    "neg_risk": getattr(m, "neg_risk", False),
                    "implied_prob": round(implied, 4),
                    "price_source": price_source,
                    "outcomes": outcomes,
                }
            except Exception:
                return None

    # ------------------------------------------------------------------ #
    # Trading (the dangerous part — fully guarded by paper mode)
    # ------------------------------------------------------------------ #

    async def place_limit_order(
        self,
        *,
        token_id: str,
        side: str,  # "BUY" or "SELL"
        price: float,
        size: float,  # notional in USDC or shares (SDK handles)
        market_id: str | None = None,
    ) -> dict[str, Any]:
        """
        Place a limit order.

        When paper_trading=True this method **never** calls the real SDK.
        It returns a fake successful response so the agent logic can be exercised.
        """
        if self.paper_trading:
            fake_order_id = f"paper_{token_id[:8]}_{int(price * 10000)}"
            logger.info(
                "paper_place_limit_order",
                token_id=token_id[:12],
                side=side,
                price=price,
                size=size,
            )
            return {
                "order_id": fake_order_id,
                "status": "OPEN",
                "paper": True,
                "message": "Paper order accepted (no real funds at risk)",
            }

        # === REAL TRADING PATH (only reached when paper_trading=False) ===
        secure = await self._get_secure()
        if secure is None:
            raise RuntimeError("Secure client not available outside paper mode")

        async with self._rate_limiter:
            try:
                # The exact call depends on the current beta SDK shape.
                # We keep it defensive and log heavily.
                resp = await secure.place_limit_order(
                    token_id=token_id,
                    side=side.upper(),
                    price=str(price),
                    size=str(size),
                )
                logger.info("real_order_placed", order_id=getattr(resp, "order_id", None))
                return {
                    "order_id": getattr(resp, "order_id", None),
                    "status": getattr(resp, "status", "SUBMITTED"),
                    "paper": False,
                }
            except Exception as exc:
                logger.error("real_order_failed", error=str(exc))
                raise

    async def cancel_order(self, order_id: str) -> bool:
        if self.paper_trading:
            logger.info("paper_cancel_order", order_id=order_id)
            return True

        secure = await self._get_secure()
        if secure is None:
            return False
        # real cancel logic...
        return True

    async def get_open_orders(self) -> list[dict[str, Any]]:
        if self.paper_trading:
            return []
        # real implementation later
        return []

    async def get_positions(self) -> list[dict[str, Any]]:
        """Return current positions. Paper mode returns empty (positions tracked in our DB)."""
        if self.paper_trading:
            return []
        secure = await self._get_secure()
        if secure is None:
            return []
        # TODO: map real positions once SDK shape is stable
        return []

    async def close(self) -> None:
        """Graceful shutdown for any open connections."""
        if self._secure_client is not None:
            try:
                await self._secure_client.close()
            except Exception:
                pass
        logger.info("polymarket_client_closed", paper=self.paper_trading)


# Convenience factory
async def get_client(paper_trading: bool | None = None) -> PolymarketClient:
    return PolymarketClient(paper_trading=paper_trading)
