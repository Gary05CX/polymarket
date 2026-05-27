"""
LLM Provider abstraction using litellm.

Features:
- Unified interface for many providers (Grok, OpenAI, Anthropic, etc.)
- Rough cost estimation using known pricing tables (updated 2026)
- Structured JSON output enforcement where supported
- Automatic cheap/fast model routing for research vs reasoning
"""
from __future__ import annotations

import json
from typing import Any

import litellm
from litellm import acompletion

from ..config import get_settings
from ..data.repositories import CostRepo
from ..logging import get_logger
from .prompts import PROBABILITY_JUDGE_V1, RESEARCH_SYNTHESIS_V1

logger = get_logger(__name__)

# Very rough 2026 pricing (USD per 1M tokens) - update periodically
# Focused on cheap strong models for agents
MODEL_PRICING = {
    "grok-4-fast": {"input": 0.20, "output": 0.50},
    "grok-3-fast": {"input": 0.20, "output": 0.50},
    "gpt-4o-mini": {"input": 0.15, "output": 0.60},
    "gpt-4o-mini-2024-07-18": {"input": 0.15, "output": 0.60},
    "claude-3-5-haiku-20241022": {"input": 1.00, "output": 5.00},
    "claude-3-haiku-20240307": {"input": 0.25, "output": 1.25},
}


def _estimate_cost(model: str, input_tokens: int, output_tokens: int) -> float:
    """Rough cost in USD. Falls back to $0.0005 per 1k tokens if unknown."""
    key = model.lower()
    for k, p in MODEL_PRICING.items():
        if k in key:
            return (input_tokens * p["input"] + output_tokens * p["output"]) / 1_000_000
    return (input_tokens + output_tokens) * 0.0005 / 1000


class LLMProvider:
    def __init__(self) -> None:
        self.settings = get_settings()
        # litellm will pick up env keys automatically (XAI_API_KEY, OPENAI_API_KEY, etc.)
        litellm.set_verbose = False

    async def _call(
        self,
        model: str,
        prompt: str,
        max_tokens: int = 600,
        temperature: float = 0.3,
    ) -> tuple[dict[str, Any], dict[str, Any]]:
        """Internal call. Returns (parsed_json, usage_info)."""
        try:
            resp = await acompletion(
                model=model,
                messages=[{"role": "user", "content": prompt}],
                max_tokens=max_tokens,
                temperature=temperature,
                response_format={"type": "json_object"},  # many providers support this
            )
            content = resp.choices[0].message.content  # type: ignore[attr-defined]
            usage = getattr(resp, "usage", {}) or {}
            input_t = int(getattr(usage, "prompt_tokens", 0) or 0)
            output_t = int(getattr(usage, "completion_tokens", 0) or 0)

            # Try to parse strict JSON
            try:
                data = json.loads(content)
            except Exception:
                # Fallback: strip markdown fences if present
                cleaned = content.strip().removeprefix("```json").removesuffix("```").strip()
                data = json.loads(cleaned)

            cost = _estimate_cost(model, input_t, output_t)

            # Persist cost immediately (best effort)
            try:
                CostRepo.record_cost(
                    provider=model.split("/")[0] if "/" in model else "litellm",
                    model=model,
                    input_tokens=input_t,
                    output_tokens=output_t,
                    estimated_cost_usd=round(cost, 6),
                )
            except Exception:
                pass

            logger.info(
                "llm_call",
                model=model,
                in_tokens=input_t,
                out_tokens=output_t,
                est_cost_usd=round(cost, 5),
            )
            return data, {"model": model, "input_tokens": input_t, "output_tokens": output_t, "cost_usd": cost}

        except Exception as exc:
            logger.error("llm_call_failed", model=model, error=str(exc)[:200])
            raise

    async def judge_probability(
        self,
        *,
        question: str,
        market_price: float,
        research_summary: str,
        time_to_resolution: str = "unknown",
        category: str = "unknown",
        volume_usd: float = 0.0,
        liquidity: float = 0.0,
    ) -> tuple[dict[str, Any], dict[str, Any]]:
        """Core reasoning call: estimate true probability and edge."""
        prompt = PROBABILITY_JUDGE_V1.format(
            question=question,
            market_price=market_price,
            time_to_resolution=time_to_resolution,
            category=category,
            volume_usd=volume_usd,
            liquidity=liquidity,
            research_summary=research_summary or "No recent external information found.",
        )
        model = self.settings.reasoning_model
        return await self._call(model, prompt, max_tokens=550, temperature=0.25)

    async def synthesize_research(
        self, question: str, raw_items: list[str]
    ) -> tuple[dict[str, Any], dict[str, Any]]:
        """Cheap fast-model call to compress news into decision-relevant facts."""
        if not raw_items:
            return {"key_facts": "No recent news found.", "sentiment": "neutral"}, {"cost_usd": 0}

        prompt = RESEARCH_SYNTHESIS_V1.format(
            question=question,
            raw_items="\n- " + "\n- ".join(raw_items[:8]),  # cap input
        )
        model = self.settings.fast_model
        return await self._call(model, prompt, max_tokens=350, temperature=0.2)


# Singleton
_provider: LLMProvider | None = None


def get_llm_provider() -> LLMProvider:
    global _provider
    if _provider is None:
        _provider = LLMProvider()
    return _provider
