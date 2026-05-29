"""
Main autonomous loop.

This is where everything comes together:
Scanner → Research → EdgeDetector → RiskManager → Executor → DB

The loop is deliberately defensive and will refuse to trade the vast majority of the time.
"""
from __future__ import annotations

import asyncio
from typing import Any

from pathlib import Path

from ..config import get_settings
from ..data.repositories import CostRepo, MarketScoreRepo
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
            llm_calls_this_cycle = 0
            max_llm = self.settings.max_llm_calls_per_cycle

            for market in candidates[:3]:  # hard cap on research spend
                market_id = str(market.get("id") or market.get("slug") or market.get("_market_id") or "")
                q_short = market.get("question", "")[:55]

                # === MAJOR TOKEN SAVER: skip before any research/LLM if we already know from DB it's low value ===
                prior = MarketScoreRepo.get_recent_news_score(market_id, within_hours=36)
                if prior is not None and prior < 0.22:
                    logger.info("skipped_by_prior_low_news_score", market=q_short, prior_score=round(prior, 3))
                    # Still record a lightweight assessment so cooldown/ignore logic sees it
                    try:
                        from .risk_manager import RiskManager  # local to avoid circular at import time
                        # lightweight path - we won't call full record_decision here
                        MarketScoreRepo.log_news_score(
                            market_id=market_id,
                            news_score=prior,
                            notes="skipped_prior",
                            category=market.get("category"),
                        )
                    except Exception:
                        pass
                    results.append({"market": q_short, "approved": False, "reason": f"skipped (prior news_score {prior:.2f})"})
                    continue

                # Research (may still do cheap RSS + fast synthesize)
                research_text, sources = await self.research.gather_for_market(
                    market.get("question", ""), market.get("category")
                )

                # === SECOND BIG SAVER: zero external signal → do not call the expensive reasoning model at all ===
                if research_text.startswith("No recent external signals"):
                    logger.info("skipped_zero_signal_research", market=q_short)
                    MarketScoreRepo.log_news_score(
                        market_id=market_id,
                        news_score=0.12,
                        has_fresh_signal=False,
                        key_signal="no_rss_or_news_hits",
                        notes="zero_signal_bypass",
                        category=market.get("category"),
                        volume_snapshot=float(market.get("volume_usd", 0)),
                        liquidity_snapshot=float(market.get("liquidity", 0)),
                    )
                    results.append({"market": q_short, "approved": False, "reason": "no external signal (cheap skip)"})
                    continue

                # Build research summary
                from ..data.models import ResearchSummary
                rs = ResearchSummary(key_facts=research_text, sources=sources)

                # Only now do we pay for the reasoning model
                if llm_calls_this_cycle >= max_llm:
                    logger.warning("max_llm_calls_reached", count=llm_calls_this_cycle)
                    break

                decision = await self.edge.evaluate(market, rs)
                llm_calls_this_cycle += 1   # rough accounting (research may have used 0-1 fast calls too)

                approved, size, reason = self.risk.approve_trade(
                    edge=decision.edge,
                    confidence=decision.confidence,
                    recommended_size_usd=decision.recommended_size_usd,
                    is_micro_position=getattr(decision, "is_micro_position", False),
                )

                # Record only on usable LLM output (existing waste-reduction guard)
                if decision.llm_prob is not None or decision.edge is not None:
                    self.risk.record_decision(decision, approved, size, reason)
                    # Also update market intelligence with what we learned
                    try:
                        research_quality = "medium"
                        if "weak" in (decision.rationale or "").lower() or not sources:
                            research_quality = "low"
                        final_score = 0.55 if approved else (0.28 if decision.edge and abs(decision.edge) > 0.04 else 0.18)
                        MarketScoreRepo.log_news_score(
                            market_id=market_id,
                            news_score=round(final_score, 3),
                            has_fresh_signal=bool(sources),
                            key_signal=(decision.rationale or "")[:200],
                            notes="deep_eval" + ("_approved" if approved else "_veto"),
                            category=market.get("category"),
                            volume_snapshot=float(market.get("volume_usd", 0)),
                            liquidity_snapshot=float(market.get("liquidity", 0)),
                            llm_model=getattr(decision, "llm_model", None),
                        )
                    except Exception:
                        pass
                else:
                    logger.info("skipped_recording_failed_llm", market=q_short)
                    # Still write a low score so we don't hammer the same broken market next cycle
                    MarketScoreRepo.log_news_score(
                        market_id=market_id,
                        news_score=0.15,
                        notes="llm_failure",
                        category=market.get("category"),
                    )

                if approved:
                    logger.info("trade_approved", size=size, reason=reason)
                    results.append({"market": market.get("question"), "approved": True, "size": size})
                else:
                    if decision.llm_prob is None and decision.edge is None:
                        reason = "LLM failed to produce usable output (skipped recording)"
                    results.append({"market": q_short, "approved": False, "reason": reason[:80]})

            # 檢查 API 預算是否已耗盡，如果是則建立 flag 檔案，方便外部排程器偵測並停止任務
            self._check_and_create_budget_exhausted_flag()

            return {"action": "cycle_complete", "evaluated": len(results), "details": results}
        finally:
            await client.close()

    def _check_and_create_budget_exhausted_flag(self) -> None:
        """如果 API 預算已耗盡，建立一個 flag 檔案供外部排程器使用。"""
        settings = get_settings()
        spent = CostRepo.total_spent_usd()
        if spent >= settings.max_api_budget_usd * 0.98:  # 達到 98% 就視為即將耗盡
            flag_path = Path("data/budget_exhausted.flag")
            flag_path.parent.mkdir(parents=True, exist_ok=True)
            flag_path.write_text(
                f"API budget exhausted at {spent:.2f} USD (limit: {settings.max_api_budget_usd} USD)\n"
                f"Timestamp: {__import__('datetime').datetime.now().isoformat()}\n",
                encoding="utf-8"
            )
            logger.warning("api_budget_exhausted_flag_created", spent=spent, limit=settings.max_api_budget_usd)

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
