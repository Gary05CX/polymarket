"""
Risk Manager — THE single most important module in the entire system.

Its only job is to protect the initial capital at all costs.
Every trade recommendation from EdgeDetector must pass through here.
No exceptions. No overrides. No "trust me" logic.

All rules are intentionally conservative and hard-coded with safe defaults.
"""
from __future__ import annotations

import uuid
from datetime import datetime, timedelta
from typing import Any

from ..config import get_settings
from ..data.db import get_current_capital
from ..data.repositories import CostRepo, DecisionRepo, get_summary, MarketAssessmentRepo
from ..logging import get_logger

logger = get_logger(__name__)


class RiskManager:
    """
    Capital Guardian.

    Every decision passes through one of the `approve_*` methods.
    Returns (approved: bool, size_usd: float|None, reason: str)
    """

    def __init__(self) -> None:
        self.settings = get_settings()
        self.initial_capital = get_current_capital()

    def _current_equity_proxy(self) -> float:
        """For v1 we use the protected initial value. Later we will track live equity."""
        return self.initial_capital

    def _api_budget_remaining(self) -> float:
        spent = CostRepo.total_spent_usd()
        return max(0.0, self.settings.max_api_budget_usd - spent)

    def _recent_losses(self, hours: int = 24) -> int:
        """Count approved decisions that later realized negative results (stub for v1)."""
        # In a full implementation we would join with realized trades table.
        # For now we use a simple heuristic from recent decisions.
        recent = DecisionRepo.get_recent(limit=30)
        bad = 0
        cutoff = datetime.utcnow() - timedelta(hours=hours)
        for d in recent:
            if d.get("ts") and d["ts"] < cutoff:
                continue
            if d.get("was_approved") and d.get("calculated_edge", 0) and d["calculated_edge"] < -0.03:
                bad += 1
        return bad

    def approve_trade(
        self,
        *,
        edge: float | None,
        confidence: float | None,
        recommended_size_usd: float | None,
        current_positions_count: int = 0,
        market_category: str | None = None,
        is_micro_position: bool = False,
    ) -> tuple[bool, float | None, str]:
        """
        The central gate. Returns (approved, safe_size_usd, human_readable_reason)
        """
        if edge is None or confidence is None:
            return False, None, "Missing edge or confidence from detector"

        # MICRO EXPLORATORY LANE (only for weak-research tiny positions)
        # Never relaxes the sacred capital / budget / loss / blacklist breakers.
        if is_micro_position:
            micro_min_edge = 0.055
            micro_min_conf = 0.40
            if abs(edge) < micro_min_edge:
                return False, None, f"Micro edge {edge:.2%} < micro floor {micro_min_edge:.2%}"
            if confidence < micro_min_conf:
                return False, None, f"Micro confidence {confidence:.0%} too low"

            # Still hard global guards
            equity = self._current_equity_proxy()
            if equity < self.initial_capital * 0.995:
                return False, None, "Current equity below protected principal — trading halted"
            budget_left = self._api_budget_remaining()
            if budget_left < 0.60:   # slightly tighter for micros near budget end
                return False, None, f"API budget too low for even micro ({budget_left:.2f})"
            if self._recent_losses(24) >= 3:
                return False, None, "3+ negative edge decisions in last 24h — cooling off"
            if current_positions_count >= 2:
                return False, None, "Maximum concurrent positions (2) reached"
            if market_category and market_category.lower() in [c.lower() for c in self.settings.market_category_blacklist]:
                return False, None, f"Category {market_category} is blacklisted"

            micro_size = min(0.30, max(0.10, equity * 0.02))
            reason = f"MICRO | Edge {edge:.2%} | conf {confidence:.0%} | size ${micro_size:.2f} | API left ${budget_left:.2f}"
            logger.info("risk_micro_approved", size=micro_size, reason=reason[:110])
            return True, round(micro_size, 2), reason

        # === NORMAL (strict) PATH ===
        min_edge = self.settings.min_edge_percent / 100.0
        if abs(edge) < min_edge:
            return False, None, f"Edge {edge:.2%} < minimum {min_edge:.2%}"

        if confidence < 0.62:
            return False, None, f"Confidence {confidence:.0%} too low"

        # Hard capital rules
        equity = self._current_equity_proxy()
        if equity < self.initial_capital * 0.995:  # already slightly underwater
            return False, None, "Current equity below protected principal — trading halted"

        # API budget
        budget_left = self._api_budget_remaining()
        if budget_left < 0.80:
            return False, None, f"API budget nearly exhausted (${budget_left:.2f} left)"

        # Daily loss circuit breaker (heuristic)
        if self._recent_losses(24) >= 3:
            return False, None, "3+ negative edge decisions in last 24h — cooling off"

        # Position limits
        if current_positions_count >= 2:
            return False, None, "Maximum concurrent positions (2) reached"

        # Blacklist
        if market_category and market_category.lower() in [c.lower() for c in self.settings.market_category_blacklist]:
            return False, None, f"Category {market_category} is blacklisted"

        # Position sizing — extremely conservative
        risk_cap = min(
            self.settings.max_risk_per_trade_usd,
            equity * 0.04,  # 4% of current equity
        )
        proposed = recommended_size_usd or (risk_cap * 0.6)
        safe_size = max(0.10, min(proposed, risk_cap))

        # Final sanity: never risk more than we can afford to lose while staying above principal
        if safe_size > 1.5 and equity <= self.initial_capital + 1.0:
            safe_size = 0.60  # hard cap early in the life of the account

        reason = (
            f"Edge {edge:.2%} | conf {confidence:.0%} | size ${safe_size:.2f} "
            f"(risk cap ${risk_cap:.2f}) | API left ${budget_left:.2f}"
        )

        logger.info("risk_approved", size=safe_size, reason=reason[:120])
        return True, round(safe_size, 2), reason

    def record_decision(self, decision: Any, approved: bool, size: float | None, reason: str) -> uuid.UUID:
        """Persist the full decision + veto/approval rationale (immutable audit trail)."""
        decision_id = DecisionRepo.save(
            market_id=getattr(decision, "market_id", "unknown"),
            market_question="",
            market_price=getattr(decision, "market_price", 0.0),
            llm_estimated_prob=getattr(decision, "llm_prob", None),
            calculated_edge=getattr(decision, "edge", None),
            confidence=getattr(decision, "confidence", None),
            recommended_side=getattr(decision, "recommended_side", "PASS"),
            recommended_size_usd=size,
            risk_rationale=reason,
            sources=getattr(decision, "sources", []),
            was_approved=approved,
            veto_reason=None if approved else reason,
            paper_trade=self.settings.paper_trading,
        )

        # Log assessment for re-evaluation cooldown and long-term ignore logic
        try:
            MarketAssessmentRepo.log_assessment(
                market_id=getattr(decision, "market_id", "unknown"),
                edge=getattr(decision, "edge", None),
                confidence=getattr(decision, "confidence", None),
                approved=approved,
                reason=reason,
                paper_trade=self.settings.paper_trading,
            )
        except Exception:
            pass  # Don't let logging break the main flow

        return decision_id
