CREATE TABLE IF NOT EXISTS runs (
    id TEXT PRIMARY KEY,
    started_at TIMESTAMP NOT NULL,
    ended_at TIMESTAMP,
    run_mode TEXT NOT NULL,
    strategy_id TEXT NOT NULL,
    strategy_source TEXT NOT NULL,
    config_json JSON NOT NULL DEFAULT '{}',
    hostname TEXT,
    git_sha TEXT,
    max_windows INT
);

CREATE TABLE IF NOT EXISTS markets (
    slug TEXT PRIMARY KEY,
    asset TEXT NOT NULL,
    condition_id TEXT,
    window_start TIMESTAMP NOT NULL,
    window_end TIMESTAMP NOT NULL,
    up_token_id TEXT,
    down_token_id TEXT,
    min_order_size DECIMAL,
    tick_size DECIMAL,
    fee_rate DECIMAL,
    seconds_delay DECIMAL,
    resolution_source TEXT
);

CREATE TABLE IF NOT EXISTS price_ticks (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    market_slug TEXT,
    source TEXT NOT NULL,
    ts TIMESTAMP NOT NULL,
    price DECIMAL NOT NULL
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
    confidence DECIMAL,
    score DECIMAL,
    features_json JSON,
    reason TEXT,
    pm_price DECIMAL,
    binance_price DECIMAL,
    window_open_px DECIMAL,
    seconds_left DECIMAL,
    created_at TIMESTAMP NOT NULL
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
    features_json JSON,
    created_at TIMESTAMP NOT NULL
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
    intended_price DECIMAL,
    intended_shares DECIMAL,
    limit_price DECIMAL,
    notional_usdc DECIMAL,
    is_min_size BOOLEAN,
    status TEXT NOT NULL,
    clob_order_id TEXT,
    request_json JSON,
    response_json JSON,
    created_at TIMESTAMP NOT NULL,
    UNIQUE (run_id, market_slug)
);

CREATE TABLE IF NOT EXISTS fills (
    id TEXT PRIMARY KEY,
    order_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    market_slug TEXT,
    fill_model TEXT NOT NULL,
    price DECIMAL NOT NULL,
    shares DECIMAL NOT NULL,
    fee_usdc DECIMAL,
    fee_rate DECIMAL,
    liquidity_flag TEXT,
    created_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS resolutions (
    id TEXT PRIMARY KEY,
    market_slug TEXT NOT NULL,
    source TEXT NOT NULL,
    winner TEXT,
    price_to_beat DECIMAL,
    final_price DECIMAL,
    resolved_at TIMESTAMP,
    raw_json JSON,
    UNIQUE (market_slug, source)
);

CREATE TABLE IF NOT EXISTS positions (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    market_slug TEXT NOT NULL,
    side TEXT,
    shares DECIMAL,
    avg_price DECIMAL,
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
    shares DECIMAL,
    entry_price DECIMAL,
    exit_value DECIMAL,
    fee_usdc DECIMAL,
    pnl_usdc DECIMAL,
    is_win BOOLEAN,
    fill_model TEXT
);
