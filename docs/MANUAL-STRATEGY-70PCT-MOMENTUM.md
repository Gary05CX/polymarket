# 手動策略筆記：70% 跟勢（$5 單）

> 來源：真人盤感總結（非 bot 現況 half-tight）。  
> 資金軌跡示例：**~$12 → ~$32**（短樣本，不保證可複製）。  
> 用途：先寫清楚規則，之後再決定要不要做成 script / 併進 bot。

---

## 1. 一句話

在 5m Up/Down 上，**現價相對本窗 target（open）已拉開一定距離、且過了至少 2 分鐘**時，把 **$5 壓在市場定價約 70%（mid≈0.70）的那一側**（多半是已領先、較「會贏」的一邊）。  
單筆設計接近：**本金 $5 → 贏約拿回 $7 → 淨 +$2**；連輸兩把就停手休息。

---

## 2. 進場條件（全部滿足才下）— **2026-08 更新**

| # | 條件 | 說明 / 建議量化 |
|---|------|-----------------|
| 1 | **標的** | BTC 與 ETH 的 **5m Up/Down**（筆記寫 ETC = **ETH**） |
| 2 | **相對 target 的距離** | **BTC：\|spot − open\| > $2**；**ETH：\|spot − open\| > $0.2** |
| 3 | **時間結構** | ① 市場開了 **≥ 1 分鐘** 才開始計時 |
|   | | ② 距離條件需 **連續維持 ≥ 2 分鐘** |
|   | | → 理論上最早約在 **開窗後 3 分鐘** 才可能下單 |
| 4 | **方向** | **跟大的那邊**（spot>open → Up；spot<open → Down） |
| 5 | **mid 帶（script）** | 預設 **0.55–0.88**（放寬以便 dry-run 有單；可再收成 ~0.70） |
| 6 | **下注金額** | **固定 $5 USD** notional |

### 方向怎麼定（建議寫死，方便 script）

- 若 `spot > target` 且距離達標 → 偏 **Up**（買 Up token，當其 mid 落在 ~70% 帶）  
- 若 `spot < target` 且距離達標 → 偏 **Down**  
- 若領先方 mid **遠高於 70%**（如 0.90）→ 本筆記策略**不鼓勵**（賠率差、一次回吐多）；手動若要碰需另訂規則  
- 若領先方 mid **遠低於 70%**（如 0.55）→ 屬另一套「便宜 edge」玩法（現有 half-tight bot），與此筆記不同

---

## 3. 期望報酬結構（理想情況）

假設買在 **0.70**、notional **$5**：

| | 約略 |
|--|------|
| 買到的 shares | \(5 / 0.70 ≈ 7.14\) |
| **贏** | 每 share 結算 $1 → 收回 ≈ **$7.14** → **淨 ≈ +$2.14** |
| **輸** | 失去 ≈ **$5**（整筆本金） |
| 盈虧比 | 約 **+2 : −5** → 要長期正期望，真實勝率需 **明顯高於 ~71%**（未計 fee / 滑點） |

筆記體感：「基本每一單都能用 5 賺 7、淨 2」= **高勝率跟勢假設**；  
若真實勝率掉到 70% 附近或更低，數學上容易打平或虧（尤其含 fee、沒成交、假突破）。

---

## 4. 風控 / 節奏

| 規則 | 內容 |
|------|------|
| **連輸 2 把** | **立刻停手**，不要急著翻本 |
| **休息** | 約 **30～60 分鐘**，等波動/情緒冷卻再重新看條件 |
| **單筆** | 固定 $5，不在連贏時加碼（本筆記未定義馬丁格爾） |
| **同窗** | 建議 **同一 5m 窗最多 1 單**（避免重複追） |
| **資金** | 示例從 ~$12 做到 ~$32；script 前應設 **日虧上限**（例如 −$10 或 −$15） |

---

## 5. 和現有 bot（half-tight）的差異

| | 本手動 70% 跟勢 | 現 bot half-tight |
|--|-----------------|-------------------|
| 進場 mid | **~0.70** | **0.55–0.64**（故意避開貴價） |
| 邏輯 | 現價已離 open 夠遠 + 時間過濾 | 模型 fair − mid edge |
| Size | **$5 固定** | ~$2–3 或 autosize |
| 頻率 | 條件鬆時可能較密 | 偏挑、常半小時 0～1 單 |
| 大風險 | **一次輸 ≈ −$5**，連兩下 −$10 | 單筆較小；高 mid 歷史 paper 曾大虧 |

