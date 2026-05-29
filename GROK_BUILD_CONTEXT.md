# GROK BUILD CONTEXT
## Polymarket AI Trading Agent

**Purpose**: This document allows any new Grok session (on any machine) to fully understand the current state of the project, the history of decisions, and how to continue working without requiring the user to repeat context.

**Last Updated**: 2026-05-29  
**Written by**: Grok Build (xAI) for future Grok Build sessions  
**Language**: English (optimized for AI consumption)

---

## 1. Project Mission & Non-Negotiable Principles

### Primary Objective
Build an **autonomous Polymarket trading agent** that can only be considered successful if it is **profitable** while starting with very small capital (~$10 USD) and a small API budget (~$10 USD).

### Success Tiers (Strict Priority Order)
1. **Minimum (Non-negotiable)**: Never let wallet balance drop below the initial sacred capital before the API budget is exhausted. Must achieve at least +$0.01 net.
2. **Qualified**: Net profit must cover all API/LLM costs.
3. **Stretch**: Total profit > $20 USD.

### Sacred Principles (Never Violate)
- **Capital Protection is #1 priority** — above all else, including making the agent "more active".
- The agent must be **extremely selective** and comfortable doing nothing for long periods.
- All trading decisions must go through `RiskManager` with no exceptions or overrides.
- Paper trading (`--dry-run`) is the default and strongly preferred mode.
- Every decision, research step, LLM call, and cost must be fully auditable in DuckDB.

**Key Constraint**: The user is Chinese-speaking and values clear, direct communication. Technical decisions should be explained clearly, but excessive English-to-Chinese translation during thinking is not required when working internally.

---

## 2. Current Branch & High-Level Status

- **Active Branch**: `feat/polymarket-ai-trading-agent`
- **Primary Database**: DuckDB (`data/trader.duckdb`)
- **Current Maturity**: The agent has a complete decision pipeline that runs in dry-run mode. It can scan, research, evaluate edge, apply strict risk rules, and log everything. Live trading is not yet wired into the main loop (`executor.py` exists but is not called from `run_once`).

**Most Recent Major Milestone (Completed 2026-05-29)**:
The implementation of the **Persistent Market Intelligence Layer** based on the user's explicit architectural request.

---

## 3. The User's Three Pain Points + The Requested Solution

In the latest phase of work, the user explicitly stated three problems:

1. **LLM keeps failing but still burns tokens repeatedly** (especially JSON parsing issues with Grok models returning single-quoted strings).
2. **The agent gets stuck on the same market types** (heavily biased toward high-volume sports events like FIFA 2026 family).
3. **RiskManager / EdgeDetector are too conservative** — even with weak research, the agent should sometimes dare to propose small positions.

**User's Explicit Solution (Direct Quote)**:
> "既然你是從polymarket抓取一批活躍的市場來玩, 不如給他一個限制, 抓完後先放進database, 每一次的跑完用新聞給分後, 都寫進database, 之後再跑的時候, 先根據上次給同一個盤打的分數來決定是否跳過, 讓agent不要浪費時間在上次放棄過的盤."

This became the guiding requirement for the recent major refactor.

---

## 4. Architecture: Persistent Market Intelligence Layer (The Big Change)

### Core Flow (Implemented)
1. `MarketScanner.find_candidates()` calls `list_active_markets()`.
2. **Immediately** after fetching the raw batch → call `MarketScoreRepo.record_batch_snapshots(raw)` (persist first).
3. Apply existing filters + new filter: `MarketScoreRepo.get_markets_to_skip(threshold=0.22, hours=36)`.
4. Mix in historically promising markets from `get_diverse_high_potential_from_history()`.
5. For candidates that reach research:
   - If research returns "No recent external signals" → immediately write low score (0.12) and **skip the expensive reasoning LLM**.
   - After full evaluation (even if vetoed or LLM failed) → write/update score in `market_news_scores`.
6. On future cycles, low-scoring markets are cheaply filtered before any LLM calls.

### New / Heavily Used Tables
- `market_snapshots`: Snapshot of every market batch fetched (volume, liquidity, category, implied_prob, etc.).
- `market_news_scores`: The critical intelligence table.
  - `news_score` (0.0–1.0)
  - `notes` (e.g. "llm_failure", "zero_signal_bypass", "deep_eval_approved", "skipped_prior")
  - `has_fresh_signal`, `sentiment`, `key_signal`, `event_family`

This design directly attacks token waste by making expensive LLM calls the exception rather than the default for previously evaluated weak markets.

---

## 5. Key Design Decisions & Rationale

### Decision: Micro Exploratory Lane Instead of Lowering Main Thresholds
- We did **not** globally relax `min_edge_percent` (9.5%) or confidence gates (0.62 in RiskManager, 0.55 in EdgeDetector).
- Instead, we created a parallel **micro lane**:
  - Triggered only when `research_quality` is low/weak **and** there is still directional edge.
  - Much smaller size cap (`$0.10 – $0.30`).
  - Slightly relaxed gates (5.5% edge / 0.40 confidence).
  - Still fully subject to all hard capital breakers (equity protection, budget exhaustion, recent losses, blacklist, max positions).
- Rationale: User wanted the agent to "dare" more on weak research, but capital protection must remain absolute. A dedicated, auditable, tiny-risk lane satisfies both goals.

