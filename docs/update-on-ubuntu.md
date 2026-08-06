# Ubuntu 更新指南（不只 git pull）

> 這份是 **repo 內正式文件**（路徑：`docs/update-on-ubuntu.md`），不是只在聊天裡說過。  
> 相關：完整安裝見 [`ubuntu-setup.md`](./ubuntu-setup.md)；短 checklist 見 [`ubuntu-deploy.md`](./ubuntu-deploy.md)。

每次在開發機改完、push 到 GitHub 後，Ubuntu 上**不能只 `git pull`**。  
Binary 是編譯產物，設定是啟動時載入的，service 也不會自動換新檔。

---

## 一鍵流程（最常用）

在 **Ubuntu 專案目錄**（例如 `~/polymarket-bot`）：

```bash
cd ~/polymarket-bot

# 1) 停 bot（避免編譯/拉碼時還在寫 DB）
sudo systemctl stop polymarket-bot

# 2) 拉最新 code
git fetch origin
git checkout dev/crypto/dev    # 或你部署的 branch
git pull origin dev/crypto/dev

# 3) 重新編譯 binary（必要）
go mod tidy
go build -o bot .

# 4) 檢查 .env（有時新版加了變數，但不會自動改你的 .env）
#    對照 repo 裡的 .env.example
diff -u .env.example .env || true
nano .env   # 若有新的 DB_DRIVER / CONFIG_PATH 等再補

# 5) 確認設定檔路徑
#    paper 大膽版（目前預設 ubuntu 設定已等同 bold）:
#    CONFIG_PATH=configs/config.ubuntu.yaml
#    或明確:
#    CONFIG_PATH=configs/config.ubuntu.paper-bold.yaml
grep -E '^(CONFIG_PATH|DB_DRIVER|DATABASE_URL|DRY_RUN)=' .env

# 6) 重啟
sudo systemctl start polymarket-bot
sudo systemctl status polymarket-bot --no-pager

# 7) 看 log 是否為新設定
sudo journalctl -u polymarket-bot -n 80 --no-pager
```

啟動 log 應類似：

```text
"assets":["btc","eth"]
"timeframes":["5m"]
"dry_run":true
"db_driver":"postgres"
```

---

## 為什麼不能只 git pull？

| 東西 | 是否在 git 裡 | pull 後會自動生效？ | 你要做什麼 |
|------|----------------|---------------------|------------|
| Go 原始碼 | 是 | **否**（要重編） | `go build -o bot .` |
| `configs/*.yaml` | 是 | **重啟後**才讀 | `systemctl restart` |
| `systemd/*.service` | 是（範本） | **否** | 若 unit 有改，要 `cp` + `daemon-reload` |
| `.env` | **否**（本機檔） | 不會被 pull 覆蓋 | 手動對 `.env.example` 補變數 |
| Postgres 資料 | 否 | 不變 | 一般不用動 |
| 已安裝的 `/etc/systemd/system/polymarket-bot.service` | 否 | 不會自動更新 | 有改 unit 才重裝 |

---

## 這次「paper 大膽版」你要確認的

### 1. 設定檔

Repo 內：

| 檔案 | 用途 |
|------|------|
| `configs/config.ubuntu.yaml` | Ubuntu **預設**（已改成大膽 paper） |
| `configs/config.ubuntu.paper-bold.yaml` | 大膽版副本（內容相同策略） |
| `configs/config.ubuntu.strict.yaml` | 之後收緊用 |

`.env` 建議：

```bash
CONFIG_PATH=configs/config.ubuntu.yaml
# 或
# CONFIG_PATH=configs/config.ubuntu.paper-bold.yaml

DB_DRIVER=postgres
DATABASE_URL=postgres://polymarket:你的密碼@127.0.0.1:5432/polymarket?sslmode=disable
DRY_RUN=true
```

### 2. Binary 一定要重建

```bash
go build -o bot .
# 確認時間是新的
ls -la bot
```

若 `ExecStart` 指向別路徑的 `bot`，要編譯到那個路徑。

### 3. systemd unit 何時要更新？

只有當你改了 `systemd/polymarket-bot.service` 內容時才需要：

```bash
sudo cp systemd/polymarket-bot.service /etc/systemd/system/polymarket-bot.service
sudo sed -i "s|YOUR_USER|$USER|g" /etc/systemd/system/polymarket-bot.service
# 檢查 WorkingDirectory / ExecStart / EnvironmentFile
sudo systemctl daemon-reload
sudo systemctl restart polymarket-bot
```

若只改 yaml / Go code：**不必**重裝 unit，但要 **rebuild + restart**。

---

## 大膽版參數對照（你在 paper 放寬了什麼）

| 項目 | 舊 strict | 新 bold |
|------|-----------|---------|
| A price band | 0.58–0.64 | **0.55–0.70** |
| A min_edge | 0.04 | **0.025** |
| A edge_slope | 0.5 | **0.15** |
| A max_edge | 0.12 | **0.35** |
| A reject fair clamp | true | **false** |
| B move threshold | 0.10% | **0.08%** |
| B max mid | 0.65 | **0.75** |
| B min seconds | 45 | **30** |
| max open markets | 4 | **6** |
| max spread | 0.05 | **0.08** |
| paper fee bps | 150 | **100** |

**仍保留：** dry_run、one_order_per_strategy、btc+eth 5m only。

預期：單量會變多，paper PnL **可能變吵或變虧**——這是為了收集樣本，不是為了立刻「看起來賺」。

---

## 可選：清紙上帳本重來

若你想「大膽版從 0 統計」（不混舊 strict 資料）：

```bash
sudo systemctl stop polymarket-bot

# 危險：會清空 paper 歷史
psql "$DATABASE_URL" -c "
TRUNCATE bot_logs, pnl_ledger, price_snapshots, trades, positions, orders, markets
RESTART IDENTITY CASCADE;
"

sudo systemctl start polymarket-bot
```

**不建議**除非你確定要重新計分。一般可直接接著跑，用 `settled_at` 過濾新區間即可。

---

## 更新後檢查清單

- [ ] `git pull` 成功  
- [ ] `go build -o bot .` 無錯誤，`bot` 時間戳是新的  
- [ ] `.env` 的 `CONFIG_PATH` 指向 bold / ubuntu yaml  
- [ ] `DB_DRIVER=postgres` 且 `DATABASE_URL` 正確  
- [ ] `DRY_RUN=true`  
- [ ] `systemctl restart`（或 stop → start）  
- [ ] journal 有 `bot_start` 且 assets/timeframes/db_driver 正確  
- [ ] 跑一陣子後 `orders` / `pnl_ledger` 有新列  

```bash
psql "$DATABASE_URL" -c "
SELECT ts, detail FROM bot_logs WHERE event='bot_start' ORDER BY ts DESC LIMIT 3;
SELECT COUNT(*) AS orders_today FROM orders WHERE created_at > NOW() - INTERVAL '1 day';
"
```

---

## 之後若要收回「嚴格版」

```bash
# .env
CONFIG_PATH=configs/config.ubuntu.strict.yaml

sudo systemctl restart polymarket-bot
```

不必重編 binary（只換 yaml），但 **restart 必要**。

---

## 最短指令摘要

```bash
cd ~/polymarket-bot
sudo systemctl stop polymarket-bot
git pull
go build -o bot .
# 確認 .env: CONFIG_PATH=configs/config.ubuntu.yaml  DRY_RUN=true  DB_DRIVER=postgres
sudo systemctl start polymarket-bot
sudo journalctl -u polymarket-bot -f
```
