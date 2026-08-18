# Plan: Go Polymarket 5m Bot (dual strategy + dual DB)

> 來源需求：`prompt.md`  
> 狀態：僅規劃，尚未實作  
> 目標：把兩個公開 Python 策略包成**一個 Go bot**，支援 **dry-run → live**、**PostgreSQL / DuckDB** 可切換，並把完整交易與結算資料寫入 DB 供分析。

---

## 1. 目標與非目標

### 1.1 必須做到

| # | 需求 | 作法 |
|---|------|------|
| 1 | 改寫兩個 GitHub 策略邏輯 | 各自獨立 strategy package，共用行情 / 下單 / DB |
| 2 | 單一 Go 程式 | `cmd/bot` 一個 binary，config 選 strategy / mode / DB |
| 3 | dry-run 一輪後再決定 live | `mode: dry_run \| paper \| live`，預設 `dry_run` |
| 4 | PostgreSQL **與** DuckDB | `database/sql` + driver 抽象；config 切換 |
| 5 | 完整 SQL 參考檔 | `sql/postgres/*.sql` + `sql/duckdb/*.sql` |
| 6 | 官方結果 resolver | 兩個 repo 都沒有可靠 resolver → 自建 |
| 7 | schema 能做分析，不只交易 | 行情快照、訊號、下單、成交、結算、PnL 分表 |
| 8 | dry-run / live 都完整落庫 | 每筆標 `run_mode` + `strategy_id` + `strategy_source` |
| 9 | 每筆以最小可買單位下單 | 讀 CLOB book `min_order_size`（目前常見 **5 shares**） |

### 1.2 非目標（v1 不做）

- 多策略同時對同一 market 下單（v1 一次只跑一個 strategy；可並行第二 process）
- Web UI / dashboard
- 自動 Kelly 大倉、多資產任意擴充（先 BTC，ETH 可選）
- 保證獲利（公開回測 ≠ 實盤）

---

## 2. 研究摘要（已核對原始碼與 API）

### 2.1 策略 A — Window Delta + Micro Momentum + ATR

