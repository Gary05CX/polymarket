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
                # For xAI models, response_format often causes the model to return
                # the JSON wrapped in single quotes (Python string literal).
                # We disable it for grok/xai models and rely on strong parsing + retry.
                **({"response_format": {"type": "json_object"}} if not model.lower().startswith(("xai/", "grok")) else {}),
            )
            content = resp.choices[0].message.content  # type: ignore[attr-defined]
            usage = getattr(resp, "usage", {}) or {}
            input_t = int(getattr(usage, "prompt_tokens", 0) or 0)
            output_t = int(getattr(usage, "completion_tokens", 0) or 0)

            try:
                data = self._robust_parse_json(content, model)
            except Exception as parse_err:
                # One retry with correction prompt (cheap because we only do it on failure)
                logger.warning("llm_json_parse_retry", model=model, original_error=str(parse_err)[:150])
                correction_prompt = (
                    "Your previous response could NOT be parsed as valid JSON.\n"
                    "The most common mistake you made was wrapping the entire JSON inside single quotes like this:\n"
                    "'{ ... }'\n\n"
                    f"Here is what you output last time (first 1200 chars):\n{content[:1200]}\n\n"
                    "**This time, output ONLY the raw JSON. Do NOT wrap it in any quotes (single or double).**\n"
                    "Do not add any text before or after. Start directly with { and end with }."
                )
                try:
                    retry_resp = await acompletion(
                        model=model,
                        messages=[{"role": "user", "content": correction_prompt}],
                        max_tokens=max_tokens,
                        temperature=min(temperature, 0.1),
                        **({"response_format": {"type": "json_object"}} if not model.lower().startswith(("xai/", "grok")) else {}),
                    )
                    retry_content = retry_resp.choices[0].message.content
                    data = self._robust_parse_json(retry_content, model)
                    # Update tokens roughly
                    usage = getattr(retry_resp, "usage", {}) or {}
                    input_t += int(getattr(usage, "prompt_tokens", 0) or 0)
                    output_t += int(getattr(usage, "completion_tokens", 0) or 0)
                    content = retry_content  # for downstream logging
                except Exception:
                    raise parse_err from None

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

    def _robust_parse_json(self, content: str, model: str) -> dict[str, Any]:
        """Extremely defensive JSON parser for grok models that often return weird formats."""
        if not content or not isinstance(content, str):
            raise ValueError("Empty or invalid content from LLM")

        original = content.strip()

        # === 強力清理策略（針對 grok 常見的各種怪格式）===
        candidates = [original]

        # 1. 移除各種 markdown / code block
        cleaned = original
        for prefix in ("```json", "```JSON", "```", "'''json", "'''", '"""json', '"""'):
            if cleaned.lower().startswith(prefix.lower()):
                cleaned = cleaned[len(prefix):].strip()
        for suffix in ("```", "'''", '"""'):
            if cleaned.lower().endswith(suffix.lower()):
                cleaned = cleaned[:-len(suffix)].strip()
        candidates.append(cleaned)

        # 2. 非常強力處理 grok 常見的「整個回應被單引號包住」的錯誤
        #    例如：'\n  "key_facts": ...' 或 '{\n  "key_facts": ...}'
        for quote in ("'", '"'):
            if original.startswith(quote) and original.endswith(quote):
                inner = original[1:-1].strip()
                candidates.append(inner)
                # 去掉開頭的換行 + 空白
                inner2 = inner.lstrip("\n\r\t ").strip()
                candidates.append(inner2)
                # 再去一次引號（有時候會雙層）
                if (inner2.startswith(quote) and inner2.endswith(quote)):
                    candidates.append(inner2[1:-1].strip())
                # 嘗試 unescape
                try:
                    unescaped = inner.replace("\\'", "'").replace('\\"', '"')
                    candidates.append(unescaped)
                except Exception:
                    pass

        # 3. 找出第一個 { 到最後一個 } 之間的內容（最強大的 fallback）
        try:
            first_brace = original.find("{")
            last_brace = original.rfind("}")
            if first_brace != -1 and last_brace != -1 and last_brace > first_brace:
                json_only = original[first_brace:last_brace + 1]
                candidates.append(json_only)
        except Exception:
            pass

        # 4. 嘗試所有候選
        for cand in candidates:
            cand = cand.strip()
            if not cand:
                continue
            try:
                return json.loads(cand)
            except Exception:
                continue

        # 5. 最後紀錄詳細錯誤並拋出
        logger.warning(
            "llm_json_parse_failed",
            model=model,
            raw_content=original[:600],   # 記錄比較多內容方便 debug
        )
        raise ValueError(f"Failed to parse JSON from model {model}. Raw content (first 600 chars):\n{original[:600]}")

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
        return await self._call(model, prompt, max_tokens=550, temperature=0.15)

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
        return await self._call(model, prompt, max_tokens=350, temperature=0.1)


# Singleton
_provider: LLMProvider | None = None


def get_llm_provider() -> LLMProvider:
    global _provider
    if _provider is None:
        _provider = LLMProvider()
    return _provider
