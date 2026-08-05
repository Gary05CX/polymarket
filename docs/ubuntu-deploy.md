# Ubuntu deploy (short)

**完整中文步驟（建議閱讀）：** [ubuntu-setup.md](./ubuntu-setup.md)

Quick English checklist:

1. Install Go 1.24+, PostgreSQL, git  
2. Create role/DB `polymarket`  
3. `git clone` → `go build -o bot .`  
4. `.env`:

```bash
CONFIG_PATH=configs/config.ubuntu.yaml
DB_DRIVER=postgres
DATABASE_URL=postgres://polymarket:SECRET@127.0.0.1:5432/polymarket?sslmode=disable
DRY_RUN=true
```

5. Install `systemd/polymarket-bot.service` (replace `YOUR_USER`)  
6. `systemctl enable --now polymarket-bot`  
7. `journalctl -u polymarket-bot -f` — expect `"db_driver":"postgres"`

Switch backends with `DB_DRIVER=duckdb|postgres` in `.env` (no data migration).