- **Repo**: [jmazzini/5m-poly-bot](https://github.com/jmazzini/5m-poly-bot)（`crypto_bot.py`）
- **strategy_id**: `window_delta`
- **strategy_source**: `jmazzini/5m-poly-bot`

| 參數 | 預設 | 說明 |
|------|------|------|
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
5. Live：`py-clob-client` BUY，size 至少 5 shares；taker price ≈ mid+0.01

**分數**

| Delta | 分 |
|-------|----|
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

---

### 2.2 策略 B — 3 分鐘動量 / Inertia 評分

- **Repo**: [ratrimaa/btc-5m-polymarket-bot](https://github.com/ratrimaa/btc-5m-polymarket-bot)（`bot.py`）
- **strategy_id**: `inertia_3m`
- **strategy_source**: `ratrimaa/btc-5m-polymarket-bot`

| 參數 | 預設 | 說明 |
|------|------|------|
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

---

### 2.3 Polymarket 5m 市場事實（2026 實測）

| 項目 | 值 |
|------|-----|
| Slug | `btc-updown-5m-{floor(now/300)*300}`（ETH 同理 `eth-updown-5m-...`） |
| Outcomes | `["Up","Down"]` → `clobTokenIds[0]=Up`, `[1]=Down`（仍以 `outcomes` 陣列為準） |
| 結算規則 | Chainlink **BTC/USD TWAP**；區間 TWAP ≥ 區間開盤價 → Up，否則 Down |
| resolutionSource 例 | `https://data.chain.link/streams/btc-usd-twap-60s-streams` |
| `orderMinSize` | **5**（Gamma 欄位；CLOB book 亦回 `min_order_size: "5"`） |
| `orderPriceMinTickSize` | 常見 **0.001**（crypto 5m；下單前以 book/Gamma 為準） |
| 公開 API | Gamma 發現、CLOB 簿/下單、Data API 持倉、RTDS 即時價 |
| RTDS | `wss://ws-live-data.polymarket.com`；Chainlink symbol `btc/usd`；需 app-level `PING` |
| Taker fee（crypto） | `fee = shares × 0.07 × p × (1-p)`；maker = 0 |
| Live 認證 | L1 EIP-712 derive API key → L2 HMAC；chain_id **137** Polygon |
| 官方 Go | [`Polymarket/go-order-utils`](https://github.com/Polymarket/go-order-utils) 簽名；完整 CLOB client 多用社群 SDK 或自寫 REST |

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
                     └───────┬────────┘
           ┌─────────────────┼─────────────────┐
           ▼                 ▼                 ▼
    ┌────────────┐   ┌──────────────┐   ┌─────────────┐
    │ MarketData │   │  Strategies  │   │  Executor   │
    │ Gamma/CLOB │   │ window_delta │   │ dry/paper/  │
    │ Binance    │   │ inertia_3m   │   │ live CLOB   │
    │ RTDS CL    │   └──────┬───────┘   └──────┬──────┘
    └─────┬──────┘          │                  │
          │          signals/orders            │
          ▼                 ▼                  ▼
    ┌─────────────────────────────────────────────┐
    │              Store (database/sql)           │
    │         postgres  |  duckdb  (config)       │
    └──────────────────────┬──────────────────────┘
                           │
                    ┌──────▼───────┐
                    │  Resolver    │
                    │ official PM  │
                    │ + local CL   │
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
│   ├── market/          # gamma, clob read, binance, rtds
│   ├── strategy/
│   │   ├── strategy.go  # interface
│   │   ├── windowdelta/
│   │   └── inertia3m/
│   ├── exec/            # dry_run / paper / live
│   ├── resolve/         # official + provisional
│   ├── store/           # Store interface + pg + duckdb
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

assets:
  - BTC
  # - ETH              # inertia_3m 原版僅 BTC；開 ETH 僅建議 window_delta

database:
  driver: duckdb       # duckdb | postgres
  dsn: "data/bot.duckdb"
  # dsn: "postgres://user:pass@localhost:5432/polymarket?sslmode=disable"

polymarket:
  gamma_url: "https://gamma-api.polymarket.com"
  clob_url: "https://clob.polymarket.com"
  rtds_url: "wss://ws-live-data.polymarket.com"
  chain_id: 137
  # secrets from env:
  # POLY_PRIVATE_KEY, POLY_FUNDER (proxy wallet), optional API creds

binance:
  rest_url: "https://api.binance.com"
  symbols:
    BTC: BTCUSDT
    ETH: ETHUSDT

execution:
  # 每筆以最小可買單位：shares = max(min_order_size, ceil(budget/price))
  # v1 預設直接打 min_order_size（通常 5）
  use_min_shares_only: true
  budget_usdc_cap: 20          # 安全上限；min shares * price 不得超過此值則 skip
  order_type: FOK              # dry 模擬 / live 建議 FOK 或 FAK
  price_offset: 0.01           # taker 滑價（再 clamp 到 tick）
  poll_book_before_order: true

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
  poll_timeout_sec: 900        # Gamma 官方結算可能數分鐘
  store_provisional_chainlink: true
```

**CLI**

```bash
go run ./cmd/bot -config config.yaml
go run ./cmd/bot -config config.yaml -mode dry_run -strategy window_delta
go run ./cmd/bot -config config.yaml -mode dry_run -strategy inertia_3m
# 確認 DB 數據後：
go run ./cmd/bot -config config.yaml -mode live   # 需明確 live + key
```

Live 必須同時：`mode=live` **且** 環境變數齊全；否則拒絕啟動。

---

## 5. Strategy 介面（共用）

```go
type Strategy interface {
    ID() string           // "window_delta"
    Source() string       // "jmazzini/5m-poly-bot"
    // 回傳是否該睡、何時再醒、或產生 Signal
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
    Features       map[string]any // delta, atr, inertia... 原樣進 DB JSON
    Reason         string
    PMPrice        float64
    BinancePrice   float64
    WindowOpenPx   float64
    SecondsLeft    float64
}
```

主迴圈：

1. 對齊 5m window → 拉/快取 market metadata  
2. 更新 Binance +（可選）Chainlink RTDS  
3. `strategy.Evaluate`  
4. 有 signal → `executor.BuyMinSize`  
5. 寫 `signals` / `orders` / `fills`  
6. window close 後 resolver 補 `resolutions` + 回填 `trades.pnl`

---

## 6. Resolver（自建，兩個 repo 都缺）

### 6.1 為什麼需要

- Crypto 5m **沒有** 即時公開 PTB REST（equity 才有 `/api/equity/price-to-beat`）
- 結算源是 Chainlink TWAP，不是 Binance spot
- Gamma `closed` + `outcomePrices` 常在 close 後數分鐘才變成權威結果
- dry-run 分析必須有 **官方 outcome** 才能算真實 win rate

### 6.2 雙層結果

| 層級 | 來源 | 用途 |
|------|------|------|
| `provisional` | 本地記錄的 Chainlink RTDS / 開收盤近似 | 快速標記、對照偏差 |
| `official` | Gamma `markets?slug=` 在 `closed=true` 後讀 `outcomePrices` | **分析與 PnL 唯一權威** |

**Official 判定**

```
outcomes = ["Up","Down"]
outcomePrices ≈ ["1","0"] → winner Up
outcomePrices ≈ ["0","1"] → winner Down
```

輪詢至 `closed==true` 且 prices 接近 0/1，或 timeout 標 `pending`。

**Provisional（可選）**

- 訂閱 RTDS Chainlink `btc/usd`（及 eth）
- window open 時刻快照 → `price_to_beat_local`
- window end 時刻 / 短 TWAP → `final_price_local`
- `final >= open → Up else Down`
- **注意**：官方是 Chainlink **TWAP stream**，本地 spot/簡易 TWAP 可能與官方不一致；只當輔助，不覆蓋 official。

### 6.3 不做的事（v1）

- 不上 Playwright 刮前端（脆、重）
- 不依賴第三方付費 scraper

---

## 7. Database schema（分析優先）

原則：

- 同一套**邏輯表名/欄位**；Postgres / DuckDB 各一份 DDL（型別方言不同）
- 每筆交易相關列都帶：`run_id`, `run_mode`, `strategy_id`, `strategy_source`
- 金額用 `DECIMAL` / `DOUBLE`（DuckDB）；時間一律 UTC `TIMESTAMPTZ`（PG）或 `TIMESTAMP`（DuckDB，約定存 UTC）

### 7.1 表

| 表 | 用途 |
|----|------|
| `runs` | 一次行程：mode、strategy、config 快照、git/version |
| `markets` | slug、condition_id、tokens、window_start/end、tick、min_size |
| `price_ticks` | Binance / PM mid / Chainlink 抽樣（分析用，可降採樣） |
| `signals` | 每次 Evaluate 產出（含 skip 原因可寫 `rejected_signals`） |
| `rejected_signals` | 過濾掉的原因（price、delta、ATR…）— 回測門檻必備 |
| `orders` | 意圖單：dry/live、side、price、size、status |
| `fills` | 成交（dry 模擬一筆 fill；live 對應 CLOB trade） |
| `resolutions` | provisional + official winner、raw JSON |
| `positions` | 依 market+run 彙總 |
| `pnl_ledger` | 結算後實現損益 |

### 7.2 核心欄位（摘要）

**runs**

- `id`, `started_at`, `ended_at`, `run_mode`, `strategy_id`, `strategy_source`, `config_json`, `hostname`, `git_sha`

**markets**

- `slug` PK, `asset`, `condition_id`, `window_start`, `window_end`, `up_token_id`, `down_token_id`, `min_order_size`, `tick_size`, `resolution_source`

**orders**

- `id`, `run_id`, `strategy_id`, `strategy_source`, `run_mode`, `market_slug`, `side`, `token_id`
- `intended_price`, `intended_shares`, `limit_price`, `notional_usdc`
- `is_min_size`, `status` (`simulated`|`submitted`|`matched`|`rejected`|`canceled`)
- `clob_order_id`, `request_json`, `response_json`, `created_at`

**resolutions**

- `market_slug`, `source` (`official_gamma`|`provisional_chainlink`)
- `winner`, `price_to_beat`, `final_price`, `resolved_at`, `raw_json`

**pnl_ledger**

- `order_id`/`fill_id`, `run_mode`, `strategy_id`, `market_slug`
- `shares`, `entry_price`, `exit_value` (win→1, lose→0), `fee_usdc`, `pnl_usdc`, `is_win`

### 7.3 分析 view（兩邊 SQL 都給）

- `v_strategy_performance`：按 `strategy_id, run_mode` 的 n、win_rate、avg_pnl、期望值
- `v_signal_funnel`：reject 原因分布
- `v_calibration`：confidence bucket vs 實際勝率
- `v_timing`：seconds_left / entry delay vs win rate
- `v_provisional_vs_official`：本地 CL 與官方不一致率（驗證 resolver）

### 7.4 方言差異（DDL 時注意）

| | PostgreSQL | DuckDB |
|--|------------|--------|
| Driver | `github.com/jackc/pgx/v5/stdlib` | `github.com/duckdb/duckdb-go/v2` |
| PK | `BIGSERIAL` / `UUID` | `BIGINT` + sequence 或應用層 snowflake |
| JSON | `JSONB` | `JSON` |
| Time | `TIMESTAMPTZ` | `TIMESTAMP`（UTC） |
| Upsert | `ON CONFLICT` | `ON CONFLICT`（支援） |
| 遷移 | 啟動時跑 `sql/postgres` | 啟動時跑 `sql/duckdb` |

Store 介面只用標準 SQL + 少量 dialect 檔（placeholder `$1` vs `?`：統一用檔案內已寫死的語句 map，或 `sqlx` named rebind）。

**建議**：v1 用手寫 `store` + 嵌入 SQL 檔，不引 heavy ORM。

---

## 8. Execution 層

### 8.1 三種 mode

| Mode | 行情 | 下單 | DB |
|------|------|------|-----|
| `dry_run` | 真實 | 不算簽名、不送 CLOB；用 book 模擬 fill | 完整寫入，`run_mode=dry_run` |
| `paper` | 真實 | 同 dry（別名可合併；若要區分「含延遲模型」可之後再拆） | `paper` |
| `live` | 真實 | EIP-712 簽單 + L2 POST `/order` | `live` + 真實 order id |

v1 可把 `paper` ≡ `dry_run`（與 jmazzini 語意接近），config 保留三值以免之後分叉。

### 8.2 最小下單單位（硬需求）

```
book = GET /book?token_id=...
minShares = parse(book.min_order_size)   // 通常 5
tick     = parse(book.tick_size)

shares = minShares                       // use_min_shares_only=true
limitPrice = round_to_tick(mid + offset, tick)
cost = shares * limitPrice
if cost > budget_usdc_cap: skip + log rejected
```

Dry-run 模擬 fill：

- 吃 ask 直到 shares 滿（簡單 walk book）
- 估算 taker fee：`shares * 0.07 * p * (1-p)`
- 寫入 `fills`（`liquidity_flag=taker`）

### 8.3 Live 依賴

- 簽名：官方 `go-order-utils` 或經驗證的社群 Go CLOB client  
- Env：`POLY_PRIVATE_KEY`、`POLY_FUNDER`（proxy/funder，signature_type 依帳號）  
- 啟動時 derive/create API creds，禁止把 private key 寫進 DB（只存 address）

---

## 9. 主迴圈時序

### window_delta

```
loop:
  close = next_5m_boundary()
  sleep until close-65s
  while now < close:
    fetch markets (BTC/ETH) + binance analyze + clob mid
    if 10s <= left <= 50s: try enter once per slug
    sleep 3s
  enqueue resolve(slug)
```

### inertia_3m

```
loop:
  find active btc-updown-5m slug
  if elapsed < 170s: sleep remaining
  if remaining < 60s: skip → next window
  analyze 1m klines vs window open
  if signal & edge: buy min shares
  wait window end → resolve
```

兩策略可共用 `WindowScheduler`，差在 `Evaluate` 觸發條件。

---

## 10. 實作階段

### Phase 0 — 骨架（0.5d）

- [ ] `go mod init`、config load、logging
- [ ] `sql/postgres` + `sql/duckdb` schema + views
- [ ] Store open + migrate on boot
- [ ] `runs` 插入與 graceful shutdown 更新

### Phase 1 — 行情（1d）

- [ ] Gamma: event/market by slug
- [ ] CLOB: midpoint、price、book（min size / tick）
- [ ] Binance: ticker + klines 1m/5m
- [ ] （可選）RTDS Chainlink 訂閱 + 重連
- [ ] 寫 `markets` / 抽樣 `price_ticks`

### Phase 2 — 策略移植（1–1.5d）

- [ ] `windowdelta`：對齊 `crypto_bot.py` 參數與 score
- [ ] `inertia3m`：對齊 `analyze_market()` + edge 過濾
- [ ] unit test：固定 klines fixture → 分數/方向金樣
- [ ] rejected 原因碼 enum 落庫

### Phase 3 — Executor + dry-run（1d）

- [ ] min-share sizing
- [ ] book walk 模擬 fill + fee
- [ ] 端到端 dry-run ≥ 1 個完整 5m window
- [ ] 驗證 DB：signal → order → fill 鏈完整

### Phase 4 — Resolver（0.5–1d）

- [ ] close 後 poll Gamma official
- [ ] provisional chainlink（若 RTDS 有開）
- [ ] 回填 `pnl_ledger`、`positions`
- [ ] 印 session summary + SQL 查 win rate

### Phase 5 — Live 路徑（1d，最後做）

- [ ] L1/L2 auth
- [ ] create/post order（FOK/FAK）
- [ ] 安全開關：雙重確認 config + env
- [ ] 小額 live 1 筆後對帳 Data API / CLOB trades

### Phase 6 — 文件與交付

- [ ] README：安裝、dry-run、切 DB、查分析 view
- [ ] `config.example.yaml`
- [ ] 免責聲明（非投資建議）

---

## 11. 驗收標準

1. `mode=dry_run` + DuckDB 可跑完至少一個 5m window，無需 private key  
2. 同一 code path 改 `driver=postgres` 可跑（schema 已 apply）  
3. `strategy=window_delta` 與 `inertia_3m` 皆可切換  
4. DB 內每筆 order 可見 `strategy_source` ∈  
   `jmazzini/5m-poly-bot` | `ratrimaa/btc-5m-polymarket-bot`  
5. `orders.intended_shares >= markets.min_order_size`（預設等於 min）  
6. Resolver 在 timeout 前寫入 `official` winner，或明確 `pending`  
7. 可查：

```sql
SELECT strategy_id, run_mode,
       count(*) AS n,
       avg(CASE WHEN is_win THEN 1.0 ELSE 0.0 END) AS win_rate,
       sum(pnl_usdc) AS pnl
FROM pnl_ledger
GROUP BY 1,2;
```

8. Live 未配置 key 時 process **拒絕**啟動  

---

## 12. 風險與誠實邊界

- 公開 91.5% 回測 **不可外推**；Binance 方向 ≠ Chainlink TWAP 結算。  
- 尾盤高價（0.94–0.99）勝率高但 **賠率差**；fee + 滑價後 EV 可能為負。  
- Taker fee 在 p≈0.5 最痛；尾盤 p→1 fee 變小，但 edge 也被定價掉。  
- 延遲：Go 優於 Python，但仍可能在 10s 內搶不到好價。  
- 兩策略可能高度相關（都吃短期動量）→ 分析時分開看，勿重複加碼。  
- 私鑰與 funder 只走 env；永不入 git / DB。  
- 遵守 Polymarket ToS 與當地法規。

---

## 13. 依賴建議（盡量少）

| 用途 | 套件 |
|------|------|
| YAML config | `gopkg.in/yaml.v3` |
| Postgres | `github.com/jackc/pgx/v5/stdlib` |
| DuckDB | `github.com/duckdb/duckdb-go/v2` |
| WS | `github.com/gorilla/websocket` 或 `nhooyr.io/websocket` |
| Decimal | `github.com/shopspring/decimal` |
| CLOB 簽名 | `github.com/Polymarket/go-order-utils`（live） |
| ETH account | `github.com/ethereum/go-ethereum`（live） |

HTTP：stdlib `net/http` 足夠。

---

## 14. 參考連結

### 策略原始碼

- https://github.com/jmazzini/5m-poly-bot  
- https://github.com/ratrimaa/btc-5m-polymarket-bot  

### Polymarket 官方

- Docs index: https://docs.polymarket.com/llms.txt  
- Discover markets: https://docs.polymarket.com/market-data/discover-markets  
- Place orders: https://docs.polymarket.com/trading/orders/create  
- Fees: https://docs.polymarket.com/trading/fees  
- RTDS: https://docs.polymarket.com/market-data/websocket/rtds  
- go-order-utils: https://github.com/Polymarket/go-order-utils  

### 市場 / 結算補充

- 5m API 實務：https://polymarkets.co.il/en/api/polymarket-crypto-5min-markets/  
- PTB/outcome 缺口說明：https://dev.to/bluewhale-quant-lab/how-to-read-a-polymarket-updown-outcome-and-the-price-to-beat-before-the-oracle-settles-3kcj  
- Chainlink BTC stream（市場 description 指向）: https://data.chain.link/streams/btc-usd-twap-60s-streams  

### DB

- DuckDB Go: https://duckdb.org/docs/current/clients/go  
- pgx: https://github.com/jackc/pgx  

### 實測快照（撰寫 plan 時）

- 例：`btc-updown-5m-1787074800`  
- `orderMinSize: 5`, `orderPriceMinTickSize: 0.001`  
- `resolutionSource`: Chainlink BTC/USD TWAP  
- outcomes: Up/Down + 對應 `clobTokenIds`

---

## 15. 建議下一步（實作時）

1. 先落地 **Phase 0–4 + DuckDB dry-run**（零金流）  
2. 跑足夠 windows 後用 `v_strategy_performance` / `v_calibration` 看要不要 live  
3. 再開 Postgres（若要長期存）與 live 小額  

---

*本文件對應 `prompt.md`；實作完成後應以實際 config 預設與 schema 回寫 README，並在偏離原 repo 邏輯處加註 `strategy_source` 與 diff 說明。*
