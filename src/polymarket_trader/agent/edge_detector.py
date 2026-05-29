"""
Edge Detector — decides whether a real statistical edge exists.

This is where the "AI" makes its probability call and compares it to the market.
Heavily biased toward PASS. Only recommends action on high-conviction, well-researched edges.
"""
from __future__ import annotations

from ..config import get_settings
from ..data.models import EdgeDecision, ResearchSummary
from ..llm.provider import get_llm_provider
from ..logging import get_logger

logger = get_logger(__name__)


class EdgeDetector:
    def __init__(self) -> None:
        self.settings = get_settings()
        self.llm = get_llm_provider()

    async def evaluate(
        self,
        market: dict[str, any],
        research: ResearchSummary,
    ) -> EdgeDecision:
        """
        Core method. Returns a conservative EdgeDecision.
        """
        question = market.get("question", "")
        market_price = float(market.get("implied_prob", market.get("price", 0.5)))
        volume = float(market.get("volume_usd", 0))
        liquidity = float(market.get("liquidity", 0))
        category = market.get("category", "unknown")

        # Cheap research synthesis already happened in ResearchEngine
        research_text = research.key_facts or "No strong external signal."

        try:
            judgment, usage = await self.llm.judge_probability(
                question=question,
                market_price=market_price,
                research_summary=research_text,
                category=category,
                volume_usd=volume,
                liquidity=liquidity,
            )
        except Exception as exc:
            logger.warning("llm_judge_failed_fallback_pass", error=str(exc))
            return EdgeDecision(
                market_id=str(market.get("id")),
                market_price=market_price,
                llm_prob=None,
                edge=None,
                confidence=0.1,
                recommended_side="PASS",
                recommended_size_usd=None,
                rationale="LLM call failed — defaulting to PASS for capital safety.",
                sources=research.sources,
            )

        true_p = float(judgment.get("true_probability_yes", 0.5))
        confidence = float(judgment.get("confidence", 0.3))
        raw_edge = true_p - market_price
        action = judgment.get("recommended_action", "PASS")
        rationale = judgment.get("rationale", "")[:280]

        # Apply our own hard filters on top of LLM opinion
        min_edge = self.settings.min_edge_percent / 100.0
        final_action = "PASS"
        size = None
        veto = None
        is_micro = False

        research_weak = (judgment.get("research_quality", "medium") or "").lower() in ("low", "medium") or \
                        (research.key_facts or "").startswith("No strong external signal") or \
                        not research.sources

        if abs(raw_edge) < min_edge:
            # MICRO LANE: allow tiny exploratory position on weak research when edge is still directionally interesting
            if research_weak and abs(raw_edge) >= 0.055 and confidence >= 0.42 and volume >= 4000:
                final_action = "BUY" if raw_edge > 0 else "SELL"
                is_micro = True
                rationale = (rationale or "") + " | MICRO: weak research, exploratory $0.10-0.30 only (data gathering)"
            else:
                veto = f"Edge {raw_edge:.2%} below minimum threshold {min_edge:.2%}"
        elif confidence < 0.55:
            if research_weak and abs(raw_edge) >= 0.05 and confidence >= 0.40 and volume >= 3500:
                final_action = "BUY" if raw_edge > 0 else "SELL"
                is_micro = True
                rationale = (rationale or "") + " | MICRO: low-conf but non-zero signal, micro only"
            else:
                veto = f"Low LLM confidence ({confidence:.0%})"
        elif action != "PASS" and volume < 8000:
            veto = "Insufficient volume for reliable edge"
        else:
            final_action = "BUY" if raw_edge > 0 else "SELL"
            # Size will be decided by RiskManager (never here)
            size = None  # RiskManager owns sizing

        decision = EdgeDecision(
            market_id=str(market.get("id")),
            market_price=market_price,
            llm_prob=true_p,
            edge=raw_edge,
            confidence=confidence,
            recommended_side=final_action,
            recommended_size_usd=size,
            rationale=rationale if not veto else f"{rationale} | VETO: {veto}",
            sources=research.sources,
            llm_model=usage.get("model"),
            is_micro_position=is_micro,
        )

        logger.info(
            "edge_evaluated",
            market=question[:60],
            edge=raw_edge,
            action=final_action,
            confidence=confidence,
        )
        return decision
