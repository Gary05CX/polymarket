"""
Market Scanner — finds candidate markets worth the (expensive) research step.

Extremely selective. Only high-liquidity, reasonably efficient markets with
enough volume and time to resolution for our research to matter.
"""
from __future__ import annotations

from ..config import get_settings
from ..data.repositories import MarketAssessmentRepo, MarketScoreRepo
from ..logging import get_logger
from ..polymarket.client import PolymarketClient

logger = get_logger(__name__)


class MarketScanner:
    def __init__(self) -> None:
        self.settings = get_settings()

        # Categories that are often easier to find edge in (lower efficiency or 0% fees)
        self.preferred_categories = {
            "weather", "geopolitics", "world", "economics", "science",
            "environment", "climate", "natural-disasters"
        }

    async def find_candidates(self, client: PolymarketClient, limit: int = 8) -> list[dict]:
        """Return a small list of promising markets.

        DB-First flow (user requirement):
        1. Fetch raw batch → immediately persist to market_snapshots + quick scores (memory for future runs).
        2. Apply prior news_score skips (the key waste-reduction mechanism).
        3. Mix in historically good but currently low-volume markets for real diversity.
        4. Then normal hard filters + cooldown + long-term ignore.
        """
        raw = await client.list_active_markets(limit=50, min_volume=5_000)

        # === USER REQUIREMENT: 抓完後先放進 database ===
        try:
            MarketScoreRepo.record_batch_snapshots(raw)
        except Exception as exc:
            logger.warning("batch_persist_failed_nonfatal", error=str(exc)[:80])

        candidates: list[dict] = []
        blacklist = {c.lower().strip() for c in self.settings.market_category_blacklist}

        # Markets we should skip entirely this cycle (cheap DB check, huge token saver)
        recent_ids = MarketAssessmentRepo.get_recently_assessed_ids(self.settings.min_reevaluation_hours)
        ignored_ids = MarketAssessmentRepo.get_frequently_ignored_market_ids(
            self.settings.max_assessments_before_ignore
        )
        # New: prior low news scores → hard skip (no research, no LLM)
        low_news_skip_ids = MarketScoreRepo.get_markets_to_skip(threshold=0.22, hours=36)

        # Pull a few historically decent markets that may have dropped out of top-volume (diversity engine)
        history_diverse = MarketScoreRepo.get_diverse_high_potential_from_history(
            preferred_categories=self.preferred_categories,
            min_score=0.28,
            hours=72,
            limit=5,
        )
        history_by_id = {h["market_id"]: h for h in history_diverse}

        for m in raw:
            market_id = str(m.get("id") or m.get("slug") or m.get("_market_id") or "")
            vol = float(m.get("volume_usd", 0))
            liq = float(m.get("liquidity", 0))
            category = (m.get("category") or "").lower().strip()
            question_lower = m.get("question", "").lower()

            if vol < 6_000 or liq < 1_500:
                continue

            if category and category in blacklist:
                continue

            if market_id in recent_ids:
                continue

            if market_id in ignored_ids:
                continue

            if market_id in low_news_skip_ids:
                # The big win: we already decided this market had no actionable signal last time(s)
                logger.info("skipped_prior_low_news_score", market=market_id[:30])
                continue

            # Transitional: still de-prioritize noisy FIFA family (will be replaced by event_family scores)
            if "fifa world cup" in question_lower or "world cup 2026" in question_lower:
                m["_score"] = m.get("_score", 0) * 0.15   # even harsher now that we have DB memory

            # Base score
            score = vol + liq * 0.4
            if category in self.preferred_categories:
                score *= 1.8

            # Boost if this market had good prior news signal (even if volume dipped)
            if market_id in history_by_id:
                hist = history_by_id[market_id]
                score *= 1.4 + min(hist.get("best_prior_score", 0.3) * 0.6, 0.5)
                m["_from_history"] = True

            m["_score"] = score
            m["_market_id"] = market_id
            candidates.append(m)

        # Sort
        candidates.sort(key=lambda x: x.get("_score", 0), reverse=True)

        # Light diversity + inject a couple from history if they didn't make the volume cut
        final: list[dict] = []
        seen_cats: set[str] = set()
        for m in candidates:
            cat = (m.get("category") or "unknown").lower()
            if cat not in seen_cats or len(final) < limit // 2:
                final.append(m)
                seen_cats.add(cat)
            if len(final) >= limit:
                break

        # If we are still low on diversity, inject 1-2 high-potential history markets
        if len(final) < max(3, limit // 2) and history_diverse:
            for h in history_diverse[:2]:
                if h["market_id"] not in {f.get("_market_id") for f in final}:
                    # fabricate a minimal dict the rest of the pipeline can consume
                    final.append({
                        "id": h["market_id"],
                        "question": f"[History] {h['market_id']}",
                        "_market_id": h["market_id"],
                        "category": h.get("category"),
                        "_score": 8000 * h.get("best_prior_score", 0.4),
                        "_from_history": True,
                        "volume_usd": 7000,
                        "liquidity": 2000,
                    })
                    if len(final) >= limit:
                        break

        logger.info(
            "scanner_candidates_built",
            total=len(final),
            from_history=sum(1 for f in final if f.get("_from_history")),
            prior_skips_applied=len(low_news_skip_ids),
        )
        return final[:limit]
