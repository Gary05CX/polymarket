-- PostgreSQL schema mirror for polymarket-bot
-- Use when migrating off DuckDB to a stable Ubuntu + Postgres setup.

CREATE TABLE IF NOT EXISTS markets (
    slug            TEXT PRIMARY KEY,
    condition_id    TEXT NOT NULL,
    up_token_id     TEXT NOT NULL,
    down_token_id   TEXT NOT NULL,
    asset           TEXT NOT NULL,
    timeframe       TEXT NOT NULL,
    window_start    BIGINT NOT NULL,
    end_time        TIMESTAMPTZ,
    event_start     TIMESTAMPTZ,
    open_price      NUMERIC(36, 18),
    close_price     NUMERIC(36, 18),
    title           TEXT,
    settled_at      TIMESTAMPTZ,
    settle_outcome  TEXT,
    settle_pnl_usd  NUMERIC(36, 18),
    discovered_at   TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS orders (
    id              TEXT PRIMARY KEY,
    market_slug     TEXT NOT NULL,
    token_id        TEXT NOT NULL,
    strategy        TEXT NOT NULL,
    side            TEXT NOT NULL,
    price           NUMERIC(36, 18) NOT NULL,
    size            NUMERIC(36, 18) NOT NULL,
    size_usd        NUMERIC(36, 18) NOT NULL,
    status          TEXT NOT NULL,
    clob_order_id   TEXT,
    reason          TEXT,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS trades (
    id              TEXT PRIMARY KEY,
    order_id        TEXT NOT NULL REFERENCES orders(id),
    market_slug     TEXT,
    token_id        TEXT,
    price           NUMERIC(36, 18) NOT NULL,
    size            NUMERIC(36, 18) NOT NULL,
    fee             NUMERIC(36, 18),
    side            TEXT,
    ts              TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS positions (
    market_slug     TEXT NOT NULL,
    token_id        TEXT NOT NULL,
    size            NUMERIC(36, 18) NOT NULL,
    avg_price       NUMERIC(36, 18) NOT NULL,
    size_usd        NUMERIC(36, 18) NOT NULL,
    updated_at      TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (market_slug, token_id)
);

CREATE TABLE IF NOT EXISTS price_snapshots (
    id              BIGSERIAL PRIMARY KEY,
    ts              TIMESTAMPTZ DEFAULT NOW(),
    asset           TEXT NOT NULL,
    spot            NUMERIC(36, 18),
    market_slug     TEXT,
    mid_up          NUMERIC(36, 18),
    mid_down        NUMERIC(36, 18),
    fair_up         NUMERIC(36, 18),
    fair_down       NUMERIC(36, 18),
    open_price      NUMERIC(36, 18)
);

CREATE TABLE IF NOT EXISTS bot_logs (
    id              BIGSERIAL PRIMARY KEY,
    ts              TIMESTAMPTZ DEFAULT NOW(),
    level           TEXT NOT NULL,
    event           TEXT NOT NULL,
    detail          TEXT
);

CREATE TABLE IF NOT EXISTS pnl_ledger (
    id              BIGSERIAL PRIMARY KEY,
    ts              TIMESTAMPTZ DEFAULT NOW(),
    kind            TEXT NOT NULL,
    amount_usd      NUMERIC(36, 18) NOT NULL,
    detail          TEXT
);

CREATE INDEX IF NOT EXISTS idx_orders_market ON orders(market_slug);
CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);
CREATE INDEX IF NOT EXISTS idx_snapshots_ts ON price_snapshots(ts);
CREATE INDEX IF NOT EXISTS idx_bot_logs_ts ON bot_logs(ts);
CREATE INDEX IF NOT EXISTS idx_pnl_ts ON pnl_ledger(ts);
