[English](README.md)
# polymarket

**Polymarket AI Trading Agent**（中文主要文件）

這是一個**以盈利為唯一目標**、把「保護本金」放在最高優先的自主 AI 交易代理。

### 設計目標（嚴格排序）
1. **最低目標（不可妥協）**：在 API 預算（約 $10）消耗完之前，錢包資金**絕對不能低於本金**，至少要賺到 $0.01。
2. **合格目標**：賺回來的利潤能夠 cover 所使用的 API Token 費用。
3. **滿分目標**：總利潤超過 $20 USD。

### 核心限制
- 可用資金約 **$10 USD**
- API 預算約 **$10 USD**
- 資料庫只能用 **DuckDB** 或 **PostgreSQL**
- 必須完整相容 Windows、macOS、Linux
- 所有敏感資訊（私鑰、API Key）**只能**透過 `.env` 管理
- 交易一律使用官方 `polymarket-client` SDK

### 目前狀態
- 主要開發分支：`feat/polymarket-ai-trading-agent`
- 目前為**基礎架構階段**（skeleton + 安全框架 + 配置 + DB + CLI 雛形）
- 完整自主迴圈、Edge Detector、Risk Manager 正在積極實作中

**強烈建議**：任何人使用前務必先完整閱讀本文件與 `docs/architecture.md`，並且**永遠先用 `--dry-run` 模式**跑很久再考慮真實交易。

詳細設計思路、風險控制規則、系統架構圖（Mermaid）、專案結構、運作流程，請繼續閱讀下方完整說明。

---

## 1. 系統架構圖（Mermaid）

```mermaid
flowchart TD
    subgraph "外部世界"
        PM_GAMMA[Polymarket Gamma API<br/>市場、事件、價格]
        PM_CLOB[Polymarket CLOB + 官方 SDK<br/>訂單簿、交易]
        NEWS_RSS[免費 RSS + 新聞 API<br/>Politico、Reuters、GNews、RSSHub]
        LLM_PROVIDERS[LLM 提供者<br/>Grok Fast / GPT-4o-mini / Haiku<br/>（透過 litellm）]
    end

    subgraph "Polymarket AI Trader（Python）"
        direction TB
        CLI[Typer CLI<br/>run / status / dry-run / export]
        
        subgraph "Agent 核心"
            LOOP[自主迴圈<br/>可設定間隔 + 多重安全閘門]
            SCANNER[市場掃描器<br/>流動性 + 成交量 + 類別過濾]
            RESEARCH[研究引擎<br/>RSS + 新聞抓取 + 摘要]
            EDGE[Edge 偵測器<br/>LLM 機率判斷 + 規則引擎]
            RISK[風險管理器<br/>本金守護者 + 倉位計算<br/>硬性斷路器]
            EXEC[交易執行器<br/>SDK 包裝 + Dry-Run 模擬]
        end

        subgraph "資料與持久化"
            DB[(DuckDB<br/>trader.duckdb<br/>決策 · 交易 · 損益<br/>API 花費 · 完整稽核]]
            CONFIG[.env 設定載入器<br/>（pydantic-settings）]
            LOGGER[結構化日誌<br/>+ 決策理由永久保存]
        end
    end

    LOOP -->|1. 掃描候選市場| SCANNER
    SCANNER -->|2. 外部研究| RESEARCH
    RESEARCH -->|3. 事實摘要| EDGE
    EDGE -->|4. 估計真實機率、edge、信心| RISK
    RISK -->|5. 否決或批准 + 倉位大小| EXEC
    EXEC -->|6. 下單（官方 SDK）| PM_CLOB
    EXEC -->|成交、取消、錯誤| DB
    EDGE -->|完整理由 + 來源 + LLM 輸出| DB
    RISK -->|本金快照、限制、斷路決策| DB

    PM_GAMMA --> SCANNER
    NEWS_RSS --> RESEARCH
    LLM_PROVIDERS --> EDGE
    CLI --> LOOP
    CLI -->|查詢狀態| DB

    classDef critical fill:#fee2e2,stroke:#ef4444
    class RISK,LOOP critical
```

**最重要資料流**：
- **所有交易路徑**都必須經過 `RiskManager` 審核，沒有例外。
- 每一次 LLM 呼叫與決策都會完整寫入 DuckDB（含提示、回應、研究來源）。
- 本金與 API 花費在每次迭代前都會被檢查。

---

## 2. 完整專案結構（目前進行中）

