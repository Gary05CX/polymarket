# polymarket 5m bot

Go port of two public 5-minute Up/Down strategies. Default: **dry-run → PostgreSQL**.

```bash
cp config.example.yaml config.yaml
# DSN 預設 Unix socket: postgres:///polymarket_bot?host=/var/run/postgresql
go run ./cmd/bot -config config.yaml -mode dry_run -max-windows 1
```

- Strategies: `window_delta` (`jmazzini/5m-poly-bot`), `inertia_3m` (`ratrimaa/btc-5m-polymarket-bot`)
- Dry-run uses real Gamma/CLOB/Binance data, walks the book, does **not** send orders
- After `max_windows` (default 1) it waits for Gamma official resolution, writes `pnl_ledger`, then exits
- Live CLOB V2 is **not implemented** yet
- DuckDB SQL 在 `sql/duckdb/`；預設 binary 只連 Postgres（這台的主庫）

systemd（這台 Ubuntu，user service，dry-run 常駐）：

```bash
go build -o bot ./cmd/bot
cp deploy/polymarket-bot.service deploy/polymarket-bot-inertia.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now polymarket-bot polymarket-bot-inertia
# 比成績：
psql -d polymarket_bot -c 'SELECT * FROM v_strategy_performance;'
```

Not financial advice.
