-- DuckDB schema for polymarket-bot
-- Amounts stored as VARCHAR (decimal strings) to avoid float error.

CREATE TABLE IF NOT EXISTS markets (
    slug            VARCHAR PRIMARY KEY,
    condition_id    VARCHAR NOT NULL,
    up_token_id     VARCHAR NOT NULL,
    down_token_id   VARCHAR NOT NULL,
    asset           VARCHAR NOT NULL,
    timeframe       VARCHAR NOT NULL,
    window_start    BIGINT NOT NULL,
    end_time        TIMESTAMP,
    event_start     TIMESTAMP,
    open_price      VARCHAR,
    close_price     VARCHAR,
    title           VARCHAR,
    settled_at      TIMESTAMP,
    settle_outcome  VARCHAR,
    settle_pnl_usd  VARCHAR,
    discovered_at   TIMESTAMP DEFAULT current_timestamp,
    updated_at      TIMESTAMP DEFAULT current_timestamp
);

CREATE TABLE IF NOT EXISTS orders (
    id              VARCHAR PRIMARY KEY,
    market_slug     VARCHAR NOT NULL,
    token_id        VARCHAR NOT NULL,
    strategy        VARCHAR NOT NULL,
    side            VARCHAR NOT NULL,
    price           VARCHAR NOT NULL,
    size            VARCHAR NOT NULL,
    size_usd        VARCHAR NOT NULL,
    status          VARCHAR NOT NULL,
    clob_order_id   VARCHAR,
    reason          VARCHAR,
    dry_run         BOOLEAN NOT NULL DEFAULT TRUE,
    error_message   VARCHAR,
    created_at      TIMESTAMP DEFAULT current_timestamp,
    updated_at      TIMESTAMP DEFAULT current_timestamp
);

CREATE TABLE IF NOT EXISTS trades (
    id              VARCHAR PRIMARY KEY,
    order_id        VARCHAR NOT NULL,
    market_slug     VARCHAR,
    token_id        VARCHAR,
    price           VARCHAR NOT NULL,
    size            VARCHAR NOT NULL,
    fee             VARCHAR,
    side            VARCHAR,
    ts              TIMESTAMP DEFAULT current_timestamp
);

CREATE TABLE IF NOT EXISTS positions (
    market_slug     VARCHAR NOT NULL,
    token_id        VARCHAR NOT NULL,
    size            VARCHAR NOT NULL,
    avg_price       VARCHAR NOT NULL,
    size_usd        VARCHAR NOT NULL,
    updated_at      TIMESTAMP DEFAULT current_timestamp,
    PRIMARY KEY (market_slug, token_id)
);

CREATE TABLE IF NOT EXISTS price_snapshots (
    id              BIGINT PRIMARY KEY,
    ts              TIMESTAMP DEFAULT current_timestamp,
    asset           VARCHAR NOT NULL,
    spot            VARCHAR,
    market_slug     VARCHAR,
    mid_up          VARCHAR,
    mid_down        VARCHAR,
    fair_up         VARCHAR,
    fair_down       VARCHAR,
    open_price      VARCHAR
);

CREATE SEQUENCE IF NOT EXISTS price_snapshots_id_seq START 1;

CREATE TABLE IF NOT EXISTS bot_logs (
    id              BIGINT PRIMARY KEY,
    ts              TIMESTAMP DEFAULT current_timestamp,
    level           VARCHAR NOT NULL,
    event           VARCHAR NOT NULL,
    detail          VARCHAR
);

CREATE SEQUENCE IF NOT EXISTS bot_logs_id_seq START 1;

CREATE TABLE IF NOT EXISTS pnl_ledger (
    id              BIGINT PRIMARY KEY,
    ts              TIMESTAMP DEFAULT current_timestamp,
    kind            VARCHAR NOT NULL,
    amount_usd      VARCHAR NOT NULL,
    detail          VARCHAR
);

CREATE SEQUENCE IF NOT EXISTS pnl_ledger_id_seq START 1;

CREATE INDEX IF NOT EXISTS idx_orders_market ON orders(market_slug);
CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);
CREATE INDEX IF NOT EXISTS idx_orders_dry_run ON orders(dry_run);
CREATE INDEX IF NOT EXISTS idx_snapshots_ts ON price_snapshots(ts);
CREATE INDEX IF NOT EXISTS idx_bot_logs_ts ON bot_logs(ts);
