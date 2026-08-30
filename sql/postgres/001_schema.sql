CREATE TABLE IF NOT EXISTS runs (
    id TEXT PRIMARY KEY,
    started_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,
    run_mode TEXT NOT NULL,
    strategy_id TEXT NOT NULL,
    strategy_source TEXT NOT NULL,
    config_json JSONB NOT NULL DEFAULT '{}',
    hostname TEXT,
    git_sha TEXT,
    max_windows INT
);

CREATE TABLE IF NOT EXISTS markets (
    slug TEXT PRIMARY KEY,
    asset TEXT NOT NULL,
    condition_id TEXT,
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    up_token_id TEXT,
    down_token_id TEXT,
    min_order_size NUMERIC,
    tick_size NUMERIC,
    fee_rate NUMERIC,
    seconds_delay NUMERIC,
    resolution_source TEXT
);

CREATE TABLE IF NOT EXISTS price_ticks (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    market_slug TEXT,
    source TEXT NOT NULL,
    ts TIMESTAMPTZ NOT NULL,
    price NUMERIC NOT NULL
);

CREATE TABLE IF NOT EXISTS signals (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    strategy_id TEXT NOT NULL,
    strategy_source TEXT NOT NULL,
    run_mode TEXT NOT NULL,
    market_slug TEXT NOT NULL,
    side TEXT,
    token_id TEXT,
    confidence NUMERIC,
    score NUMERIC,
    features_json JSONB,
    reason TEXT,
    pm_price NUMERIC,
    binance_price NUMERIC,
    window_open_px NUMERIC,
    seconds_left NUMERIC,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS rejected_signals (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    strategy_id TEXT NOT NULL,
    strategy_source TEXT NOT NULL,
    run_mode TEXT NOT NULL,
    market_slug TEXT,
    reason_code TEXT NOT NULL,
    reason TEXT,
    features_json JSONB,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS orders (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    strategy_id TEXT NOT NULL,
    strategy_source TEXT NOT NULL,
    run_mode TEXT NOT NULL,
    market_slug TEXT NOT NULL,
    side TEXT NOT NULL,
    token_id TEXT NOT NULL,
    intended_price NUMERIC,
    intended_shares NUMERIC,
    limit_price NUMERIC,
    notional_usdc NUMERIC,
    is_min_size BOOLEAN,
    status TEXT NOT NULL,
    clob_order_id TEXT,
    request_json JSONB,
    response_json JSONB,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (run_id, market_slug)
);

CREATE TABLE IF NOT EXISTS fills (
    id TEXT PRIMARY KEY,
    order_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    market_slug TEXT,
    fill_model TEXT NOT NULL,
    price NUMERIC NOT NULL,
    shares NUMERIC NOT NULL,
    fee_usdc NUMERIC,
    fee_rate NUMERIC,
    liquidity_flag TEXT,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS resolutions (
    id TEXT PRIMARY KEY,
    market_slug TEXT NOT NULL,
    source TEXT NOT NULL,
    winner TEXT,
    price_to_beat NUMERIC,
    final_price NUMERIC,
    resolved_at TIMESTAMPTZ,
    raw_json JSONB,
    UNIQUE (market_slug, source)
);

CREATE TABLE IF NOT EXISTS positions (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    market_slug TEXT NOT NULL,
    side TEXT,
    shares NUMERIC,
    avg_price NUMERIC,
    UNIQUE (run_id, market_slug)
);

CREATE TABLE IF NOT EXISTS pnl_ledger (
    id TEXT PRIMARY KEY,
    order_id TEXT,
    fill_id TEXT,
    run_id TEXT NOT NULL,
    run_mode TEXT NOT NULL,
    strategy_id TEXT NOT NULL,
    market_slug TEXT NOT NULL,
    shares NUMERIC,
    entry_price NUMERIC,
    exit_value NUMERIC,
    fee_usdc NUMERIC,
    pnl_usdc NUMERIC,
    is_win BOOLEAN,
    fill_model TEXT
);