```
polymarket-ai-trader/
├── .env.template              # ← 複製成 .env 後填寫（絕對不要 commit）
├── .gitignore
├── README.md / README_zh.md
├── pyproject.toml             # uv + Python 3.12+
├── src/polymarket_trader/
│   ├── cli.py                 # Typer 入口
│   ├── config.py
│   ├── logging.py
│   ├── agent/
│   │   ├── loop.py            # 自主主迴圈（開發中）
│   │   ├── risk_manager.py    # ★ 最重要模組：本金守護
│   │   └── ...
│   ├── polymarket/client.py   # 官方 SDK 包裝 + dry-run
│   ├── data/db.py             # DuckDB schema + 資本快照
│   ├── llm/provider.py        # litellm 抽象 + 成本估算
│   └── ...
├── docs/architecture.md
├── tests/                     # 重點測試 risk rules
└── data/ logs/                # 執行期產生（已 gitignore）
```

---

## 3. 專案運作流程（高階）

1. 啟動 → 載入 `.env` → 驗證金鑰 → 初始化 DuckDB（建立資本快照）
2. 主迴圈（每 5~30 分鐘）：
   - Scanner 過濾高流動性、有 edge 潛力的市場
   - Research 引擎用**免費 RSS + 廉價新聞 API**先做研究
   - Edge Detector 呼叫 LLM 估計「真實機率」vs 市場隱含機率，計算 edge
   - **Risk Manager** 嚴格審核（本金、每日虧損、API 預算、單筆風險上限等 11 條鐵律）
   - 只有通過所有檢查才允許下單（預設 limit maker 單，賺 maker rebate）
3. 每一次決策、研究、交易、成本都會寫入 DB（永久可稽核）
4. 任何時候本金低於初始值或 API 預算快用完 → 立即停止交易

---

## 4. 風險控制具體規則（最高優先）

（完整 11 條鐵律見原始 plan.md，此處摘要重點）

- **神聖資本快照**：第一次 `init` 時記錄的 `initial_capital_usd` 終生不可破。
- 單筆風險上限：`min(目前權益 × 4%, $0.75)`（一開始極度保守）。
- 最小 edge：預設 ≥ 9.5%（已扣除費用、滑價、研究成本）。
- 每日虧損斷路器：24 小時內虧損 > 5% 本金 → 暫停交易 24~48 小時。
- API 預算守護：研究成本即將超過剩餘預算或已超過已實現利潤 → 自動降低研究深度甚至不交易。
- 強制 maker 傾向：盡量掛限價單賺 rebate。
- 所有被否決的交易也會完整記錄「為什麼被否決」。
- **Paper Trading 模式** 是預設且強烈建議的起步方式。

---

## 5. 整體設計思路（如何針對三個目標等級設計）

- **最低目標（本金不破 + 至少 +$0.01）**：整個架構就是為了這個目標而生。
  - RiskManager 是所有路徑的必經關卡。
  - 高門檻 + 極小倉位 + 「什麼都不做」是預設行為。
  - 只要有一絲可能破本金的交易，Agent 會主動拒絕。
- **合格目標（利潤 cover API 費用）**：
  - 每次決策都會估算本次研究花費。
  - 只有「預期價值明顯高於研究成本 2~3 倍以上」的交易才會通過。
  - 優先使用完全免費的研究來源（RSS）。
- **滿分目標（>$20）**：
  - 當真正安全獲利、權益成長後，倉位才會緩慢放大。
  - 歷史決策資料庫未來可用來做簡單的自我改進（哪類市場、哪種 prompt 表現較好）。

**結論**：這個 Agent 設計上是「寧可錯過 100 個好機會，也絕不做 1 個可能傷本金的交易」。

---

## 6. 開始使用（極度重要）

1. 安裝 [uv](https://docs.astral.sh/uv/)（最快）
2. `cp .env.template .env` 並填寫：
   - Polygon 私鑰（建議用全新小錢包，只放 $12~15 USDC）
   - 至少一個 LLM API Key（Grok Fast 或 GPT-4o-mini 最划算）
3. `uv sync`
4. `uv run polymarket-trader init`
5. **長期使用 `--dry-run`** 觀察決策品質與風險邏輯
6. 確認沒問題後，再考慮把 `PAPER_TRADING` 改 false，並把 `REQUIRE_TRADE_CONFIRMATION` 開著，先用最小金額測試

**再次警告**：
- 這是真金白銀。
- 預測市場本來就很難有穩定 edge。
- 本專案目前仍處於積極開發階段，API 與 SDK 也可能有變動。
- 作者與任何貢獻者都不對任何損失負責。

---

有任何問題、想貢獻、或想討論特定市場的 edge 判斷邏輯，歡迎開 issue 或直接在對應分支討論。

保護本金，穩健為先。
