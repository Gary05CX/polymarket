-- Manual / reference migration: orders.dry_run + orders.error_message
-- Bot also applies these on start (openPostgres / openDuckDB).
-- Safe to re-run on PostgreSQL.

ALTER TABLE orders ADD COLUMN IF NOT EXISTS dry_run BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS error_message TEXT;
CREATE INDEX IF NOT EXISTS idx_orders_dry_run ON orders(dry_run);

-- Backfill historical rows (best-effort)
UPDATE orders SET dry_run = FALSE
WHERE dry_run IS TRUE
  AND (
    status IN ('error', 'cancelled', 'live', 'pending', 'open', 'filled', 'submitted', 'settled', 'matched')
    OR (clob_order_id IS NOT NULL AND clob_order_id <> '' AND clob_order_id NOT LIKE 'dry-%')
  );

UPDATE orders SET dry_run = TRUE
WHERE status LIKE 'dry%';
