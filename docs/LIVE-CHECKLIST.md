# 真錢最小設定清單（Live go-live checklist）

目標：在 **half-tight 邏輯不變** 的前提下，安全地從 paper → 小 size 真錢。  
**預設不要改策略 band / edge**；只動 wallet、`DRY_RUN`、size profile。

---

## 0. 你現在有什麼

| 項目 | 狀態（截至 paper 分析） |
|------|------------------------|
| 策略 | half-tight：A mid 0.55–0.64、B 極少成交 |
| Paper ~$1 | 約 +$23 / 29 筆 / WR ~83%（仍偏短樣本） |
| Paper ~$5 profile | `configs/config.ubuntu.half-tight-size5.yaml`（**仍 dry_run**） |
| Live ~$2–3 / ~$10 本金 | `configs/config.ubuntu.live-size2-3.yaml`（日虧 $5 / 時虧 $3） |
| 自動 redeem / Relayer | **沒做** — 贏了要自己在站上 redeem 或另寫流程 |
| 成交模型 | Paper 樂觀 mid 成交；Live 是限價單，**可能不成交** |

---

## 1. 錢包（先搞對再碰 `DRY_RUN=false`）

### 必備

| 項目 | 說明 |
|------|------|
| **Export private key** | Polymarket 站上 **Export private key** 的那把 = **Signer** → 填 `PRIVATE_KEY` |
| **FUNDER** | **有錢的那個地址**（portfolio / deposit / proxy 顯示的資金地址）。**不要填 `0`** |
| **SIGNATURE_TYPE** | 常見：站上 proxy / Magic / email 登入 → 多半 **`1`（PolyProxy）**；自管 EOA 直接連 → **`0`**。不確定時：Signer ≠ 資金地址 → 先試 `1` |
| 鏈上 USDC.e | FUNDER 在 **Polygon** 上有足夠抵押（小 size 先放 **$30–$100** 夠 smoke） |

### 不要搞混

| 東西 | 要不要 |
|------|--------|
| API-only key / 只讀 | ❌ 不能下單 |
| Relayer / `RELAYER_*` | ❌ 本 bot **沒用**（redeem 另議） |
| Apple Pay 本身 | ❌ 不是 key；只是充值管道 |
| 把 `PRIVATE_KEY` 貼到 chat / git | ❌ 永遠不要 |

### `PRIVATE_KEY` 與 `FUNDER` 可以相同嗎？

- **EOA 直連且錢在同一地址**：可以相同，`SIGNATURE_TYPE=0`，`FUNDER` 可空或 = 同一地址。  
- **站上 proxy（多數 Apple Pay / 網站錢包）**：**通常不同** — key 是 signer，`FUNDER` 是 proxy/portfolio 地址。

### 怎麼確認「哪個地址有錢」

1. 打開 Polymarket portfolio / deposit 地址。  
2. 用 Polygonscan 查該地址 **USDC.e** 餘額。  
3. 那個地址 → `FUNDER`。  
4. Export 的 private key 對應的地址 → 是 **signer**（可能和 FUNDER 不同）。

---

## 2. `.env` 最小集合

路徑：Ubuntu 上 `~/polymarket-bot/.env`（`chmod 600`）。

### 現在繼續 paper（建議先跑 size5 一晚）

```bash
CONFIG_PATH=configs/config.ubuntu.half-tight-size5.yaml
DB_DRIVER=postgres
DATABASE_URL=postgres://polymarket:SECRET@127.0.0.1:5432/polymarket?sslmode=disable
DRY_RUN=true
# PRIVATE_KEY / FUNDER 可先不填
```

### 真錢 smoke（小 size）時才加

```bash
CONFIG_PATH=configs/config.ubuntu.half-tight-size5.yaml   # 或先用更小的 ubuntu.yaml
DB_DRIVER=postgres
DATABASE_URL=postgres://...
DRY_RUN=false

PRIVATE_KEY=0x...          # Export private key（Signer）
SIGNATURE_TYPE=1           # proxy 常見；EOA 改 0
FUNDER=0x...               # 有 USDC 的地址（不要 0）

# 可選：若你已有 L2 API 憑證可填；空則 bot 會 CreateOrDerive
# CLOB_API_KEY=
# CLOB_SECRET=
# CLOB_PASSPHRASE=
```

**優先級提醒：**

- 環境變數 `DRY_RUN=true/false` **會覆蓋** yaml 裡的 `clob.dry_run`。  
- 真錢時 yaml 可維持 `dry_run: true`，只要 `.env` 寫 `DRY_RUN=false` 就會下真單 — **改 env 前再確認三遍**。

---

## 3. yaml：改什麼 / 先不要動什麼

### 可以改（size / 風控）

| 檔案 / 欄位 | 用途 |
|-------------|------|
| `CONFIG_PATH` → `half-tight-size5.yaml` | A ~$4–6、B $5、日損/時損放大 |
| `risk.max_daily_loss_usd` / `max_hourly_loss_usd` | 真錢時可再收緊 |
| `strategy_a.size_min_usd` / `size_max_usd` | 再縮到 $2–3 smoke 也可以 |

