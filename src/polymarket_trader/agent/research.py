"""
Research Engine — the "external information" part of the agent.

Design goals (budget protection):
- RSS and completely free sources first (unlimited)
- Then cheap structured news APIs (TheNewsAPI, GNews, etc.)
- Only then (rarely) expensive search
- Always compress via the cheapest LLM before feeding the expensive reasoning model
"""
from __future__ import annotations

import asyncio
import os
from datetime import datetime, timedelta
from typing import Any

import feedparser
import httpx

from ..config import get_settings
from ..llm.provider import get_llm_provider
from ..logging import get_logger

logger = get_logger(__name__)


class ResearchEngine:
    def __init__(self) -> None:
        self.settings = get_settings()
        self.llm = get_llm_provider()
        self.client = httpx.AsyncClient(timeout=12.0, follow_redirects=True)

    async def gather_for_market(
        self, question: str, category: str | None = None, hours: int = 72
    ) -> tuple[str, list[dict[str, Any]]]:
        """
        Return (synthesized_text, sources_list) ready to be fed into the probability judge.
        """
        sources: list[dict[str, Any]] = []
        raw_texts: list[str] = []

        # 1. RSS (completely free, high signal for politics/crypto)
        rss_items = await self._fetch_relevant_rss(question, hours)
        for item in rss_items[:5]:
            raw_texts.append(f"{item['title']}. {item.get('summary', '')[:300]}")
            sources.append({"type": "rss", "title": item["title"], "link": item.get("link")})

        # 2. Cheap news APIs (TheNewsAPI free tier is generous)
        if self.settings.thenewsapi_api_key:
            news_items = await self._fetch_thenewsapi(question, hours)
            for it in news_items[:4]:
                raw_texts.append(f"{it['title']}. {it.get('snippet', '')[:280]}")
                sources.append({"type": "news_api", "title": it["title"], "source": it.get("source")})

        # 3. GNews free tier
        if self.settings.gnews_api_key:
            gnews = await self._fetch_gnews(question, hours)
            for it in gnews[:3]:
                raw_texts.append(f"{it['title']}. {it.get('description', '')[:280]}")
                sources.append({"type": "gnews", "title": it["title"]})

        if not raw_texts:
            return "No recent external signals found via RSS or free news APIs.", []

        # Compress with fast/cheap LLM
        try:
            synthesis, usage = await self.llm.synthesize_research(question, raw_texts)
            facts = synthesis.get("key_facts", "")
            takeaway = synthesis.get("one_line_takeaway", "")
            sentiment = synthesis.get("sentiment", "neutral")
            combined = f"Sentiment: {sentiment}. {facts} {takeaway}"
            return combined, sources
        except Exception as exc:
            logger.warning("research_synthesis_failed", error=str(exc))
            return "\n".join(raw_texts[:3]), sources

    # ------------------------------------------------------------------ #
    # Individual fetchers (best-effort, never fatal)
    # ------------------------------------------------------------------ #

    async def _fetch_relevant_rss(self, question: str, hours: int) -> list[dict[str, Any]]:
        """Use Google News RSS + a few high-signal feeds as starting point."""
        feeds = [
            "https://news.google.com/rss/search?q={q}+when:{h}h&hl=en-US&gl=US&ceid=US:en",
            "https://rss.politico.com/politics.xml",
            "https://feeds.reuters.com/reuters/topNews",
        ]
        q = "+".join(question.split()[:6])  # crude keyword query
        since = hours

        items: list[dict[str, Any]] = []
        for template in feeds:
            url = template.format(q=q, h=since)
            try:
                resp = await self.client.get(url)
                feed = feedparser.parse(resp.text)
                for entry in feed.entries[:6]:
                    items.append(
                        {
                            "title": entry.get("title", ""),
                            "summary": entry.get("summary", ""),
                            "link": entry.get("link", ""),
                            "published": entry.get("published", ""),
                        }
                    )
            except Exception:
                continue
        return items

    async def _fetch_thenewsapi(self, question: str, hours: int) -> list[dict[str, Any]]:
        key = self.settings.thenewsapi_api_key
        if not key:
            return []
        url = "https://api.thenewsapi.com/v2/news/all"
        params = {
            "api_token": key,
            "search": " ".join(question.split()[:5]),
            "language": "en",
            "limit": 6,
            "published_after": (datetime.utcnow() - timedelta(hours=hours)).isoformat(timespec="seconds"),
        }
        try:
            r = await self.client.get(url, params=params)
            data = r.json()
            return [
                {"title": d.get("title"), "snippet": d.get("snippet") or d.get("description"), "source": d.get("source")}
                for d in data.get("data", [])[:6]
            ]
        except Exception:
            return []

    async def _fetch_gnews(self, question: str, hours: int) -> list[dict[str, Any]]:
        key = self.settings.gnews_api_key
        if not key:
            return []
        url = "https://gnews.io/api/v4/search"
        params = {
            "q": " ".join(question.split()[:5]),
            "token": key,
            "lang": "en",
            "max": 5,
            "from": (datetime.utcnow() - timedelta(hours=hours)).strftime("%Y-%m-%dT%H:%M:%SZ"),
        }
        try:
            r = await self.client.get(url, params=params)
            data = r.json()
            return [
                {"title": a.get("title"), "description": a.get("description"), "source": a.get("source", {}).get("name")}
                for a in data.get("articles", [])
            ]
        except Exception:
            return []

    async def close(self) -> None:
        await self.client.aclose()

    async def quick_news_relevance_score(
        self, question: str, raw_texts: list[str] | None = None, category: str | None = None
    ) -> dict[str, Any]:
        """
        Ultra-cheap relevance scorer for the broader market batch (used for persistent news scores).
        Intended to be called on 10-20 markets per cycle using only free RSS + FAST_MODEL.
        Returns a dict ready for MarketScoreRepo.log_news_score.
        """
        if raw_texts is None:
            # Best-effort minimal fetch (only RSS, no paid APIs, short timeout)
            rss = await self._fetch_relevant_rss(question, hours=48)
            raw_texts = [f"{it['title']}. {it.get('summary','')[:180]}" for it in rss[:4]]

        if not raw_texts:
            return {
                "news_score": 0.10,
                "has_fresh_signal": False,
                "sentiment": "neutral",
                "key_signal": "no_rss_hits",
                "sources_count": 0,
            }

        # Light compression + relevance judgment via the cheap fast model
        try:
            # Re-use existing synthesize path (it already does a small fast call)
            synthesis, usage = await self.llm.synthesize_research(question, raw_texts[:6])
            facts = synthesis.get("key_facts", "") or ""
            takeaway = synthesis.get("one_line_takeaway", "") or ""
            sentiment = synthesis.get("sentiment", "neutral")

            # Simple heuristic score from the synthesis
            score = 0.35
            if len(facts) > 40:
                score += 0.25
            if sentiment in ("bullish", "bearish"):
                score += 0.20
            if "surprise" in (synthesis.get("surprise_level") or "").lower():
                score += 0.15
            score = max(0.08, min(0.92, score))

            return {
                "news_score": round(score, 3),
                "has_fresh_signal": score > 0.35,
                "sentiment": sentiment,
                "key_signal": takeaway[:220] or facts[:220],
                "sources_count": len(raw_texts),
                "llm_model": usage.get("model") if isinstance(usage, dict) else None,
            }
        except Exception as exc:
            logger.warning("quick_relevance_failed", error=str(exc)[:80])
            # Rule-based fallback
            base = 0.22 + min(len(raw_texts) * 0.08, 0.35)
            return {
                "news_score": round(base, 3),
                "has_fresh_signal": len(raw_texts) >= 2,
                "sentiment": "neutral",
                "key_signal": f"{len(raw_texts)} raw items (fallback)",
                "sources_count": len(raw_texts),
            }
