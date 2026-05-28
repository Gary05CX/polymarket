[Traditional Chinese](README_zh.md)

# polymarket

**Polymarket AI Trading Agent**

An autonomous AI trading agent whose **sole objective is profitability**, with **capital protection placed as the absolute highest priority**.

### Design Objectives (Strictly Ordered)

1. **Minimum Goal (Non-Negotiable)**: Before the API budget (~$10) is exhausted, wallet funds **must never drop below the initial capital**. Must achieve at least +$0.01 profit.
2. **Acceptable Goal**: Realized profit is sufficient to cover all API / LLM token costs incurred.
3. **Excellent Goal**: Total profit exceeds $20 USD.

### Core Constraints
- Starting capital: approximately **$10 USD**
- API budget: approximately **$10 USD**
- Database must be **DuckDB** or **PostgreSQL**
- Must be fully compatible with Windows, macOS, and Linux
- All sensitive information (private keys, API keys) **must** be managed exclusively through `.env`
- All trading must go through the official `polymarket-client` SDK

### Current Status
- Primary development branch: `feat/polymarket-ai-trading-agent`
- Core components are implemented: Risk Manager (with strict capital protection rules), Edge Detector, Research Engine, and Autonomous Loop.
- The agent can already execute **full decision cycles in dry-run mode** (`run --dry-run`), logging complete reasoning and risk evaluations to DuckDB.
- Further hardening, testing, monitoring, and live trading integration are still in progress.

**Strong recommendation**: Before using this project, please read this document and `docs/architecture.md` in full. **Always run with `--dry-run` mode** for an extended period before considering any real trading.

Detailed design rationale, risk control rules, system architecture diagram (Mermaid), project structure, and operational flow are provided below.

---

## 1. System Architecture Diagram (Mermaid)

```mermaid
flowchart TD
    subgraph "External World"
        PM_GAMMA[Polymarket Gamma API<br/>Markets, Events, Prices]
        PM_CLOB[Polymarket CLOB + Official SDK<br/>Order Book, Trading]
        NEWS_RSS[Free RSS + News APIs<br/>Politico, Reuters, GNews, RSSHub]
        LLM_PROVIDERS[LLM Providers<br/>Grok Fast / GPT-4o-mini / Haiku<br/>(via litellm)]
    end

    subgraph "Polymarket AI Trader (Python)"
        direction TB
        CLI[Typer CLI<br/>run / status / dry-run / export]
        
        subgraph "Agent Core"
            LOOP[Autonomous Loop<br/>Configurable interval + Multiple safety gates]
            SCANNER[Market Scanner<br/>Liquidity + Volume + Category filters]
            RESEARCH[Research Engine<br/>RSS + News scraping + Summarization]
            EDGE[Edge Detector<br/>LLM probability judgment + Rule engine]
            RISK[Risk Manager<br/>Capital Guardian + Position sizing<br/>Hard circuit breakers]
            EXEC[Trade Executor<br/>SDK wrapper + Dry-Run simulation]
        end

        subgraph "Data & Persistence"
            DB[(DuckDB<br/>trader.duckdb<br/>Decisions · Trades · P&L<br/>API spend · Full audit trail]]
            CONFIG[.env Configuration Loader<br/>(pydantic-settings)]
            LOGGER[Structured Logging<br/>+ Permanent decision reasoning storage]
        end
    end

    LOOP -->|1. Scan candidate markets| SCANNER
    SCANNER -->|2. External research| RESEARCH
    RESEARCH -->|3. Fact summary| EDGE
    EDGE -->|4. Estimate true probability, edge, confidence| RISK
    RISK -->|5. Veto or approve + position size| EXEC
    EXEC -->|6. Place order (official SDK)| PM_CLOB
    EXEC -->|Fill, cancel, error| DB
    EDGE -->|Full reasoning + sources + LLM output| DB
    RISK -->|Capital snapshot, limits, circuit breaker decisions| DB

    PM_GAMMA --> SCANNER
    NEWS_RSS --> RESEARCH
    LLM_PROVIDERS --> EDGE
    CLI --> LOOP
    CLI -->|Query status| DB

    classDef critical fill:#fee2e2,stroke:#ef4444
    class RISK,LOOP critical
```

**Most important data flows**:
- **Every trading path** must pass through `RiskManager` review. No exceptions.
- Every LLM call and decision is fully written to DuckDB (including prompts, responses, and research sources).
- Capital and API spend are checked before each iteration.

---

## 2. Complete Project Structure (In Progress)

