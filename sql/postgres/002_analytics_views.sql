CREATE OR REPLACE VIEW v_strategy_performance AS
SELECT
    p.strategy_id,
    p.run_mode,
    p.fill_model,
    count(*) AS n,
    avg(CASE WHEN p.is_win THEN 1.0 ELSE 0.0 END) AS win_rate,
    avg(p.pnl_usdc) AS avg_pnl,
    sum(p.pnl_usdc) AS pnl
FROM pnl_ledger p
JOIN orders o ON o.id = p.order_id
JOIN resolutions r ON r.market_slug = p.market_slug AND r.source = 'official_gamma'
WHERE r.winner IS NOT NULL AND r.winner <> 'pending'
GROUP BY 1, 2, 3;

CREATE OR REPLACE VIEW v_signal_funnel AS
SELECT strategy_id, run_mode, reason_code, count(*) AS n
FROM rejected_signals
GROUP BY 1, 2, 3;

CREATE OR REPLACE VIEW v_calibration AS
SELECT
    s.strategy_id,
    s.run_mode,
    width_bucket(s.confidence, 0, 1, 10) AS conf_bucket,
    count(*) AS n,
    avg(CASE WHEN p.is_win THEN 1.0 ELSE 0.0 END) AS win_rate
FROM signals s
LEFT JOIN pnl_ledger p ON p.market_slug = s.market_slug AND p.run_id = s.run_id
GROUP BY 1, 2, 3;

CREATE OR REPLACE VIEW v_timing AS
SELECT
    s.strategy_id,
    s.run_mode,
    round(s.seconds_left) AS seconds_left,
    count(*) AS n,
    avg(CASE WHEN p.is_win THEN 1.0 ELSE 0.0 END) AS win_rate
FROM signals s
LEFT JOIN pnl_ledger p ON p.market_slug = s.market_slug AND p.run_id = s.run_id
GROUP BY 1, 2, 3;

CREATE OR REPLACE VIEW v_provisional_vs_official AS
SELECT
    o.market_slug,
    o.winner AS official_winner,
    p.winner AS provisional_winner,
    (o.winner IS DISTINCT FROM p.winner) AS disagree
FROM resolutions o
LEFT JOIN resolutions p
  ON p.market_slug = o.market_slug AND p.source = 'provisional_twap'
WHERE o.source = 'official_gamma';
