# Hermes agent: 更新並重啟 polymarket-bot（Ubuntu）

給在 **Ubuntu 機器上的 agent** 執行。目標：拉取最新 code、編譯、套用 **half-tight** 設定、重啟 systemd。

---

## 背景（為何改）

Paper-bold 長跑結果（Postgres 分析）：

- 總 PnL 約 **−$14** / 153 settle（樣本夠）
- **BTC 5m 正**；**ETH 拖累**
- Strategy A 買價 **0.64–0.70** 嚴重虧；**≤0.64 偏正**
- B 高價 $5 波動大

### 本次 half-tight 變更

| 項目 | paper-bold | **half-tight（現預設）** |
|------|------------|--------------------------|
| A price_max | 0.70 | **0.64** |
| A reject_fair_at_clamp | false | **true** |
| B max_size_usd | 5 | **3** |
| B max_market_price | 0.75 | **0.62** |
| one_order_per_market | 無 | **true（A/B 互斥）** |
| 仍 dry_run / btc+eth / 5m only | ✓ | ✓ |

設定檔：

- `configs/config.ubuntu.yaml` ← **預設 = half-tight ~$1–3**
- `configs/config.ubuntu.half-tight.yaml` ← 同內容
- `configs/config.ubuntu.half-tight-size5.yaml` ← half-tight **~$5**（仍 dry_run；需 rebuild）
- `configs/config.ubuntu.live-size2-3.yaml` ← **~$2–3** + 日虧$5/時虧$3（$10 本金）
- `configs/config.ubuntu.paper-bold.yaml` ← 舊大膽版
- `configs/config.ubuntu.strict.yaml` ← 最嚴
- 真錢步驟：`docs/LIVE-CHECKLIST.md`

---

## 執行步驟（請依序）

假設專案在 `~/polymarket-bot`（若路徑不同請替換）。

### 1. 停服務

```bash
sudo systemctl stop polymarket-bot
sudo systemctl is-active polymarket-bot || true
# 應為 inactive
```

### 2. 拉取最新程式

```bash
cd ~/polymarket-bot
git fetch origin
git checkout dev/crypto/dev
git pull origin dev/crypto/dev
git log -1 --oneline
```

### 3. 重新編譯（必要 — pull 不會更新 binary）

```bash
cd ~/polymarket-bot
go version   # 需要 Go 1.24+
go mod tidy
go build -o bot .
ls -la bot
```

### 4. 確認 `.env`（不要覆蓋密碼；只補缺項）

```bash
cd ~/polymarket-bot
test -f .env || cp .env.example .env
chmod 600 .env

# 建議內容（密碼保留機器上現有 DATABASE_URL）：
grep -E '^(CONFIG_PATH|DB_DRIVER|DATABASE_URL|DRY_RUN|DUCKDB_PATH)=' .env || true
```

**必須/建議：**

```bash
CONFIG_PATH=configs/config.ubuntu.yaml
DB_DRIVER=postgres
DATABASE_URL=postgres://...existing...
DRY_RUN=true
```

若 `CONFIG_PATH` 仍指向 `paper-bold`，請改成：

```bash
# 編輯 .env
sed -i 's|CONFIG_PATH=.*|CONFIG_PATH=configs/config.ubuntu.yaml|' .env
# 或手動 nano .env
```

### 5. （僅當 systemd unit 有改時）重裝 unit

```bash
# 通常 half-tight 不需要；若 git pull 改了 systemd/polymarket-bot.service 才做
sudo cp ~/polymarket-bot/systemd/polymarket-bot.service /etc/systemd/system/polymarket-bot.service
sudo sed -i "s|YOUR_USER|$(whoami)|g" /etc/systemd/system/polymarket-bot.service
# 確認 WorkingDirectory / ExecStart / EnvironmentFile 路徑正確
grep -E 'User=|WorkingDirectory=|ExecStart=|EnvironmentFile=|CONFIG_PATH' /etc/systemd/system/polymarket-bot.service
sudo systemctl daemon-reload
```

### 6. 啟動並驗證

```bash
sudo systemctl start polymarket-bot
sudo systemctl status polymarket-bot --no-pager
sudo journalctl -u polymarket-bot -n 100 --no-pager
```

**成功跡象（log 中）：**

```text
bot_start
"assets":["btc","eth"]
"timeframes":["5m"]
"dry_run":true
"db_driver":"postgres"
entering main loop
```

**失敗時：**

```bash
sudo journalctl -u polymarket-bot -n 200 --no-pager
# 常見：DATABASE_URL 錯、bot 路徑錯、.env 權限、Go 編譯失敗
```

### 7. 可選：確認 DB 有新活動（幾分鐘後）

```bash
# 從 .env 載入 URL
set -a; source ~/polymarket-bot/.env; set +a
psql "$DATABASE_URL" -c "SELECT ts, detail FROM bot_logs WHERE event='bot_start' ORDER BY ts DESC LIMIT 2;"
psql "$DATABASE_URL" -c "SELECT COUNT(*) FROM orders WHERE created_at > NOW() - INTERVAL '15 minutes';"
```

---

## 一鍵腳本（agent 可整段執行）

```bash
#!/usr/bin/env bash
set -euo pipefail
APP="${HOME}/polymarket-bot"
cd "$APP"

sudo systemctl stop polymarket-bot || true
git fetch origin
git checkout dev/crypto/dev
git pull origin dev/crypto/dev
go mod tidy
go build -o bot .

# ensure CONFIG_PATH if missing
if ! grep -q '^CONFIG_PATH=' .env 2>/dev/null; then
  echo 'CONFIG_PATH=configs/config.ubuntu.yaml' >> .env
fi
# force half-tight default path (comment out if you intentionally use another profile)
grep -q '^CONFIG_PATH=' .env && sed -i 's|^CONFIG_PATH=.*|CONFIG_PATH=configs/config.ubuntu.yaml|' .env

sudo systemctl start polymarket-bot
sleep 2
sudo systemctl is-active polymarket-bot
sudo journalctl -u polymarket-bot -n 40 --no-pager
```

---

## 不要做的事

- ❌ 只 `git pull` 不 `go build`  
- ❌ 不 `restart`/`start`（yaml 不會生效）  
- ❌ 用 `git clean` 刪掉 `.env`  
- ❌ 在未確認 dry_run 時改 `DRY_RUN=false`  
- ❌ 清空 Postgres 除非人類明確要求  

---

## 回報人類時建議貼

1. `git log -1 --oneline`  
2. `systemctl is-active polymarket-bot`  
3. `journalctl` 裡最近一條 `bot_start` 的 detail  
4. 若有錯：最後 30 行 error log  

---

## 相關文件

| 文件 | 用途 |
|------|------|
| `docs/HERMES-UPDATE-AND-RESTART.md` | **本文件（agent 更新用）** |
| `docs/update-on-ubuntu.md` | 更新原理說明 |
| `docs/ubuntu-setup.md` | 從零安裝 |
