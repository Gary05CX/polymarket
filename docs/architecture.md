# Architecture & Design

See the main README for the primary Mermaid diagram and explanation.

This document will contain deeper technical details after initial implementation.

## Core Principles (Non-Negotiable)

1. **Capital Protection First** — The RiskManager can veto *any* action.
2. **Full Auditability** — Every decision, research step, and trade is recorded immutably.
3. **Cheap Research First** — RSS + free news APIs before any paid LLM or search call.
4. **Willingness to Do Nothing** — Most cycles will result in PASS. This is a feature.

## Key Components

(See plan.md in the session directory for the original approved design and the 2026 follow-up section on Persistent Market Intelligence.)

**New (2026 iteration)**: Market Intelligence Layer
- Scanner always persists the raw active batch to `market_snapshots` + `market_news_scores` first.
- Cheap news relevance scoring happens on the batch; scores are written back every cycle.
- Subsequent runs use `get_markets_to_skip()` + prior low scores to avoid re-researching low-value markets (directly solves repeated LLM token waste).
- Same scores + category distribution enable real diversity beyond pure volume ranking.
- Micro exploratory lane (tiny positions only on weak research) lives in EdgeDetector + RiskManager with its own relaxed gates while all capital breakers remain enforced.
