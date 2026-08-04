# Polymarket 5m / 15m Crypto Auto Trading Bot

Long-running **Go** bot for Polymarket **BTC / ETH / SOL** **5-minute** and **15-minute** Up or Down markets.

Default: **BTC 5m + BTC 15m**, **`dry_run: true`** (no real orders).

## Strategies

| Strategy | Idea | Size |
|----------|------|------|
| **A – stable small** | Buy when mid is in **0.60–0.70** and fair value edge ≥ `min_edge` | **1–3 USD** |
| **B – lag harvest** | Spot already moved (≥ ~0.25%) but Polymarket odds still lag | **≤ 5 USD** |

Orders are **limit GTC** (Strategy A prefers post-only / maker-ish prices). Positions are held to settlement.

## Requirements

- **Go 1.24+** (CLOB SDK requires it; older 1.22 is not enough)
- **CGO + C compiler** for DuckDB (`build-essential` / `gcc` on Ubuntu)
- Network access to:
  - `gamma-api.polymarket.com`
  - `clob.polymarket.com` (live trading)
  - Binance WebSocket `stream.binance.com`

## Project layout

```text
cmd/bot/main.go          # entrypoint
internal/
  config/                # yaml + .env
  discovery/             # slug windows + Gamma API
  price/                 # Binance WS
  fairvalue/             # simple P(up)/P(down)
  strategy/              # A + B
  risk/                  # caps + loss circuit breakers
  execution/             # CLOB V2 place/cancel
  store/                 # DuckDB
  monitor/               # slog + bot_logs + webhook
configs/config.yaml
db/schema_duckdb.sql
db/init_postgresql.sql   # future Postgres migration
systemd/polymarket-bot.service
.env.example
```

## Quick start

```bash
# 1. clone & enter repo
cd polymarket

# 2. secrets
cp .env.example .env
# edit .env — PRIVATE_KEY only required when dry_run is false

# 3. deps
go mod tidy

# 4. build
go build -o bot ./cmd/bot

# 5. run (dry-run by default)
./bot
```

On Windows (PowerShell):

```powershell
go mod tidy
go build -o bot.exe ./cmd/bot
.\bot.exe
```

### Config

Edit `configs/config.yaml`:

- `assets` / `timeframes` — default `btc` + `5m`/`15m`
- `clob.dry_run` — keep `true` until you are ready
- `strategy_a` / `strategy_b` / `risk` — sizing and safety

Environment overrides (see `.env.example`):

| Variable | Purpose |
|----------|---------|
| `PRIVATE_KEY` | Wallet key for signing |
| `CLOB_API_KEY` / `CLOB_SECRET` / `CLOB_PASSPHRASE` | Optional L2 creds (auto-derived if empty) |
| `SIGNATURE_TYPE` | 0=EOA, 1=Proxy, 2=Safe, 3=Deposit |
| `FUNDER` | Proxy/deposit funder address |
| `DRY_RUN` | `true`/`false` override |
| `WEBHOOK_URL` | Alert on circuit breaker / errors |
| `CONFIG_PATH` | Alternate config path |

## Market discovery

Windows are aligned to Unix boundaries:

- **5m** → floor to 300s  
- **15m** → floor to 900s  

Primary slug (verified live):

```text
btc-updown-5m-{window_start_ts}
btc-updown-15m-{window_start_ts}
```

Fallback patterns (longer historical names) and keyword search are tried if the primary slug misses.

Gamma: `GET https://gamma-api.polymarket.com/events?slug=...`

## Risk controls

- Max position USD per market  
- Max concurrent open markets  
- Strategy A hard band 1–3 USD; Strategy B ≤ 5 USD  
- Daily / hourly loss circuit breakers (`pnl_ledger`)  
- Prefer limit orders; hard min seconds left before expiry  

## Data

DuckDB file: `data/bot.duckdb` (configurable).

Tables: `markets`, `orders`, `trades`, `positions`, `price_snapshots`, `bot_logs`, `pnl_ledger`.

PostgreSQL DDL (for later migration): `db/init_postgresql.sql`.

## systemd (Ubuntu)

```bash
# build on the server
go build -o bot ./cmd/bot

# install
sudo cp systemd/polymarket-bot.service /etc/systemd/system/
# edit User, Group, WorkingDirectory, ExecStart, EnvironmentFile
sudo systemctl daemon-reload
sudo systemctl enable --now polymarket-bot
journalctl -u polymarket-bot -f
```

## CLOB client

Uses [`github.com/0xNetuser/Polymarket-golang`](https://github.com/0xNetuser/Polymarket-golang) (V2 orders).

## Important caveats

1. **Resolution source is Chainlink**; the bot’s open reference price is **Binance mid at first sighting** of the window → small basis risk on Strategy B.  
2. **Slug formats can change** — multi-pattern discovery mitigates but does not eliminate this.  
3. **Default is dry-run** — set `clob.dry_run: false` and fund the wallet only after you understand the risk.  
4. This is **not financial advice**. You can lose money. Start tiny.  
5. DuckDB requires **CGO**; install a C toolchain on the host.

## Development

```bash
go test ./...
go build -o bot ./cmd/bot
```

## License

Use at your own risk.
