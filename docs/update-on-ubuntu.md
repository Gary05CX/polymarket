# Ubuntu 更新指南（不只 git pull）

> 路徑：`docs/update-on-ubuntu.md`  
> **給 Hermes / 自動化 agent 的逐步指令：** [`HERMES-UPDATE-AND-RESTART.md`](./HERMES-UPDATE-AND-RESTART.md)

每次 push 後，Ubuntu 上**不能只 `git pull`**。

---

## 最短流程

```bash
cd ~/polymarket-bot
sudo systemctl stop polymarket-bot
git pull origin dev/crypto/dev
go build -o bot .
# 確認 .env: CONFIG_PATH=configs/config.ubuntu.yaml  DB_DRIVER=postgres  DRY_RUN=true
sudo systemctl start polymarket-bot
sudo journalctl -u polymarket-bot -n 50 --no-pager
```

---

## 為什麼

| 東西 | pull 後自動生效？ | 動作 |
|------|-------------------|------|
| Go 原始碼 | 否 | `go build -o bot .` |
| `configs/*.yaml` | 重啟後 | `systemctl restart/start` |
| `.env` | 不會被 pull 改 | 手動對照 `.env.example` |
| systemd unit | 否 | 僅 unit 有改時 `cp` + `daemon-reload` |
| Postgres 資料 | 不變 | 一般不用動 |

---

## 設定檔 profile

| 檔案 | 用途 |
|------|------|
| `configs/config.ubuntu.yaml` | **目前預設 = half-tight**（砍 0.64–0.70、壓 B、A/B 互斥） |
| `configs/config.ubuntu.half-tight.yaml` | 同上（明確檔名） |
| `configs/config.ubuntu.paper-bold.yaml` | 大膽收樣本 |
| `configs/config.ubuntu.strict.yaml` | 最嚴 |

`.env`：

```bash
CONFIG_PATH=configs/config.ubuntu.yaml
DB_DRIVER=postgres
DATABASE_URL=postgres://...
DRY_RUN=true
```

---

## Half-tight 變更摘要（相對 paper-bold）

- A `price_max`: 0.70 → **0.64**
- A 重新開啟 `reject_fair_at_clamp`
- B size: 5 → **3**；`max_market_price`: 0.75 → **0.62**
- 新增 **`one_order_per_market: true`**（同窗口 A/B 只能下一個）

---

## 驗證

Log 應含：

```text
"db_driver":"postgres"
"assets":["btc","eth"]
"timeframes":["5m"]
"dry_run":true
```

詳見 **`docs/HERMES-UPDATE-AND-RESTART.md`**。