**不要**在未驗證前把 0.70 帶直接併進 half-tight 同一套參數；較適合 **獨立 profile / 獨立 script**。

---

## 6. 之後若做成 script：建議狀態機

```text
IDLE
  → 每 N 秒掃 BTC/ETH 當前 5m 窗
  → 若 elapsed ≥ 120s
     且 |spot-open| ≥ thr(asset)
     且 領先側 mid ∈ [0.65, 0.75]（可調）
     且 本窗尚未下單
     且 不在冷卻中
  → PLACE $5 limit/market（規則另定）
  → 等結算

ON_LOSS: consecutive_losses++
  → 若 consecutive_losses ≥ 2
     → COOLDOWN 30–60 min
     → consecutive_losses = 0（或冷卻結束再清）

ON_WIN: consecutive_losses = 0
```

### Script 必備欄位（對帳）

- `dry_run`、成交價、size、$ PnL  
- 進場時的 `open/spot/move/mid/seconds_elapsed`  
- 連敗計數與冷卻截止時間  

### 實作時要注意（手動沒寫但真錢會踩）

1. **CLOB 最小 shares（常 ≥ 5）**、post-only 穿價  
2. **70% 流動性**、是否吃 ask  
3. **官方結算** vs spot open/close 近似  
4. **BTC±10 / ETH±1** 是否要用「百分比」或隨波動調整（靜態美元在不同價位意義不同）  
5. 與現有 bot **互斥**（同帳戶避免雙策略同窗對打或疊倉）

---

## 7. 驗證建議（做 script 之前）

1. **先 paper / 小 size（$1–2）** 記 30～50 筆：勝率、平均進場 mid、連敗長度  
2. 統計 **「達距離+≥2min」之後 mid≈0.70 的真實勝率** 是否 ≥ ~75%+  
3. 再考慮固定 $5 live  
4. 通過後再寫進 repo（例如 `strategy_c` 或獨立 `scripts/seventy_momentum`）

---

## 8. 參數速查（預設草案）

```yaml
# configs/config.ubuntu.strategy-c-70.yaml（已接入 Strategy C）
strategy_c:
  enabled: true
  size_usd: "5"
  min_elapsed_sec: 60          # 開窗 1 分鐘後才開始計
  sustained_above_sec: 120     # 距離條件連續 2 分鐘
  btc_move_usd: "2"
  eth_move_usd: "0.2"
  mid_min: "0.55"
  mid_max: "0.88"
  max_consecutive_losses: 2
  cooldown_sec: 2700
```

---

## 9. 回撤過濾（已寫進 Strategy C）

手動補充：

> 若線**往 target 靠近**（回撤），先**不下手**；等約 **30 秒～1 分鐘** 看是否穩住；仍不穩（繼續往 open 靠）→ **不下**。

Script 對應：

| 參數 | 預設 | 行為 |
|------|------|------|
| `stability_lookback_sec` | 30 | 與 N 秒前的 \|spot−open\| 比 |
| `retrace_epsilon_usd` | 0.3 | abs 距離少超過此值 → 視為回撤 |
| `stability_wait_sec` | 45 | 回撤時先等這麼久 |
| 等完仍回撤 | — | `retrace_unstable_after_wait`，本 setup 放棄 |

## 10. Dry-run 設定檔

```bash
CONFIG_PATH=configs/config.ubuntu.strategy-c-70.yaml
DRY_RUN=true
```

- 只開 **Strategy C**（A/B 關閉）  
- `strategy` 欄位在 orders 為 **`C`**  
- 連輸 2 次後 log：`strategy_c_cooldown`

查 paper：

```sql
SELECT status, strategy, ROUND(size_usd::numeric,2), market_slug, created_at
FROM orders WHERE strategy = 'C' ORDER BY created_at DESC LIMIT 30;

SELECT kind, COUNT(*), ROUND(SUM(amount_usd)::numeric,2)
FROM pnl_ledger WHERE ts > now() - interval '1 day' GROUP BY 1;
```

## 11. 一句話備忘

**離 open 夠遠 + 過 2 分鐘 + $5 打在 ~70% 領先邊；回撤就等、不穩不下；贏約 +$2，輸 −$5；連輸 2 次休息 ~45 分。**  
先 **dry_run** 收樣本，再談 live。