```
polymarket-ai-trader/
├── .env.template              # ← Copy to .env and fill in (never commit)
├── .gitignore
├── README.md / README_zh.md
├── pyproject.toml             # uv + Python 3.12+
├── src/polymarket_trader/
│   ├── cli.py                 # Typer entrypoint
│   ├── config.py
│   ├── logging.py
│   ├── agent/
│   │   ├── loop.py            # Autonomous decision loop (functional in dry-run)
│   │   ├── risk_manager.py    # ★ Most critical module: capital protection
│   │   ├── edge_detector.py   # LLM + rule-based edge evaluation
│   │   └── ...
│   ├── polymarket/client.py   # Official SDK wrapper + full dry-run support
│   ├── data/db.py             # DuckDB schema + capital snapshot + audit trail
│   ├── llm/provider.py        # litellm abstraction + cost tracking
│   └── ...
├── docs/architecture.md
├── tests/                     # Focus on testing risk rules
└── data/ logs/                # Runtime generated (already gitignored)
```

---

## 3. Project Operation Flow (High Level)

1. Startup → Load `.env` → Validate keys → Initialize DuckDB (record capital snapshot)
2. Main Loop (every 5~30 minutes):
   - Scanner filters high-liquidity markets with edge potential
   - Research engine performs initial research using **free RSS + low-cost news APIs**
   - Edge Detector calls LLM to estimate "true probability" vs. market-implied probability and calculates edge
   - **Risk Manager** performs strict review (capital, daily loss, API budget, per-trade risk cap, and 11 hard rules)
   - Only decisions passing all checks are allowed to place orders (default: limit maker orders to earn maker rebate)
3. Every decision, research item, trade, and cost is written to DB (permanently auditable)
4. If capital falls below initial value or API budget is nearly exhausted → trading stops immediately

---

## 4. Risk Control Rules (Highest Priority)

(Summary of key rules. The complete 11 iron laws are documented in the original design plan.)

- **Sacred Capital Snapshot**: The `initial_capital_usd` recorded during the first `init` is immutable for the lifetime of the project.
- Per-trade risk limit: `min(current_equity × 4%, $0.75)` (extremely conservative at the beginning).
- Minimum edge: default ≥ 9.5% (after fees, slippage, and research costs).
- Daily loss circuit breaker: If loss > 5% of initial capital within 24 hours → pause trading for 24~48 hours.
- API budget guardian: If research costs are about to exceed remaining budget or have already exceeded realized profit → automatically reduce research depth or skip trading.
- Forced maker bias: Prefer posting limit orders to earn rebates.
- All rejected trades are fully logged with "why they were rejected".
- **Paper Trading mode** is the default and strongly recommended starting approach.

---

## 5. Overall Design Philosophy (How the Three Goal Levels Are Addressed)

- **Minimum Goal (Capital never broken + at least +$0.01)**: The entire architecture is built around this goal.
  - RiskManager is an unavoidable checkpoint on every path.
  - High thresholds + tiny position sizes + "do nothing" as the default behavior.
  - The agent will proactively reject any trade with even a remote chance of breaking capital.
- **Acceptable Goal (Profit covers API costs)**:
  - Every decision estimates the research cost for that cycle.
  - Only trades where "expected value is clearly 2~3× higher than research cost" are allowed to proceed.
  - Free research sources (RSS) are always preferred.
- **Excellent Goal (>$20)**:
  - Position sizes are increased only after genuinely safe profits and equity growth.
  - The historical decision database can later be used for simple self-improvement (which market types and prompts perform better).

**Conclusion**: This agent is deliberately designed with the principle: *"It is better to miss 100 good opportunities than to make even one trade that risks breaking capital."*

---

## 6. Getting Started (Extremely Important)

1. Install [uv](https://docs.astral.sh/uv/) (recommended for speed)
2. `cp .env.template .env` and fill in:
   - Polygon private key (recommended: use a fresh small wallet containing only $12~15 USDC)
   - At least one LLM API key (Grok Fast or GPT-4o-mini are the most cost-effective)
3. `uv sync`
4. `uv run polymarket-trader init`
5. **Use `--dry-run` extensively** to observe decision quality and risk logic
6. Only after confirming everything looks safe, consider setting `PAPER_TRADING` to false, keep `REQUIRE_TRADE_CONFIRMATION` enabled, and test with the smallest possible amounts first

**Repeated Warning**:
- This involves real money.
- Prediction markets are inherently difficult to find consistent edge in.
- This project is still under active development. Both the APIs and the SDK may change.
- The author and any contributors are not responsible for any losses.

---

If you have any questions, wish to contribute, or want to discuss edge detection logic for specific markets, feel free to open an issue or discuss directly on the relevant branch.

Protect capital first. Trade conservatively.