### Decision: Write Low Scores Even on LLM Failures
- When `llm_judge_failed_fallback_pass` or JSON parse failure occurs, we still call `MarketScoreRepo.log_news_score(..., notes="llm_failure", news_score=0.15)`.
- This ensures the same problematic market is cheaply skipped next time instead of repeatedly wasting tokens on retries.

### Other Notable Decisions
- Improved `PolymarketClient` to extract real `implied_prob` from `outcomes` when available (previously always defaulted to 0.5, which was poisoning edge calculations and LLM judgments).
- `max_llm_calls_per_cycle` is now somewhat enforced in the loop (rough accounting).
- All new scoring logic is best-effort and non-fatal (never breaks the main flow).

---

## 6. Critical Files & Responsibilities

| File | Role | Notes |
|------|------|-------|
| `src/polymarket_trader/agent/scanner.py` | Market discovery + DB-first persistence + prior score filtering + diversity injection | Most important behavioral change location |
| `src/polymarket_trader/data/repositories.py` | `MarketScoreRepo` (new) | Contains `record_batch_snapshots`, `log_news_score`, `get_markets_to_skip`, `get_diverse_high_potential_from_history`, etc. |
| `src/polymarket_trader/agent/loop.py` | Main orchestration + early exits before expensive LLM calls | Contains the "skip zero signal" and "skip prior low score" logic |
| `src/polymarket_trader/agent/edge_detector.py` | LLM probability judgment + micro lane detection | Sets `is_micro_position` flag |
| `src/polymarket_trader/agent/risk_manager.py` | All capital protection logic + micro sizing | Sacred module — changes here require extreme caution |
| `src/polymarket_trader/polymarket/client.py` | Market data fetching + price enrichment | Now populates `implied_prob` and `price_source` |
| `src/polymarket_trader/data/db.py` | Schema (including new `market_news_scores` table) | |
| `GROK_BUILD_CONTEXT.md` | This file | Primary context restoration document |

---

## 7. Configuration (New Tunables)

Located in `config.py` and `.env.template`:

- `BATCH_NEWS_SCORING_ENABLED`
- `NEWS_SCORE_SKIP_THRESHOLD` (default 0.22)
- `NEWS_SCORE_COOLDOWN_HOURS` (default 36)
- `MICRO_EXPLORATORY_ENABLED`
- `MIN_EDGE_MICRO_PERCENT` (5.5)
- `MIN_CONF_MICRO` (0.40)
- `MICRO_MAX_RISK_USD` (0.30)

These are the primary levers for tuning the new intelligence + micro behavior.

---

## 8. How to Work Efficiently in Future Sessions

When starting a new session, the user will likely ask you to read this file first.

**Recommended Internal Process**:
1. Read `GROK_BUILD_CONTEXT.md` completely.
2. Read `README.md` and `README_zh.md` (especially the "Recent major updates" section).
3. Optionally skim `docs/architecture.md`.
4. Check current git branch and recent commits if needed.
5. Ask the user clarifying questions only when truly ambiguous — otherwise proceed with high confidence based on this context.

**Communication Style with User**:
- User thinks in Chinese but is comfortable with technical English.
- Be direct. Avoid unnecessary translation during internal reasoning.
- When proposing changes, clearly state impact on capital protection and token usage.
- Always verify changes with dry-run + DB inspection when possible.

---

## 9. Current Known Limitations & Open Areas

- LLM JSON parsing (especially with Grok models returning single-quoted output) is still fragile. The current strategy is "fail fast + record low score + skip next time" rather than endless parser hardening.
- `executor.py` is not yet called from the main loop (live trading path is incomplete).
- `market_snapshots` and `market_news_scores` are growing; no cleanup/archival strategy exists yet.
- The agent still has a strong bias toward whatever markets currently have high volume on Polymarket.
- No manual "always ignore this market" management UI/CLI yet.

---

## 10. Verification Commands (Useful for Future Sessions)

```bash
# Run critical safety tests
uv run pytest tests/test_risk_manager.py -q --tb=line

# Typical development run
uv run polymarket-trader run --dry-run --max-iterations 5

# Inspect recent intelligence scores
uv run python -c '
import duckdb
con = duckdb.connect("data/trader.duckdb")
print(con.execute("""
    SELECT market_id, news_score, notes, category, scored_at 
    FROM market_news_scores 
    ORDER BY scored_at DESC 
    LIMIT 10
""").fetchall())
con.close()
'
```

---

## 11. Summary for Quick Restoration (TL;DR for Grok)

- This is a capital-protection-first Polymarket trading agent.
- The latest major evolution is the **Persistent Market Intelligence Layer**: fetch → persist immediately → score with news → use scores to skip on future runs.
- This was built specifically to solve repeated LLM token waste, lack of market diversity, and excessive conservatism on weak research.
- We introduced a controlled "micro exploratory" path for small positions instead of weakening the main risk rules.
- Capital protection rules in `RiskManager` are sacred and must never be relaxed for the sake of activity.
- When in doubt, prefer safety, auditability, and cheap early exits over aggressive behavior.

---

**End of Context Document**

This file should be the primary source of truth for any future Grok Build session working on this project. Update it whenever major architectural or behavioral changes are made.