### 先不要動（策略邏輯）

| 欄位 | 原因 |
|------|------|
| `price_min` / `price_max` (0.55–0.64) | paper 已證明 0.64–0.70 大虧 |
| `min_edge` / `reject_fair_at_clamp` | half-tight 核心 |
| `one_order_per_market: true` | 防 A+B 同窗雙開 |
| `assets` / `timeframes` | 先維持 btc+eth / 5m |

### Size5 與程式注意

舊版 `risk` 曾把 A **硬卡 $3**。新版本改為只受 `max_position_usd_per_market` 限制。  
**上 size5 前必須 `git pull` + `go build`**，否則 yaml 寫 6 也會被 clip 成 3。

---

## 4. 建議上線順序（不要跳步）

```
[A] half-tight ~$1 paper     ✅ 已做
[B] half-tight ~$5 paper     ← 現在建議跑一晚
[C] 錢包 + FUNDER 對帳       ← 可並行準備，仍 DRY_RUN=true
[D] DRY_RUN=false + 極小 size 真錢 smoke（數筆）
[E] 確認站上訂單 / 成交 / 結算 / redeem
[F] 再放大 size 或加資金
```

### [B] 切 size5 paper（Ubuntu / Hermes）

```bash
sudo systemctl stop polymarket-bot
cd ~/polymarket-bot
git pull origin dev/crypto/dev
go build -o bot .
# .env:
#   CONFIG_PATH=configs/config.ubuntu.half-tight-size5.yaml
#   DRY_RUN=true
sudo systemctl start polymarket-bot
journalctl -u polymarket-bot -n 40 --no-pager
# 確認 log: "dry_run":true 且 size_usd 約 4–6
```

### [D] 真錢 smoke 前 10 秒確認

```bash
grep -E '^(CONFIG_PATH|DRY_RUN|SIGNATURE_TYPE|FUNDER)=' .env
# PRIVATE_KEY 有值即可，不要 cat 出來
# 確認: DRY_RUN=false 且 FUNDER 非空
```

啟動後立刻看：

```bash
journalctl -u polymarket-bot -f
# 應看到 CLOB client 初始化成功；出現 order 時 status 不應再全是 dry_run
```

站上 / API：是否出現 **限價買單**；下一 window 是否有成交或取消。

### [E] 對帳 SQL（Postgres）

```sql
-- 最近訂單（dry_run / error_message 從 2026-08 起有欄位）
SELECT strategy, status, dry_run, price, size_usd,
       LEFT(error_message, 80) AS err, created_at
FROM orders
ORDER BY created_at DESC
LIMIT 20;

-- 只看真單
SELECT * FROM orders WHERE dry_run = false ORDER BY created_at DESC LIMIT 20;

-- 結算 PnL（paper_settle vs live_settle）
SELECT kind, amount_usd, detail, ts
FROM pnl_ledger
WHERE ts > now() - interval '24 hours'
ORDER BY ts DESC;

SELECT asset, settle_outcome, settle_pnl_usd, settled_at
FROM markets
WHERE settled_at > now() - interval '24 hours'
ORDER BY settled_at DESC;
```

---

## 5. 本 bot 上真錢後「還缺什麼」（務實）

| 能力 | 現況 |
|------|------|
| 下 CLOB 限價買 | ✅ live 模式會打 |
| Paper 用 spot open/close 結算 | ✅ dry_run |
| Live 自動標 settled / 算 PnL | ⚠️ 有邏輯但依賴訂單狀態；請用站上 + DB 交叉驗證 |
| 贏後 **redeem** USDC | ❌ 需手動或另做 Relayer/CTF |
| 部分成交 / 撤單策略 | ⚠️ 簡化；不要假設 100% 成交 |
| 多帳戶 / 自動轉資 | ❌ |

**真錢第一週心態：** 驗證「能掛單、能成交、數字對得上」，不是最大化 PnL。

---

## 6. 緊急停損

```bash
sudo systemctl stop polymarket-bot
# .env 立刻改回
# DRY_RUN=true
# 或拿掉 PRIVATE_KEY
sudo systemctl start polymarket-bot   # 若還要繼續 paper
```

風控熔斷：`max_daily_loss_usd` / `max_hourly_loss_usd` 觸發後會 halt 新單；仍建議 systemd stop 雙保險。

---

## 7. 一頁速查

| 階段 | CONFIG_PATH | DRY_RUN | PRIVATE_KEY | FUNDER |
|------|-------------|---------|-------------|--------|
| 現況 ~$1 paper | `config.ubuntu.yaml` | true | 可不填 | 可不填 |
| 今晚 ~$5 paper | `...half-tight-size5.yaml` | **true** | 可不填 | 可不填 |
| **~$2–3 真錢（$10 本金）** | **`...live-size2-3.yaml`** | **false** | **必填** | proxy **必填** |
| 真錢 smoke（舊路徑） | size5 或更小 | **false** | **必填** | proxy **必填** |

**記住：`DRY_RUN=false` 是唯一會讓錢真的出去的開關（外加有效 key）。改它之前停服務、看一眼 `.env`、再啟動。**
