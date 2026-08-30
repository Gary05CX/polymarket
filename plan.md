# Plan: Go Polymarket 5m Bot (dual strategy + dual DB)

> 來源需求：`prompt.md`  
> 狀態：僅規劃，尚未實作  
> 目標：把兩個公開 Python 策略包成**一個 Go bot**，支援 **dry-run → live**。**預設 PostgreSQL**（dry-run / live 都寫這裡）；DuckDB 只當內建臨時庫（沒有 PG 的機器或一次性實驗）。

---

## 1. 目標與非目標

### 1.1 必須做到

| # | 需求 | 作法 |
| --- | ------ | ------ |
| 1 | 改寫兩個 GitHub 策略邏輯 | 各自獨立 strategy package，共用行情 / 下單 / DB |
| 2 | 單一 Go 程式 | `cmd/bot` 一個 binary，config 選 strategy / mode / DB |
| 3 | dry-run **一輪**後再決定 live | `mode: dry_run \| paper \| live`，預設 `dry_run`；`max_windows: 1` 跑完當窗 + official resolve 後退出 |
| 4 | PostgreSQL 為主庫；DuckDB 可選 | 預設 `driver: postgres`。DuckDB 是內建臨時庫；若選它 **必須**單連線單寫者（§7.5） |
| 5 | 完整 SQL 參考檔 | `sql/postgres/*.sql` + `sql/duckdb/*.sql`（同一套邏輯表；方言只差型別） |
| 6 | 官方結果 resolver | 兩個 repo 都沒有可靠 resolver → 自建；PnL **只認** Gamma official |
| 7 | schema 能做分析，不只交易 | 行情快照、訊號、下單、成交、結算、PnL 分表 |
| 8 | dry-run / live 都完整落庫 | 每筆標 `run_mode` + `strategy_id` + `strategy_source`；fill 標 `fill_model` |
| 9 | 每筆以最小可買單位下單 | 讀 CLOB book `min_order_size`（目前常見 **5 shares**） |
| 10 | 同一窗不下第二單 | `UNIQUE (run_id, market_slug)`；重啟 / delayed 重送不得雙開 |

### 1.2 非目標（v1 不做）

- 多策略同時對同一 market 下單（v1 一次只跑一個 strategy；可並行第二 process）
- Web UI / dashboard
- 自動 Kelly 大倉、多資產任意擴充（先 BTC，ETH 可選）
- 保證獲利（公開回測 ≠ 實盤）
- CLOB **V1** 簽名路徑（production 自 2026-04-28 起只接受 V2）
- 用 Chainlink **spot** 或自算 TWAP 當 official 結算

---

## 2. 研究摘要（已核對原始碼與 API）

### 2.1 策略 A — Window Delta + Micro Momentum + ATR

