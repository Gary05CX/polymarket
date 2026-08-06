# Ubuntu 長跑部署指南（PostgreSQL）

本文件說明如何在 **Ubuntu** 上安裝、設定並以 **systemd** 長期運行 Polymarket 5m 交易 bot。  
Ubuntu 長跑建議使用 **PostgreSQL**；本機開發可用 DuckDB。兩者可在 **`.env` 用 `DB_DRIVER` 切換**。

---

## 目錄

1. [架構與前提](#1-架構與前提)
2. [系統需求與套件](#2-系統需求與套件)
3. [安裝 Go](#3-安裝-go)
4. [安裝與設定 PostgreSQL](#4-安裝與設定-postgresql)
5. [取得程式碼並編譯](#5-取得程式碼並編譯)
6. [設定檔與環境變數](#6-設定檔與環境變數)
7. [手動試跑（確認沒問題再裝 service）](#7-手動試跑確認沒問題再裝-service)
8. [systemd 服務](#8-systemd-服務)
9. [日常維運](#9-日常維運)
10. [查詢 PostgreSQL（看績效）](#10-查詢-postgresql看績效)
11. [切換 DuckDB / PostgreSQL](#11-切換-duckdb--postgresql)
12. [常見問題](#12-常見問題)
13. [安全提醒](#13-安全提醒)

---

## 1. 架構與前提

### 這次 Ubuntu 長跑建議設定

| 項目 | 值 |
|------|-----|
| 資產 | BTC、ETH |
| 時間框架 | **僅 5m** |
| 模式 | **`dry_run: true`**（紙上交易，不下真單） |
| 資料庫 | **PostgreSQL** |
| 設定檔 | `configs/config.ubuntu.yaml` |

### 程式做什麼

1. Binance WebSocket 收 BTC/ETH 現貨價  
2. Gamma API 發現當前 5m Up/Down 市場  
3. （可選）CLOB 公開 orderbook 取 mid  
4. Strategy A / B 判斷是否下單  
5. dry_run：只寫 DB，**不**對 CLOB 下真單  
6. 窗口結束後 paper settle，寫入 `pnl_ledger`  

### 你需要準備

- 一台可連外網的 Ubuntu（建議 22.04 / 24.04 LTS）  
- 可 `sudo` 的使用者  
- GitHub 上的本 repo 存取權限  
- **先不要**上真實 `PRIVATE_KEY`，除非你已確認 paper 結果  

---

## 2. 系統需求與套件

```bash
sudo apt update
sudo apt upgrade -y

# 編譯工具、git、postgres
sudo apt install -y \
  build-essential \
  git \
  curl \
  ca-certificates \
  postgresql \
  postgresql-contrib
```

| 套件 | 用途 |
|------|------|
| `build-essential` | 編譯用（若只用 Postgres 可選；DuckDB 需要 CGO/gcc） |
| `git` | 拉程式 |
| `postgresql` | 長跑資料庫 |
| Go 1.24+ | 見下一節（apt 版可能太舊） |

啟動並確認 Postgres：

```bash
sudo systemctl enable --now postgresql
sudo systemctl status postgresql --no-pager
```

---

## 3. 安裝 Go

CLOB SDK 需要 **Go 1.24+**。請到 [https://go.dev/dl/](https://go.dev/dl/) 下載 Linux 版，或：

```bash
# 範例：Go 1.24.5（請依官網最新版調整 URL）
cd /tmp
curl -fsSL -o go.tgz https://go.dev/dl/go1.24.5.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go.tgz

# 寫入 PATH（bash）
echo 'export PATH=/usr/local/go/bin:$HOME/go/bin:$PATH' >> ~/.bashrc
source ~/.bashrc

go version
# 應顯示 go1.24.x
```

---

## 4. 安裝與設定 PostgreSQL

### 4.1 建立使用者與資料庫

把 `CHANGE_ME` 換成強密碼：

```bash
sudo -u postgres psql <<'SQL'
CREATE USER polymarket WITH PASSWORD 'CHANGE_ME';
CREATE DATABASE polymarket OWNER polymarket;
GRANT ALL PRIVILEGES ON DATABASE polymarket TO polymarket;
\c polymarket
GRANT ALL ON SCHEMA public TO polymarket;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO polymarket;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO polymarket;
SQL
```

### 4.2 允許本機密碼連線（若預設 peer 認證）

編輯 `pg_hba.conf`（路徑因版本而異，常見如下）：

```bash
# 查設定檔位置
sudo -u postgres psql -c "SHOW hba_file;"
```

確認有類似（本機 TCP 用 scram/md5）：

```text
# IPv4 local connections:
host    all             all             127.0.0.1/32            scram-sha-256
```

改完後：

```bash
sudo systemctl reload postgresql
```

### 4.3 測試連線

```bash
export DATABASE_URL="postgres://polymarket:CHANGE_ME@127.0.0.1:5432/polymarket?sslmode=disable"
psql "$DATABASE_URL" -c "SELECT version();"
```

### 4.4 Schema

Bot **啟動時會自動執行** `db/init_postgresql.sql` 的建表邏輯（embedded）。  
也可手動先建：

```bash
cd ~/polymarket-bot   # 你的專案目錄
psql "$DATABASE_URL" -f db/init_postgresql.sql
```

主要資料表：

| 表 | 內容 |
|----|------|
| `markets` | 發現到的 5m 市場、開收盤價、結算結果 |
| `orders` | paper / live 訂單 |
| `positions` | 當前持倉（settled 後會清） |
| `price_snapshots` | 幣價 + mid + fair |
| `pnl_ledger` | paper 損益流水 |
| `bot_logs` | 重要事件 |

---

## 5. 取得程式碼並編譯

```bash
# 建議放在家目錄
cd ~
git clone https://github.com/Gary05CX/polymarket.git polymarket-bot
cd polymarket-bot

# 切到你實際要部署的 branch（例如）
# git checkout dev/crypto/dev
# git pull

go mod tidy
go build -o bot .

# 確認 binary
./bot -h 2>/dev/null || true
ls -la bot
```

> 若編譯報錯與 Go 版本有關，請確認 `go version` ≥ 1.24。

---

## 6. 設定檔與環境變數

### 6.1 兩份設定的關係

| 檔案 | 用途 |
|------|------|
| `configs/config.ubuntu.yaml` | Ubuntu 長跑：btc+eth、5m、策略參數 |
| `.env` | **密碼、DB 切換、PRIVATE_KEY**（不要 commit） |

程式讀取順序概念：

1. `CONFIG_PATH` 指定 yaml（systemd 設為 ubuntu 設定）  
2. `.env` / 環境變數 **覆蓋** 敏感項與 `DB_DRIVER`  

### 6.2 建立 `.env`

```bash
cd ~/polymarket-bot
cp .env.example .env
chmod 600 .env
nano .env   # 或 vim
```

### 6.3 建議的 Ubuntu `.env` 內容

```bash
# ---------- 設定檔 ----------
CONFIG_PATH=configs/config.ubuntu.yaml

# ---------- 資料庫：PostgreSQL ----------
DB_DRIVER=postgres
DATABASE_URL=postgres://polymarket:CHANGE_ME@127.0.0.1:5432/polymarket?sslmode=disable

# 若暫時想用檔案 DB（不建議長跑）：
# DB_DRIVER=duckdb
# DUCKDB_PATH=data/bot.duckdb

# ---------- 交易模式 ----------
# true = 紙上交易（強烈建議先長跑幾天）
DRY_RUN=true

# 實盤才需要（dry_run=false 時必填）
# PRIVATE_KEY=0x...
# SIGNATURE_TYPE=0
# FUNDER=
# CLOB_API_KEY=
# CLOB_SECRET=
# CLOB_PASSPHRASE=

CLOB_HOST=https://clob.polymarket.com
CHAIN_ID=137

# 可選：熔斷 / 錯誤通知
# WEBHOOK_URL=https://...
```

### 6.4 環境變數一覽

| 變數 | 必填 | 說明 |
|------|------|------|
| `DB_DRIVER` | 建議 | `postgres` 或 `duckdb` |
| `DATABASE_URL` | postgres 時必填 | Postgres 連線字串 |
| `DUCKDB_PATH` | duckdb 時 | 預設 `data/bot.duckdb` |
| `CONFIG_PATH` | 建議 | Ubuntu 用 `configs/config.ubuntu.yaml` |
| `DRY_RUN` | 建議 | `true` / `false` |
| `PRIVATE_KEY` | 實盤必填 | 錢包私鑰 |
| `CLOB_*` | 可選 | 預填 L2 憑證；空則自動 derive |
| `WEBHOOK_URL` | 可選 | 告警 |

### 6.5 `config.ubuntu.yaml` 重點（已幫你設好）

- `assets: [btc, eth]`  
- `timeframes: [5m]`  
- `clob.dry_run: true`  
- `database.driver: postgres`（仍以 `.env` 的 `DB_DRIVER` 為準）  
- Strategy A 較嚴（band 0.58–0.64、fair clamp 過濾等）  

一般 **不必改 yaml**，改 `.env` 即可。

---

## 7. 手動試跑（確認沒問題再裝 service）

```bash
cd ~/polymarket-bot
set -a
source .env
set +a

./bot
```

### 7.1 預期啟動 log（JSON）

關鍵欄位應類似：

```json
{
  "msg": "bot_start",
  "detail": {
    "assets": ["btc", "eth"],
    "timeframes": ["5m"],
    "dry_run": true,
    "db_driver": "postgres"
  }
}
```

接著應看到：

- `binance prices ready` / `binance ws connected`  
- 進入 `entering main loop`  
- 一段時間後可能有 `order_signal`、`paper_settle`  

用 `Ctrl+C` 可優雅關閉（會 settle 到期窗口、checkpoint 若為 DuckDB）。

### 7.2 確認 Postgres 有資料

另開一個 terminal：

```bash
export DATABASE_URL="postgres://polymarket:CHANGE_ME@127.0.0.1:5432/polymarket?sslmode=disable"

psql "$DATABASE_URL" -c "\dt"
psql "$DATABASE_URL" -c "SELECT COUNT(*) FROM price_snapshots;"
psql "$DATABASE_URL" -c "SELECT ts, event FROM bot_logs ORDER BY ts DESC LIMIT 10;"
```

若表存在且有 snapshot / log，代表寫庫成功。

---

## 8. systemd 服務

### 8.1 安裝 unit

```bash
cd ~/polymarket-bot

# 複製並替換使用者名稱
sudo cp systemd/polymarket-bot.service /etc/systemd/system/polymarket-bot.service
sudo sed -i "s|YOUR_USER|$USER|g" /etc/systemd/system/polymarket-bot.service

# 確認路徑正確（預設 ~/polymarket-bot）
grep -E 'User=|WorkingDirectory=|ExecStart=|EnvironmentFile=' /etc/systemd/system/polymarket-bot.service
```

預設 unit 內容概念：

```ini
WorkingDirectory=/home/你的帳號/polymarket-bot
ExecStart=/home/你的帳號/polymarket-bot/bot
EnvironmentFile=/home/你的帳號/polymarket-bot/.env
Environment=CONFIG_PATH=configs/config.ubuntu.yaml
Restart=always
RestartSec=8
After=network-online.target postgresql.service
```

若專案不在 `~/polymarket-bot`，請手動改 `WorkingDirectory`、`ExecStart`、`EnvironmentFile`。

### 8.2 啟用並開機自啟

```bash
sudo systemctl daemon-reload
sudo systemctl enable polymarket-bot
sudo systemctl start polymarket-bot
sudo systemctl status polymarket-bot --no-pager
```

### 8.3 看 log

```bash
# 即時追蹤
sudo journalctl -u polymarket-bot -f

# 最近 200 行
sudo journalctl -u polymarket-bot -n 200 --no-pager

# 今天
sudo journalctl -u polymarket-bot --since today
```

### 8.4 常用指令

```bash
sudo systemctl stop polymarket-bot
sudo systemctl start polymarket-bot
sudo systemctl restart polymarket-bot
sudo systemctl disable polymarket-bot   # 取消開機自啟
```

### 8.5 更新程式後重啟

```bash
cd ~/polymarket-bot
git pull
go build -o bot .
sudo systemctl restart polymarket-bot
sudo journalctl -u polymarket-bot -n 50 --no-pager
```

---

## 9. 日常維運

### 建議每天看一次

1. service 是否 `active (running)`  
2. journal 是否狂刷 error  
3. Postgres 連線與磁碟空間  

```bash
sudo systemctl is-active polymarket-bot
df -h
sudo -u postgres psql -c "SELECT pg_size_pretty(pg_database_size('polymarket'));"
```

### 備份 Postgres（建議）

```bash
# 簡單 dump
pg_dump "$DATABASE_URL" | gzip > ~/backup-polymarket-$(date +%F).sql.gz
```

可放進 cron（每天一次）。

### 不要同時開兩個 bot 寫同一個 DB

同一 `DATABASE_URL` 只跑一個 instance，避免倉位/訂單邏輯混亂。

---

## 10. 查詢 PostgreSQL（看績效）

```bash
export DATABASE_URL="postgres://polymarket:CHANGE_ME@127.0.0.1:5432/polymarket?sslmode=disable"
psql "$DATABASE_URL"
```

### 總損益

```sql
SELECT kind,
       COUNT(*) AS n,
       ROUND(SUM(amount_usd)::numeric, 4) AS total_pnl
FROM pnl_ledger
GROUP BY 1;
```

### 勝率粗算

```sql
SELECT
  COUNT(*) FILTER (WHERE settle_pnl_usd > 0) AS wins,
  COUNT(*) FILTER (WHERE settle_pnl_usd < 0) AS losses,
  ROUND(SUM(settle_pnl_usd)::numeric, 4) AS total
FROM markets
WHERE settled_at IS NOT NULL AND settle_pnl_usd IS NOT NULL;
```

### 依資產

```sql
SELECT
  CASE
    WHEN slug LIKE 'btc-%' THEN 'btc'
    WHEN slug LIKE 'eth-%' THEN 'eth'
    ELSE 'other'
  END AS asset,
  COUNT(*) AS n,
  ROUND(SUM(settle_pnl_usd)::numeric, 4) AS pnl
FROM markets
WHERE settled_at IS NOT NULL AND settle_pnl_usd IS NOT NULL
GROUP BY 1;
```

### 最近訂單

```sql
SELECT market_slug, strategy, price, size_usd, status, created_at
FROM orders
ORDER BY created_at DESC
LIMIT 20;
```

### 最近結算

```sql
SELECT slug, settle_outcome, settle_pnl_usd, open_price, close_price, settled_at
FROM markets
WHERE settled_at IS NOT NULL
ORDER BY settled_at DESC
LIMIT 20;
```

### 啟動設定紀錄

```sql
SELECT ts, detail
FROM bot_logs
WHERE event = 'bot_start'
ORDER BY ts;
```

---

## 11. 切換 DuckDB / PostgreSQL

全部在 **`.env`** 完成，不必改 code。

### 用 PostgreSQL（Ubuntu 推薦）

```bash
DB_DRIVER=postgres
DATABASE_URL=postgres://polymarket:密碼@127.0.0.1:5432/polymarket?sslmode=disable
```

### 用 DuckDB（檔案庫）

```bash
DB_DRIVER=duckdb
DUCKDB_PATH=data/bot.duckdb
# 需要 gcc / CGO：
# sudo apt install -y build-essential
```

### 重要

- **不會自動搬資料**。換 driver = 新帳本。  
- 改 `.env` 後：`sudo systemctl restart polymarket-bot`  

---

## 12. 常見問題

### Q1. `store: ping postgres: connection refused`

- Postgres 沒啟動：`sudo systemctl start postgresql`  
- 密碼 / port / 使用者錯誤  
- `pg_hba.conf` 不允許密碼連線  

### Q2. 啟動後 `db_driver` 仍是 duckdb

- `.env` 沒被 systemd 讀到（`EnvironmentFile` 路徑錯）  
- `DB_DRIVER` 拼錯或有引號問題  
- 看 unit：`systemctl cat polymarket-bot`  

### Q3. service 一直 restart

```bash
sudo journalctl -u polymarket-bot -n 100 --no-pager
```

常見：找不到 `bot` binary、`.env` 權限、`DATABASE_URL` 錯、working directory 錯。

### Q4. 沒有 `order_signal`

正常可能：市場 mid 不在 0.58–0.64、edge 不夠、fair 觸頂被擋。  
看：

```sql
SELECT ts, event, detail FROM bot_logs
WHERE event IN ('strategy_skip_summary','reject_summary','order_signal')
ORDER BY ts DESC LIMIT 30;
```

### Q5. dry_run 與真錢

- `dry_run: true`：**不會**用 `PRIVATE_KEY` 下單  
- 改 `false` 前請確認 wallet、資金、風險參數  

### Q6. 時區

Bot 內部多用 UTC；Postgres `timestamptz` 會轉。  
journal 時間依系統 timezone。

### Q7. 防火牆 / 出口

需能連：

- `stream.binance.com`（WSS）  
- `gamma-api.polymarket.com`  
- `clob.polymarket.com`（orderbook；實盤下單也要）  

---

## 13. 安全提醒

1. **`.env` 權限**：`chmod 600 .env`，不要 commit 到 git。  
2. **Postgres 密碼** 勿用預設 `CHANGE_ME`。  
3. 長跑階段保持 **`DRY_RUN=true`**，直到你自己認定 paper 結果可接受。  
4. 本 bot **不是投資建議**；paper 與實盤有成交、結算源（Binance vs Chainlink）、手續費等差異。  
5. 不要在多台機器用同一個熱錢包同時跑實盤。  

---

## 檢查清單（上線前）

- [ ] `go version` ≥ 1.24  
- [ ] `postgresql` active  
- [ ] `psql "$DATABASE_URL"` 可連  
- [ ] `./bot` 手動跑有 `db_driver=postgres`  
- [ ] `price_snapshots` / `bot_logs` 有資料  
- [ ] `.env` 權限 600，密碼已改  
- [ ] `systemctl enable --now polymarket-bot`  
- [ ] `journalctl -u polymarket-bot -f` 無連續 fatal  
- [ ] `DRY_RUN=true`  

---

## 相關檔案

| 路徑 | 說明 |
|------|------|
| `configs/config.ubuntu.yaml` | Ubuntu 預設（目前 = paper 大膽版） |
| `configs/config.ubuntu.paper-bold.yaml` | paper 大膽版副本 |
| `configs/config.ubuntu.strict.yaml` | 之後收緊用 |
| `.env.example` | 環境變數範本 |
| `systemd/polymarket-bot.service` | systemd unit 範本 |
| `db/init_postgresql.sql` | Postgres 建表 |
| `docs/ubuntu-deploy.md` | 較短的英文摘要 |
| `docs/update-on-ubuntu.md` | **更新時除了 git pull 還要做什麼** |

---

完成後，bot 會在背景長跑；你可用 `journalctl` 看即時 log、用 `psql` 看 paper 損益。若要把查詢做成一鍵腳本或加 `/stats` HTTP，可以再說。
