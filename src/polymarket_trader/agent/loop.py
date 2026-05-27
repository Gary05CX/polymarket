"""
Main autonomous loop.

This is where everything comes together:
Scanner → Research → EdgeDetector → RiskManager → Executor → DB

The loop is deliberately defensive and will refuse to trade the vast majority of the time.
"""
from __future__ import annotations

import asyncio
from typing import Any

from ..config import get_settings
from ..logging import get_logger
from ..polymarket.client import get_client
from .edge_detector import EdgeDetector
from .research import ResearchEngine
from .risk_manager import RiskManager
from .scanner import MarketScanner

logger = get_logger(__name__)


class TradingAgentLoop:
    def __init__(self) -> None:
        self.settings = get_settings()
        self.scanner = MarketScanner()
        self.research = ResearchEngine()
        self.edge = EdgeDetector()
        self.risk = RiskManager()
        self._running = False

    async def run_once(self) -> dict[str, Any]:
        """Single iteration — safe to call from CLI or scheduler."""
        client = await get_client()
        try:
            candidates = await self.scanner.find_candidates(client)
            if not candidates:
                logger.info("no_candidates_this_cycle")
                return {"action": "no_candidates"}

            results = []
            for market in candidates[:3]:  # limit research spend
                research_text, sources = await self.research.gather_for_market(
                    market.get("question", ""), market.get("category")
                )
                # Build a minimal research summary object
                from ..data.models import ResearchSummary
                rs = ResearchSummary(key_facts=research_text, sources=sources)

                decision = await self.edge.evaluate(market, rs)
                approved, size, reason = self.risk.approve_trade(
                    edge=decision.edge,
                    confidence=decision.confidence,
                    recommended_size_usd=decision.recommended_size_usd,
                )

                # Persist decision (even if vetoed)
                self.risk.record_decision(decision, approved, size, reason)

                if approved:
                    # Executor would be called here (not yet implemented)
                    logger.info("trade_approved", size=size, reason=reason)
                    results.append({"market": market.get("question"), "approved": True, "size": size})
                else:
                    results.append({"market": market.get("question")[:50], "approved": False, "reason": reason[:80]})

            return {"action": "cycle_complete", "evaluated": len(results), "details": results}
        finally:
            await client.close()

    async def run_forever(self) -> None:
        self._running = True
        interval = self.settings.poll_interval_seconds
        logger.info("agent_loop_starting", interval=interval, paper=self.settings.paper_trading)

        while self._running:
            try:
                await self.run_once()
            except Exception as exc:
                logger.error("loop_iteration_error", error=str(exc)[:200])
            await asyncio.sleep(interval)

    def stop(self) -> None:
        self._running = False