- **Repo**: [jmazzini/5m-poly-bot](https://github.com/jmazzini/5m-poly-bot)（`crypto_bot.py`）
- **strategy_id**: `window_delta`
- **strategy_source**: `jmazzini/5m-poly-bot`

| 參數 | 預設 | 說明 |
| ------ | ------ | ------ |
| `wake_before_sec` | 65 | 結算前開始監控 |
| `entry_seconds_min/max` | 10 / 50 | 只在此窗口下單 |
| `price_min` | BTC 0.94 / ETH 0.92 | Polymarket mid |
| `price_max` | 0.99 | 利潤太薄不打 |
| `delta_skip` | 0.0005 (0.05%) | Window Delta 門檻 |
| `min_confidence` | 0.30 | score/9 |
| `atr_periods` | 5 | 過去 5 根 5m |
| `atr_multiplier` | 1.5 | 當根 range > 1.5×ATR 跳過 |
| `poll_interval_sec` | 3 | |

**流程（原版）**

1. 睡到 `close_ts - 65s`
2. 平行拉 Gamma market + Binance TA
3. 進場窗 10–50s 內：PM 價區間、delta、confidence、Binance 方向 == PM 領先邊、ATR
4. 方向 = Binance window delta 正負（Up / Down）
5. Live：CLOB V2 BUY，size 至少 5 shares；taker price ≈ mid+0.01

**分數**

| Delta | 分 |
| ------- | ---- |
| > 1.0% | +7 |
| > 0.20% | +5 |
| > 0.10% | +3 |
| > 0.05% | +1 |
| 最近 2 根 1m 同向 | +2 |
| Max | 9 |

`confidence = abs(score) / 9`

**市場發現**

- Event slug：`{btc|eth}-updown-5m-{window_start_unix}`
- `GET https://gamma-api.polymarket.com/events?slug=...`
- Mid：`GET https://clob.polymarket.com/midpoint?token_id=...`
- Binance：`/api/v3/klines`、`/api/v3/ticker/price`

時鐘以 **CLOB / Binance server time** 對時（見 §9.1），不要信本機 NTP。10–50s 窗差 2–3s 就會打空或踩延遲撮合。

### 2.2 策略 B — 3 分鐘動量 / Inertia 評分

- **Repo**: [ratrimaa/btc-5m-polymarket-bot](https://github.com/ratrimaa/btc-5m-polymarket-bot)（`bot.py`）
- **strategy_id**: `inertia_3m`
- **strategy_source**: `ratrimaa/btc-5m-polymarket-bot`

| 參數 | 預設 | 說明 |
| ------ | ------ | ------ |
| `entry_delay_sec` | 170 | 窗開後等 ~2m50s |
| `min_time_remaining` | 60 | 至少剩 60s |
| Inertia / Candle / Volume | 5 / 1 / 1 | 滿分 7 |
| 訊號 | score≥4 中等；≥5 強 | 實作另有 confidence 門檻 |
| Flat skip | \|move\| ≤ 0.05% | 跳過 |
| 作者回測 | 996 窗、390 訊號、宣稱 91.5% | **僅供參考，勿當保證** |

**注意：README 與程式不完全一致**

- README：簡潔 3-factor、score≥4/5
- `bot.py` 另有 ADX / vol filter 設定，但主路徑是 inertia+candle+volume；並用 `model_prob - market_prob > 0.10` 當 edge 過濾
- **Go 移植以 `bot.py` 實際 `analyze_market()` 為準**，README 數字當 config 預設註解

**市場發現**

- `GET https://gamma-api.polymarket.com/markets?slug=btc-updown-5m-{ts}`
- CLOB price：`/price?token_id=&side=buy`

### 2.3 Polymarket 5m 市場事實（2026-08 核對）

| 項目 | 值 |
| ------ | ----- |
| Slug | `btc-updown-5m-{floor(now/300)*300}`（ETH 同理 `eth-updown-5m-...`） |
| Outcomes | `["Up","Down"]` → `clobTokenIds[0]=Up`, `[1]=Down`（仍以 `outcomes` 陣列為準） |
| 結算規則 | Chainlink **TWAP** ≥ 區間開盤 TWAP → Up，否則 Down。**5m 以 30s TWAP 為主**（2026-08-07 cutover）；15m/更長常見 60s。下單前讀該 market 的 `resolutionSource`，不要寫死 60s |
| RTDS TWAP | `wss://ws-live-data.polymarket.com`；topic `crypto_prices_twap_thirty` / `crypto_prices_twap_sixty`；filter `{"symbol":"btc/usd"}`；app-level `PING` 每 5s |
| RTDS spot | `crypto_prices_chainlink` 是 **spot**，**不是**結算源；provisional 不要訂這個 |
| `orderMinSize` | **5**（Gamma；CLOB book 亦回 `min_order_size: "5"`） |
| `orderPriceMinTickSize` | 常見 **0.001**（crypto 5m；下單前以 book / `getClobMarketInfo` 為準） |
| Taker fee | `fee = C × feeRate × p × (1-p)`；**feeRate 從該 market 讀**（`fd.r`；crypto 常見 0.07，禁止當常數寫死）。maker = 0。費用在撮合時由 operator 套，不進簽名 |
| 撮合延遲 | 部分 market 有 `secondsDelay`；回 `status=delayed` 時視為 pending，**禁止當失敗重送** |
| Live 認證 | L1 EIP-712 derive API key → L2 HMAC（V2 **沒改**）；chain **137** Polygon |
| CLOB | **V2 only**（2026-04-28）。V1 SDK / V1 簽名單 production 拒收 |
| 抵押 | **pUSD**（不是 USDC.e）。live 帳戶需已 wrap；bot 不負責 onramp |
| 簽名 | V2 EIP-712 domain version `"2"`；欄位 `timestamp, metadata, builder`（無 `nonce` / `feeRateBps` / `taker`）。`go-order-utils` 當 V1，**不要用** |
| 錢包 | `signatureType` 依帳號：EOA / proxy / Safe / deposit wallet；`POLY_FUNDER` = maker/proxy |

**兩個 repo 都沒有可用的官方結算寫入 DB 流程** → 必須自建 resolver（見 §6）。

---

## 3. 高層架構

```
                    ┌──────────────────┐
                    │   config.yaml    │
                    │  + env secrets   │
                    └────────┬─────────┘
                             │
                     ┌───────▼────────┐
                     │   cmd/bot      │
                     │  mode/strategy │
                     │  max_windows   │
                     └───────┬────────┘
           ┌─────────────────┼─────────────────┐
           ▼                 ▼                 ▼
    ┌────────────┐   ┌──────────────┐   ┌─────────────┐
    │ MarketData │   │  Strategies  │   │  Executor   │
    │ Gamma/CLOB │   │ window_delta │   │ dry/paper/  │
    │ Binance    │   │ inertia_3m   │   │ live CLOB V2│
    │ RTDS TWAP  │   └──────┬───────┘   └──────┬──────┘
    └─────┬──────┘          │                  │
          │          signals/orders            │
          ▼                 ▼                  ▼
    ┌─────────────────────────────────────────────┐
    │              Store (database/sql)           │
    │   postgres (default, pool) | duckdb (embedded, conn=1) │
    └──────────────────────┬──────────────────────┘
                           │
                    ┌──────▼───────┐
                    │  Resolver    │
                    │ official Γ   │
                    │ + TWAP prov. │
                    └──────────────┘
```

### 建議目錄

```
.
├── prompt.md
├── plan.md
├── README.md
├── config.example.yaml
├── go.mod
├── cmd/
│   └── bot/main.go
├── internal/
│   ├── config/
│   ├── market/          # gamma, clob read, binance, rtds twap, clock
│   ├── strategy/
│   │   ├── strategy.go  # interface
│   │   ├── windowdelta/
│   │   └── inertia3m/
│   ├── exec/            # dry_run / paper / live (V2)
│   ├── resolve/         # official gamma + provisional twap
│   ├── store/           # Store interface；pg 預設；duckdb 臨時庫單寫者
│   └── domain/          # shared types
├── sql/
│   ├── postgres/
│   │   ├── 001_schema.sql
│   │   └── 002_analytics_views.sql
│   └── duckdb/
│       ├── 001_schema.sql
│       └── 002_analytics_views.sql
└── scripts/
    └── dryrun.sh
```

---

## 4. Config 設計

`config.example.yaml`（所有策略門檻可調）：

```yaml
mode: dry_run          # dry_run | paper | live
strategy: window_delta # window_delta | inertia_3m
max_windows: 1         # 0 = 無限；dry-run 預設 1（跑完當窗 + resolve 後退出）

assets:
  - BTC
  # - ETH              # inertia_3m 原版僅 BTC；開 ETH 僅建議 window_delta

database:
  driver: postgres     # postgres | duckdb；dry-run / live 預設 postgres
  dsn: "postgres://user:pass@localhost:5432/polymarket?sslmode=disable"
  # driver: duckdb     # 內建臨時庫；沒有 PG 或一次性實驗才用
  # dsn: "data/bot.duckdb"
  # duckdb: MaxOpenConns=1 + 程序內 write queue（硬性）

polymarket:
  gamma_url: "https://gamma-api.polymarket.com"
  clob_url: "https://clob.polymarket.com"   # CLOB V2 production
  rtds_url: "wss://ws-live-data.polymarket.com"
  chain_id: 137
  # secrets from env:
  # POLY_PRIVATE_KEY, POLY_FUNDER (maker/proxy), optional L2 API creds
  # live 另需帳戶已有 pUSD；bot 不 wrap

binance:
  rest_url: "https://api.binance.com"
  symbols:
    BTC: BTCUSDT
    ETH: ETHUSDT

clock:
  sync: clob           # clob | binance | local（local 僅 debug）
  resync_interval_sec: 30

execution:
  use_min_shares_only: true
  budget_usdc_cap: 20          # min shares * price 超過則 skip
  order_type: FOK              # dry 模擬 / live FOK 或 FAK
  price_offset: 0.01           # taker 滑價（再 clamp 到 tick）
  poll_book_before_order: true
  # fee_rate 不寫死；下單前讀 market fd.r

strategies:
  window_delta:
    wake_before_sec: 65
    entry_seconds_min: 10
    entry_seconds_max: 50
    price_min: { BTC: 0.94, ETH: 0.92 }
    price_max: 0.99
    delta_skip: 0.0005
    delta_weak: 0.001
    delta_strong: 0.002
    min_confidence: 0.30
    atr_periods: 5
    atr_multiplier: 1.5
    poll_interval_sec: 3

  inertia_3m:
    entry_delay_sec: 170
    min_time_remaining: 60
    flat_threshold_pct: 0.05
    strong_move_pct: 0.10
    min_score_moderate: 4
    min_score_strong: 5
    min_edge: 0.10             # model_prob - market_prob
    poll_interval_sec: 10

resolver:
  enabled: true
  poll_after_close_sec: 30
  poll_interval_sec: 15
  poll_timeout_sec: 900
  store_provisional_twap: true
  twap_topic: auto             # auto = 依 market resolutionSource；5m 預設 thirty
```

**CLI**

```bash
go run ./cmd/bot -config config.yaml
go run ./cmd/bot -config config.yaml -mode dry_run -strategy window_delta
go run ./cmd/bot -config config.yaml -mode dry_run -strategy inertia_3m
go run ./cmd/bot -config config.yaml -mode dry_run -max-windows 3
# 確認 DB 數據後（且已 pUSD + V2 錢包）：
go run ./cmd/bot -config config.yaml -mode live   # 需明確 live + key
```

Live 必須同時：`mode=live` **且** `POLY_PRIVATE_KEY` + `POLY_FUNDER` 齊全；否則拒絕啟動。缺 pUSD / 簽不出 V2 order 也拒絕。

v1 可把 `paper` ≡ `dry_run`。config 保留三值，程式視為同一 executor。

---

## 5. Strategy 介面（共用）

```go
type Strategy interface {
    ID() string           // "window_delta"
    Source() string       // "jmazzini/5m-poly-bot"
    Evaluate(ctx context.Context, snap MarketSnapshot) (*Signal, error)
}

type Signal struct {
    StrategyID     string
    StrategySource string
    Asset          string
    MarketSlug     string
    ConditionID    string
    Side           string   // Up | Down
    TokenID        string
    Confidence     float64
    Score          float64
    Features       map[string]any
    Reason         string
    PMPrice        float64
    BinancePrice   float64
    WindowOpenPx   float64
    SecondsLeft    float64
}
```

`Evaluate` 只做「現在能不能下」。睡多久、何時醒由 `WindowScheduler` 管（兩策略觸發點不同）。

主迴圈：

1. 對齊 5m window（用 **server time**）→ 拉/快取 market metadata（含 feeRate、tick、min size、secondsDelay、resolutionSource）
2. 更新 Binance + RTDS **TWAP**
3. `strategy.Evaluate`
4. 有 signal 且該 `(run_id, market_slug)` 尚無單 → `executor.BuyMinSize`
5. 寫 `signals` / `orders` / `fills`（reject 只在原因變化或終態寫 `rejected_signals`，不要每 3s 灌一筆）
6. window close 後 resolver 補 `resolutions` + 回填 `trades.pnl`
7. 若已跑滿 `max_windows` 且該窗 official 或 timeout → 印 summary、結束 process

---

## 6. Resolver（自建，兩個 repo 都缺）

### 6.1 為什麼需要

- Crypto 5m **沒有** 即時公開 PTB REST（equity 才有 `/api/equity/price-to-beat`）
- 結算源是 Chainlink **TWAP**，不是 Binance spot，也不是 Chainlink spot
- Gamma `closed` + `outcomePrices` 常在 close 後數分鐘才變成權威結果
- dry-run 分析必須有 **官方 outcome** 才能算真實 win rate

### 6.2 雙層結果

| 層級 | 來源 | 用途 |
|------|------|------|
| `provisional` | RTDS TWAP（`crypto_prices_twap_thirty` 或 `_sixty`，跟該 market 走） | 快速標記、對照偏差 |
| `official` | Gamma `markets?slug=` 在 `closed=true` 後讀 `outcomePrices` | **分析與 PnL 唯一權威** |

**Official 判定**

```
outcomes = ["Up","Down"]
outcomePrices ≈ ["1","0"] → winner Up
outcomePrices ≈ ["0","1"] → winner Down
```

輪詢至 `closed==true` 且 prices 接近 0/1，或 timeout 標 `pending`。timeout 的窗 **不算進 win_rate**（view 要 filter）。

**Provisional**

- 訂閱對應 TWAP topic（5m 預設 `crypto_prices_twap_thirty`；若 `resolutionSource` 寫 60s 則改 sixty）
- window open 的 TWAP → `price_to_beat_local`
- window end 的 TWAP → `final_price_local`
- `final >= open → Up else Down`
- **禁止**用 `crypto_prices_chainlink` spot 或 Binance 當 provisional
- 本地 TWAP 仍可能與官方不一致（取樣邊界）；只當輔助，**不覆蓋** official，也不回填 `pnl_ledger`

### 6.3 不做的事（v1）

- 不上 Playwright 刮前端
- 不依賴第三方付費 scraper
- 不自己重算 Chainlink TWAP（官方說不要 reproduce 權重/缺值行為）

---

## 7. Database schema（分析優先）

原則：

- 同一套**邏輯表名/欄位**；Postgres / DuckDB 各一份 DDL（型別方言不同）
- 每筆交易相關列都帶：`run_id`, `run_mode`, `strategy_id`, `strategy_source`
- 金額：Postgres `NUMERIC`；DuckDB 也用 `DECIMAL`（不要 `DOUBLE` 存錢）
- 時間一律 UTC：PG `TIMESTAMPTZ`；DuckDB `TIMESTAMP`（約定 UTC）

### 7.1 表

| 表 | 用途 |
| ---- | ------ |
| `runs` | 一次行程：mode、strategy、config 快照、git/version、`max_windows` |
| `markets` | slug、condition_id、tokens、window_start/end、tick、min_size、fee_rate、seconds_delay、resolution_source |
| `price_ticks` | Binance / PM mid / **TWAP** 抽樣（可降採樣） |
| `signals` | 每次**終態** Evaluate（下單或 skip） |
| `rejected_signals` | 過濾原因（price、delta、ATR…）— **原因變化才 insert**，禁止每 poll 一列 |
| `orders` | 意圖單：dry/live、side、price、size、status |
| `fills` | 成交（dry 模擬；live 對應 CLOB trade） |
| `resolutions` | provisional TWAP + official winner、raw JSON |
| `positions` | 依 market+run 彙總 |
| `pnl_ledger` | **僅 official 結算後**實現損益 |

### 7.2 核心欄位（摘要）

**runs**

- `id`, `started_at`, `ended_at`, `run_mode`, `strategy_id`, `strategy_source`, `config_json`, `hostname`, `git_sha`, `max_windows`

**markets**

- `slug` PK, `asset`, `condition_id`, `window_start`, `window_end`, `up_token_id`, `down_token_id`, `min_order_size`, `tick_size`, `fee_rate`, `seconds_delay`, `resolution_source`

**orders**

- `id`, `run_id`, `strategy_id`, `strategy_source`, `run_mode`, `market_slug`, `side`, `token_id`
- `intended_price`, `intended_shares`, `limit_price`, `notional_usdc`
- `is_min_size`, `status` (`simulated`|`submitted`|`matched`|`delayed`|`rejected`|`canceled`)
- `clob_order_id`, `request_json`, `response_json`, `created_at`
- **`UNIQUE (run_id, market_slug)`** — 同一 run 同一窗只能一張意圖單

**fills**

- `fill_model`：`book_walk`（dry）| `clob_fill`（live）
- `fee_usdc`, `fee_rate`, `liquidity_flag`

**resolutions**

- `market_slug`, `source` (`official_gamma`|`provisional_twap`)
- `winner`, `price_to_beat`, `final_price`, `resolved_at`, `raw_json`
- official 與 provisional 可共存；PnL 只 join official

**pnl_ledger**

- `order_id`/`fill_id`, `run_mode`, `strategy_id`, `market_slug`
- `shares`, `entry_price`, `exit_value` (win→1, lose→0), `fee_usdc`, `pnl_usdc`, `is_win`
- `fill_model`（分析時 **分開** 看 dry vs live，禁止混算決定是否 live）

### 7.3 分析 view（兩邊 SQL 都給）

- `v_strategy_performance`：按 `strategy_id, run_mode, fill_model` 的 n、win_rate、avg_pnl、期望值；排除 `resolutions` pending
- `v_signal_funnel`：reject 原因分布
- `v_calibration`：confidence bucket vs 實際勝率
- `v_timing`：seconds_left / entry delay vs win rate
- `v_provisional_vs_official`：本地 TWAP 與官方不一致率

### 7.4 方言差異（DDL 時注意）

| | PostgreSQL | DuckDB |
| -- | ------------ | -------- |
| Driver | `github.com/jackc/pgx/v5/stdlib` | `github.com/duckdb/duckdb-go/v2` |
| PK | `BIGSERIAL` / `UUID` | `BIGINT` + sequence 或應用層 id |
| JSON | `JSONB` | `JSON` |
| Time | `TIMESTAMPTZ` | `TIMESTAMP`（UTC） |
| Money | `NUMERIC` | `DECIMAL` |
| Upsert | `ON CONFLICT` | `ON CONFLICT` |
| 連線 | pool OK | **`MaxOpenConns = 1`** |
| 遷移 | 啟動時跑 `sql/postgres` | 啟動時跑 `sql/duckdb` |

Store 介面只用標準 SQL + 少量 dialect 檔（placeholder 用已寫死的語句 map）。v1 手寫 `store` + 嵌入 SQL，不引 ORM。

先寫一份邏輯 schema，再複製成兩份方言。**v1 dry-run 跑 Postgres**；DuckDB SQL 一併給，但不是預設路徑。

### 7.5 DuckDB 寫入（僅 `driver: duckdb` 時）

- `sql.DB.SetMaxOpenConns(1)`
- 所有 INSERT/UPDATE 走**一條** write queue（ticks / signals / orders / resolver 不得並行寫）
- 分析時另開 **read-only** 連線；寫入中的檔不要第二個 writer
- 踩過：`MaxOpenConns > 1` 會 WAL/checkpoint FATAL（duckdb-go#127）

---

## 8. Execution 層

### 8.1 三種 mode

| Mode | 行情 | 下單 | DB |
| ------ | ------ | ------ | ----- |
| `dry_run` | 真實 | 不簽名、不送 CLOB；walk book 模擬 fill | 完整寫入，`run_mode=dry_run`，`fill_model=book_walk` |
| `paper` | 真實 | v1 ≡ dry_run | `paper`（同一 executor） |
| `live` | 真實 | **CLOB V2** 簽單 + L2 POST `/order` | `live` + 真實 order id，`fill_model=clob_fill` |

### 8.2 最小下單單位（硬需求）

```
book = GET /book?token_id=...
info = CLOB market info          // mts, mos, fd.r, secondsDelay
minShares = parse(book.min_order_size)   // 通常 5
tick     = parse(book.tick_size)
feeRate  = info.fd.r             // 不要用 0.07 常數

shares = minShares
limitPrice = round_to_tick(mid + offset, tick)
cost = shares * limitPrice
if cost > budget_usdc_cap: skip + rejected_signals
if already exists order(run_id, market_slug): skip
```

Dry-run 模擬 fill：

- 吃 ask 直到 shares 滿（簡單 walk book）
- 估算 taker fee：`shares * feeRate * p * (1-p)`（p = 實際 walk 到的價）
- 寫入 `fills`（`liquidity_flag=taker`, `fill_model=book_walk`）
- **這不是實盤**。尾盤 5 shares FOK 常因深度不足失敗；win rate 不可直接拿去開 live

Live：

- FOK 失敗（沒吃到）→ `rejected`，同一窗不重送
- `status=delayed` → 存單、poll 結果，**不重送**
- 成交後用 CLOB trade / Data API 對帳，不要用模擬 fill 覆寫

### 8.3 Live（CLOB V2，最後做）

- 簽名：自寫 V2 EIP-712 REST，或**已確認支援 V2** 的社群 Go client。**不用** `Polymarket/go-order-utils`（V1）
- Order typed data：`salt, maker, signer, tokenId, makerAmount, takerAmount, side, signatureType, timestamp, metadata, builder`；domain version `"2"`
- Env：`POLY_PRIVATE_KEY`、`POLY_FUNDER`；啟動時 derive/create L2 creds
- 啟動檢查：能簽 V2、funder 有 pUSD、allowance OK；否則 exit
- 私鑰不進 DB（只存 address）
- `orderType` 在 POST body（不進 EIP-712）：FOK / FAK
- Phase 5 不估「1 天」；等 Phase 0–4 的 official PnL 再說

---

## 9. 主迴圈時序

### 9.1 時鐘

啟動與每隔 `resync_interval_sec` 拉 CLOB 或 Binance server time，存 offset。所有 `close_ts`、`seconds_left`、sleep 用 `now = local + offset`。`clock.sync=local` 僅 debug。

### 9.2 window_delta

```
windows = 0
loop:
  close = next_5m_boundary(server_now)
  sleep until close-65s
  while now < close:
    fetch markets (BTC/ETH) + binance + clob mid
    if 10s <= left <= 50s: try enter once per slug  # UNIQUE 擋第二單
    sleep 3s
  enqueue resolve(slug)
  windows += 1
  if max_windows > 0 && windows >= max_windows:
    wait resolver (official or timeout) → summary → exit
```

### 9.3 inertia_3m

```
windows = 0
loop:
  find active btc-updown-5m slug
  if elapsed < 170s: sleep remaining
  if remaining < 60s: skip → next window
  analyze 1m klines vs window open
  if signal & edge: buy min shares   # UNIQUE 擋第二單
  wait window end → resolve
  windows += 1
  if max_windows > 0 && windows >= max_windows:
    wait resolver → summary → exit
```

兩策略共用 `WindowScheduler`，差在 `Evaluate` 觸發條件。

---

## 10. 實作階段

### Phase 0 — 骨架（0.5d）

- [ ] `go mod init`、config load、logging
- [ ] `sql/postgres` + `sql/duckdb` schema + views（含 UNIQUE、`fill_model`、`fee_rate`）
- [ ] Store open + migrate；預設 Postgres pool；若選 DuckDB 則 `MaxOpenConns=1` + write queue
- [ ] `runs` 插入與 graceful shutdown 更新

### Phase 1 — 行情（1d）

- [ ] Gamma: event/market by slug
- [ ] CLOB: midpoint、price、book、market info（min/tick/fee/delay）
- [ ] Binance: ticker + klines 1m/5m
- [ ] Clock offset（CLOB 或 Binance）
- [ ] RTDS **TWAP** 訂閱 + PING 5s + 重連（可選但 provisional 需要）
- [ ] 寫 `markets` / 抽樣 `price_ticks`

### Phase 2 — 策略移植（1–1.5d）

- [ ] `windowdelta`：對齊 `crypto_bot.py` 參數與 score
- [ ] `inertia3m`：對齊 `analyze_market()` + edge 過濾
- [ ] unit test：固定 klines fixture → 分數/方向金樣
- [ ] rejected 原因碼 enum；只在原因變化落庫

### Phase 3 — Executor + dry-run（1d）

- [ ] min-share sizing
- [ ] book walk 模擬 fill + **market feeRate**
- [ ] `UNIQUE (run_id, market_slug)` 擋雙開
- [ ] `max_windows=1` 端到端：一窗 + resolve 後 process 結束
- [ ] 驗證 DB：signal → order → fill 鏈完整，`fill_model=book_walk`

### Phase 4 — Resolver（0.5–1d）

- [ ] close 後 poll Gamma official
- [ ] provisional **TWAP**（若 RTDS 有開）
- [ ] 回填 `pnl_ledger`（只 official）、`positions`
- [ ] 印 session summary + SQL 查 win rate（排除 pending）

### Phase 5 — Live 路徑（最後做，不排進 v1 時程）

- [ ] CLOB **V2** 簽名 + POST `/order`
- [ ] pUSD / signatureType / funder 啟動檢查
- [ ] `delayed` 不重送；小額 1 筆對帳 Data API / CLOB trades
- [ ] 安全開關：`mode=live` + env 雙重確認

### Phase 6 — 文件與交付

- [ ] README：安裝、`max_windows=1` dry-run、切 DB、查 view
- [ ] `config.example.yaml`
- [ ] 免責聲明（非投資建議）
- [ ] 註明 dry `book_walk` ≠ live fill

---

## 11. 驗收標準

1. `mode=dry_run` + **Postgres** + `max_windows=1` 可跑完一個 5m window，等到 official 或 timeout 後 **process 退出**，無需 private key
2. 同一 code path 改 `driver=duckdb` 可跑（schema 已 apply；內建臨時庫）
3. `strategy=window_delta` 與 `inertia_3m` 皆可切換
4. DB 內每筆 order 可見 `strategy_source` ∈  
   `jmazzini/5m-poly-bot` | `ratrimaa/btc-5m-polymarket-bot`
5. `orders.intended_shares >= markets.min_order_size`（預設等於 min）
6. 同一 `run_id`+`market_slug` 不能插入第二張 order
7. Resolver 在 timeout 前寫入 `official` winner，或明確 `pending`；`pnl_ledger` 不含 pending
8. dry fill 的 `fill_model=book_walk`；可查：

   ```sql
   SELECT strategy_id, run_mode, fill_model,
          count(*) AS n,
          avg(CASE WHEN is_win THEN 1.0 ELSE 0.0 END) AS win_rate,
          sum(pnl_usdc) AS pnl
   FROM pnl_ledger
   GROUP BY 1,2,3;
   ```

9. Live 未配置 key / 非 V2 / 無 pUSD 時 process **拒絕**啟動
10. 若 `driver=duckdb`：運行期間第二個 writer 不出現；`MaxOpenConns=1`

---

## 12. 風險與誠實邊界

- 公開 91.5% 回測 **不可外推**；Binance 方向 ≠ Chainlink TWAP 結算。
- 尾盤高價（0.94–0.99）勝率高但 **賠率差**；fee + 滑價後 EV 可能為負。以 p=0.96、5 shares 粗算，未計滑價 breakeven WR 約 96%。
- Taker fee 在 p≈0.5 最痛；尾盤 p→1 fee 變小，但 edge 也被定價掉。feeRate 以市場為準。
- 延遲：Go 優於 Python，但仍可能在 10s 內搶不到好價；`secondsDelay` 會讓 FOK 變 delayed。
- dry `book_walk` 勝率 **不能**當 live 決策；要分開看 `fill_model`。
- 兩策略可能高度相關（都吃短期動量）→ 分析時分開看，勿重複加碼。
- 私鑰與 funder 只走 env；永不入 git / DB。
- 遵守 Polymarket ToS 與當地法規。

---

## 13. 依賴建議（盡量少）

| 用途 | 套件 |
| ------ | ------ |
| YAML config | `gopkg.in/yaml.v3` |
| Postgres | `github.com/jackc/pgx/v5/stdlib` |
| DuckDB | `github.com/duckdb/duckdb-go/v2` |
| WS | `nhooyr.io/websocket` 或 `github.com/coder/websocket` |
| Decimal | `github.com/shopspring/decimal` |
| Live 簽名 | **V2 EIP-712 自寫** 或已確認 V2 的社群 Go CLOB client。不用 `go-order-utils` |
| ETH account | live 才加；選最小能簽 V2 的庫，不要為了 dry-run 拉 `go-ethereum` |

HTTP：stdlib `net/http` 足夠。

---

## 14. 參考連結

### 策略原始碼

- <https://github.com/jmazzini/5m-poly-bot>
- <https://github.com/ratrimaa/btc-5m-polymarket-bot>

### Polymarket 官方

- Docs index: <https://docs.polymarket.com/llms.txt>
- Discover markets: <https://docs.polymarket.com/market-data/discover-markets>
- Place orders: <https://docs.polymarket.com/trading/orders/create>
- Fees: <https://docs.polymarket.com/trading/fees>
- RTDS: <https://docs.polymarket.com/market-data/websocket/rtds>
- Chainlink TWAP: <https://docs.polymarket.com/market-data/chainlink-twap>
- CLOB V2 migration: <https://docs.polymarket.com/v2-migration>

### 市場 / 結算補充

- 5m API 實務：<https://polymarkets.co.il/en/api/polymarket-crypto-5min-markets/>
- PTB/outcome 缺口說明：<https://dev.to/bluewhale-quant-lab/how-to-read-a-polymarket-updown-outcome-and-the-price-to-beat-before-the-oracle-settles-3kcj>

### DB

- DuckDB Go: <https://duckdb.org/docs/current/clients/go>
- DuckDB concurrency: <https://duckdb.org/docs/current/connect/concurrency.html>
- pgx: <https://github.com/jackc/pgx>

### 實測快照（撰寫 plan 時）

- 例：`btc-updown-5m-1787074800`
- `orderMinSize: 5`, `orderPriceMinTickSize: 0.001`
- `resolutionSource`: 以該 market 為準（5m 多為 30s TWAP，不要抄 60s URL）
- outcomes: Up/Down + 對應 `clobTokenIds`

---

## 15. 建議下一步（實作時）

1. 先落地 **Phase 0–4 + Postgres + `max_windows=1` dry-run**（零金流；數據進 PostgreSQL）
2. 跑足夠 windows 後用 `v_strategy_performance`（**按 fill_model 拆**）決定要不要碰 live
3. DuckDB 留給沒有 PG 的環境；live 仍是 **CLOB V2** 小額

---

*本文件對應 `prompt.md`；實作完成後應以實際 config 預設與 schema 回寫 README，並在偏離原 repo 邏輯處加註 `strategy_source` 與 diff 說明。*
