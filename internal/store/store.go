// Package store provides DuckDB or PostgreSQL persistence for the bot.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Store wraps a SQL database (DuckDB or PostgreSQL).
type Store struct {
	db      *sql.DB
	dialect dialect
}

// Market is a discovered up/down market window.
type Market struct {
	Slug          string
	ConditionID   string
	UpTokenID     string
	DownTokenID   string
	Asset         string
	Timeframe     string
	WindowStart   int64
	EndTime       *time.Time
	EventStart    *time.Time
	OpenPrice     *string
	Title         string
	SettledAt     *time.Time
	SettleOutcome string
	SettlePnLUSD  string
	DiscoveredAt  time.Time
	UpdatedAt     time.Time
}

// Order is a bot-placed order record.
type Order struct {
	ID          string
	MarketSlug  string
	TokenID     string
	Strategy    string
	Side        string
	Price       string
	Size        string
	SizeUSD     string
	Status      string
	CLOBOrderID string
	Reason      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Position is current exposure on a token.
type Position struct {
	MarketSlug string
	TokenID    string
	Size       string
	AvgPrice   string
	SizeUSD    string
	UpdatedAt  time.Time
}

// Snapshot is a price observation row.
type Snapshot struct {
	Asset      string
	Spot       string
	MarketSlug string
	MidUp      string
	MidDown    string
	FairUp     string
	FairDown   string
	OpenPrice  string
}

// Close closes the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	if s.dialect == dialectDuckDB {
		_, _ = s.db.Exec(`CHECKPOINT`)
	}
	return s.db.Close()
}

// Checkpoint flushes DuckDB WAL; no-op for PostgreSQL.
func (s *Store) Checkpoint(ctx context.Context) error {
	if s == nil || s.dialect != dialectDuckDB {
		return nil
	}
	_, err := s.exec(ctx, `CHECKPOINT`)
	return err
}

// DriverName returns "duckdb" or "postgres".
func (s *Store) DriverName() string {
	if s != nil && s.dialect == dialectPostgres {
		return "postgres"
	}
	return "duckdb"
}

// DB exposes the underlying *sql.DB (tests / advanced use).
func (s *Store) DB() *sql.DB { return s.db }

// UpsertMarket inserts or updates a market row.
func (s *Store) UpsertMarket(ctx context.Context, m Market) error {
	const q = `
INSERT INTO markets (
  slug, condition_id, up_token_id, down_token_id, asset, timeframe,
  window_start, end_time, event_start, open_price, title, discovered_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (slug) DO UPDATE SET
  condition_id = excluded.condition_id,
  up_token_id = excluded.up_token_id,
  down_token_id = excluded.down_token_id,
  end_time = excluded.end_time,
  event_start = excluded.event_start,
  open_price = COALESCE(markets.open_price, excluded.open_price),
  title = excluded.title,
  updated_at = excluded.updated_at
`
	now := time.Now().UTC()
	discovered := now
	if !m.DiscoveredAt.IsZero() {
		discovered = m.DiscoveredAt.UTC()
	}
	var endTime, eventStart any
	if m.EndTime != nil {
		endTime = m.EndTime.UTC()
	}
	if m.EventStart != nil {
		eventStart = m.EventStart.UTC()
	}
	var open any
	if m.OpenPrice != nil {
		open = *m.OpenPrice
	}
	_, err := s.exec(ctx, q,
		m.Slug, m.ConditionID, m.UpTokenID, m.DownTokenID, m.Asset, m.Timeframe,
		m.WindowStart, endTime, eventStart, open, m.Title, discovered, now,
	)
	return err
}

// SetOpenPrice sets open_price only if currently null.
func (s *Store) SetOpenPrice(ctx context.Context, slug, openPrice string) error {
	q := `
UPDATE markets SET open_price = ?, updated_at = ?
WHERE slug = ? AND open_price IS NULL`
	// DuckDB may store decimals as VARCHAR historically.
	if s.dialect == dialectDuckDB {
		q = `
UPDATE markets SET open_price = ?, updated_at = ?
WHERE slug = ? AND (open_price IS NULL OR open_price = '')`
	}
	_, err := s.exec(ctx, q, openPrice, time.Now().UTC(), slug)
	return err
}

// GetMarket loads a market by slug.
func (s *Store) GetMarket(ctx context.Context, slug string) (*Market, error) {
	const q = `
SELECT slug, condition_id, up_token_id, down_token_id, asset, timeframe,
       window_start, end_time, event_start, open_price, title, discovered_at, updated_at
FROM markets WHERE slug = ?
`
	row := s.queryRow(ctx, q, slug)
	var m Market
	var endTime, eventStart, discovered, updated sql.NullTime
	var open sql.NullString
	if err := row.Scan(
		&m.Slug, &m.ConditionID, &m.UpTokenID, &m.DownTokenID, &m.Asset, &m.Timeframe,
		&m.WindowStart, &endTime, &eventStart, &open, &m.Title, &discovered, &updated,
	); err != nil {
		return nil, err
	}
	if endTime.Valid {
		t := endTime.Time
		m.EndTime = &t
	}
	if eventStart.Valid {
		t := eventStart.Time
		m.EventStart = &t
	}
	if open.Valid {
		v := open.String
		m.OpenPrice = &v
	}
	if discovered.Valid {
		m.DiscoveredAt = discovered.Time
	}
	if updated.Valid {
		m.UpdatedAt = updated.Time
	}
	return &m, nil
}

// InsertOrder stores a new order.
func (s *Store) InsertOrder(ctx context.Context, o Order) error {
	if o.ID == "" {
		o.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	const q = `
INSERT INTO orders (
  id, market_slug, token_id, strategy, side, price, size, size_usd,
  status, clob_order_id, reason, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`
	_, err := s.exec(ctx, q,
		o.ID, o.MarketSlug, o.TokenID, o.Strategy, o.Side, o.Price, o.Size, o.SizeUSD,
		o.Status, nullStr(o.CLOBOrderID), nullStr(o.Reason), now, now,
	)
	return err
}

// UpdateOrderStatus updates status / clob id.
func (s *Store) UpdateOrderStatus(ctx context.Context, id, status, clobOrderID string) error {
	_, err := s.exec(ctx, `
UPDATE orders SET status = ?, clob_order_id = COALESCE(?, clob_order_id), updated_at = ?
WHERE id = ?
`, status, nullStr(clobOrderID), time.Now().UTC(), id)
	return err
}

// ListOpenOrders returns non-terminal orders.
func (s *Store) ListOpenOrders(ctx context.Context) ([]Order, error) {
	const q = `
SELECT id, market_slug, token_id, strategy, side, price, size, size_usd,
       status, clob_order_id, reason, created_at, updated_at
FROM orders
WHERE status IN ('pending', 'live', 'open', 'dry_run')
ORDER BY created_at
`
	rows, err := s.query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		var o Order
		var clob, reason sql.NullString
		var created, updated sql.NullTime
		if err := rows.Scan(
			&o.ID, &o.MarketSlug, &o.TokenID, &o.Strategy, &o.Side, &o.Price, &o.Size, &o.SizeUSD,
			&o.Status, &clob, &reason, &created, &updated,
		); err != nil {
			return nil, err
		}
		if clob.Valid {
			o.CLOBOrderID = clob.String
		}
		if reason.Valid {
			o.Reason = reason.String
		}
		if created.Valid {
			o.CreatedAt = created.Time
		}
		if updated.Valid {
			o.UpdatedAt = updated.Time
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// PositionUSD returns total size_usd for a market.
func (s *Store) PositionUSD(ctx context.Context, marketSlug string) (decimal.Decimal, error) {
	row := s.queryRow(ctx, `
SELECT COALESCE(SUM(`+s.castFloat("size_usd")+`), 0) FROM positions WHERE market_slug = ?
`, marketSlug)
	var v float64
	if err := row.Scan(&v); err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromFloat(v), nil
}

// UpsertPosition merges fill exposure into positions.
func (s *Store) UpsertPosition(ctx context.Context, marketSlug, tokenID string, addSize, price, addUSD decimal.Decimal) error {
	existing, err := s.GetPosition(ctx, marketSlug, tokenID)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if existing == nil {
		return s.insertPosition(ctx, marketSlug, tokenID, addSize, price, addUSD)
	}
	oldSize, _ := decimal.NewFromString(existing.Size)
	oldAvg, _ := decimal.NewFromString(existing.AvgPrice)
	oldUSD, _ := decimal.NewFromString(existing.SizeUSD)
	newSize := oldSize.Add(addSize)
	newUSD := oldUSD.Add(addUSD)
	var newAvg decimal.Decimal
	if newSize.IsZero() {
		newAvg = decimal.Zero
	} else {
		// weighted avg price
		newAvg = oldAvg.Mul(oldSize).Add(price.Mul(addSize)).Div(newSize)
	}
	_, err = s.exec(ctx, `
UPDATE positions SET size = ?, avg_price = ?, size_usd = ?, updated_at = ?
WHERE market_slug = ? AND token_id = ?
`, newSize.String(), newAvg.String(), newUSD.String(), time.Now().UTC(), marketSlug, tokenID)
	return err
}

func (s *Store) insertPosition(ctx context.Context, marketSlug, tokenID string, size, price, usd decimal.Decimal) error {
	_, err := s.exec(ctx, `
INSERT INTO positions (market_slug, token_id, size, avg_price, size_usd, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
`, marketSlug, tokenID, size.String(), price.String(), usd.String(), time.Now().UTC())
	return err
}

// GetPosition loads one position.
func (s *Store) GetPosition(ctx context.Context, marketSlug, tokenID string) (*Position, error) {
	row := s.queryRow(ctx, `
SELECT market_slug, token_id, size, avg_price, size_usd, updated_at
FROM positions WHERE market_slug = ? AND token_id = ?
`, marketSlug, tokenID)
	var p Position
	var updated sql.NullTime
	if err := row.Scan(&p.MarketSlug, &p.TokenID, &p.Size, &p.AvgPrice, &p.SizeUSD, &updated); err != nil {
		return nil, err
	}
	if updated.Valid {
		p.UpdatedAt = updated.Time
	}
	return &p, nil
}

// CountOpenMarkets counts *active* (not settled, not expired) markets with exposure.
func (s *Store) CountOpenMarkets(ctx context.Context, now time.Time) (int, error) {
	// dry_run exposure lives in positions only (avoid double-count via orders).
	cf := s.castFloat("size_usd")
	row := s.queryRow(ctx, `
SELECT COUNT(DISTINCT x.market_slug) FROM (
  SELECT market_slug FROM positions WHERE `+cf+` > 0
  UNION
  SELECT market_slug FROM orders WHERE status IN ('pending', 'live', 'open')
) x
INNER JOIN markets m ON m.slug = x.market_slug
WHERE m.settled_at IS NULL
  AND (m.end_time IS NULL OR m.end_time > ?)
`, now.UTC())
	var n int
	err := row.Scan(&n)
	return n, err
}

// HasStrategyOrder reports whether this market already has a non-failed order for strategy.
func (s *Store) HasStrategyOrder(ctx context.Context, marketSlug, strategy string) (bool, error) {
	row := s.queryRow(ctx, `
SELECT COUNT(*) FROM orders
WHERE market_slug = ? AND strategy = ?
  AND status IN ('pending', 'live', 'open', 'dry_run', 'dry_filled', 'dry_settled', 'matched')
`, marketSlug, strategy)
	var n int
	if err := row.Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListPositions returns all positions for a market.
func (s *Store) ListPositions(ctx context.Context, marketSlug string) ([]Position, error) {
	rows, err := s.query(ctx, `
SELECT market_slug, token_id, size, avg_price, size_usd, updated_at
FROM positions WHERE market_slug = ? AND `+s.castFloat("size_usd")+` > 0
`, marketSlug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Position
	for rows.Next() {
		var p Position
		var updated sql.NullTime
		if err := rows.Scan(&p.MarketSlug, &p.TokenID, &p.Size, &p.AvgPrice, &p.SizeUSD, &updated); err != nil {
			return nil, err
		}
		if updated.Valid {
			p.UpdatedAt = updated.Time
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ClearPositions deletes all positions for a market.
func (s *Store) ClearPositions(ctx context.Context, marketSlug string) error {
	_, err := s.exec(ctx, `DELETE FROM positions WHERE market_slug = ?`, marketSlug)
	return err
}

// MarkOrdersStatus sets status for all orders of a market matching fromStatuses.
func (s *Store) MarkOrdersStatus(ctx context.Context, marketSlug, newStatus string, from []string) error {
	if len(from) == 0 {
		return nil
	}
	placeholders := make([]string, len(from))
	args := []any{newStatus, time.Now().UTC()}
	for i, st := range from {
		placeholders[i] = "?"
		args = append(args, st)
	}
	args = append(args, marketSlug)
	q := fmt.Sprintf(`
UPDATE orders SET status = ?, updated_at = ?
WHERE status IN (%s) AND market_slug = ?
`, strings.Join(placeholders, ","))
	_, err := s.exec(ctx, q, args...)
	return err
}

// ListMarketsDueForSettle returns markets past end_time that are not yet settled
// and have positions or dry/live orders to resolve.
func (s *Store) ListMarketsDueForSettle(ctx context.Context, now time.Time) ([]Market, error) {
	// end_time preferred; else window_start + 300/900 as fallback for older rows
	cf := s.castFloat("p.size_usd")
	rows, err := s.query(ctx, `
SELECT m.slug, m.condition_id, m.up_token_id, m.down_token_id, m.asset, m.timeframe,
       m.window_start, m.end_time, m.event_start, m.open_price, m.title
FROM markets m
WHERE m.settled_at IS NULL
  AND (
    (m.end_time IS NOT NULL AND m.end_time <= ?)
    OR (
      m.end_time IS NULL AND m.window_start > 0
      AND (m.window_start + CASE WHEN m.timeframe = '15m' THEN 900 ELSE 300 END)
          <= ?
    )
  )
  AND (
    EXISTS (SELECT 1 FROM positions p WHERE p.market_slug = m.slug AND `+cf+` > 0)
    OR EXISTS (
      SELECT 1 FROM orders o
      WHERE o.market_slug = m.slug
        AND o.status IN ('pending', 'live', 'open', 'dry_run', 'dry_filled')
    )
  )
ORDER BY m.end_time NULLS LAST
`, now.UTC(), now.UTC().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Market
	for rows.Next() {
		var m Market
		var endTime, eventStart sql.NullTime
		var open sql.NullString
		if err := rows.Scan(
			&m.Slug, &m.ConditionID, &m.UpTokenID, &m.DownTokenID, &m.Asset, &m.Timeframe,
			&m.WindowStart, &endTime, &eventStart, &open, &m.Title,
		); err != nil {
			return nil, err
		}
		if endTime.Valid {
			t := endTime.Time
			m.EndTime = &t
		}
		if eventStart.Valid {
			t := eventStart.Time
			m.EventStart = &t
		}
		if open.Valid {
			v := open.String
			m.OpenPrice = &v
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MarkMarketSettled records settlement metadata.
func (s *Store) MarkMarketSettled(ctx context.Context, slug, outcome string, pnlUSD decimal.Decimal, at time.Time) error {
	_, err := s.exec(ctx, `
UPDATE markets
SET settled_at = ?, settle_outcome = ?, settle_pnl_usd = ?, updated_at = ?
WHERE slug = ?
`, at.UTC(), outcome, pnlUSD.String(), at.UTC(), slug)
	return err
}

// LatestSpotForMarket returns the most recent snapshot spot for a market (paper resolve).
func (s *Store) LatestSpotForMarket(ctx context.Context, marketSlug string) (decimal.Decimal, bool, error) {
	q := `
SELECT spot FROM price_snapshots
WHERE market_slug = ? AND spot IS NOT NULL
ORDER BY ts DESC LIMIT 1`
	if s.dialect == dialectDuckDB {
		q = `
SELECT spot FROM price_snapshots
WHERE market_slug = ? AND spot IS NOT NULL AND spot != ''
ORDER BY ts DESC LIMIT 1`
	}
	row := s.queryRow(ctx, q, marketSlug)
	var spot string
	if err := row.Scan(&spot); err != nil {
		if err == sql.ErrNoRows {
			return decimal.Zero, false, nil
		}
		return decimal.Zero, false, err
	}
	d, err := decimal.NewFromString(spot)
	if err != nil {
		return decimal.Zero, false, err
	}
	return d, true, nil
}

// ListOrdersForMarket returns orders for a market (any status).
func (s *Store) ListOrdersForMarket(ctx context.Context, marketSlug string) ([]Order, error) {
	rows, err := s.query(ctx, `
SELECT id, market_slug, token_id, strategy, side, price, size, size_usd,
       status, clob_order_id, reason, created_at, updated_at
FROM orders WHERE market_slug = ?
ORDER BY created_at
`, marketSlug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		var o Order
		var clob, reason sql.NullString
		var created, updated sql.NullTime
		if err := rows.Scan(
			&o.ID, &o.MarketSlug, &o.TokenID, &o.Strategy, &o.Side, &o.Price, &o.Size, &o.SizeUSD,
			&o.Status, &clob, &reason, &created, &updated,
		); err != nil {
			return nil, err
		}
		if clob.Valid {
			o.CLOBOrderID = clob.String
		}
		if reason.Valid {
			o.Reason = reason.String
		}
		if created.Valid {
			o.CreatedAt = created.Time
		}
		if updated.Valid {
			o.UpdatedAt = updated.Time
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// InsertSnapshot appends a price snapshot.
func (s *Store) InsertSnapshot(ctx context.Context, snap Snapshot) error {
	now := time.Now().UTC()
	if s.dialect == dialectPostgres {
		_, err := s.exec(ctx, `
INSERT INTO price_snapshots (
  ts, asset, spot, market_slug, mid_up, mid_down, fair_up, fair_down, open_price
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, now, snap.Asset, nullStr(snap.Spot), nullStr(snap.MarketSlug),
			nullStr(snap.MidUp), nullStr(snap.MidDown),
			nullStr(snap.FairUp), nullStr(snap.FairDown), nullStr(snap.OpenPrice))
		return err
	}
	id, err := s.nextID(ctx, "price_snapshots_id_seq")
	if err != nil {
		return err
	}
	_, err = s.exec(ctx, `
INSERT INTO price_snapshots (
  id, ts, asset, spot, market_slug, mid_up, mid_down, fair_up, fair_down, open_price
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, id, now, snap.Asset, nullStr(snap.Spot), nullStr(snap.MarketSlug),
		nullStr(snap.MidUp), nullStr(snap.MidDown),
		nullStr(snap.FairUp), nullStr(snap.FairDown), nullStr(snap.OpenPrice))
	return err
}

// LogEvent writes to bot_logs.
func (s *Store) LogEvent(ctx context.Context, level, event, detail string) error {
	now := time.Now().UTC()
	if s.dialect == dialectPostgres {
		_, err := s.exec(ctx, `
INSERT INTO bot_logs (ts, level, event, detail) VALUES (?, ?, ?, ?)
`, now, level, event, nullStr(detail))
		return err
	}
	id, err := s.nextID(ctx, "bot_logs_id_seq")
	if err != nil {
		return err
	}
	_, err = s.exec(ctx, `
INSERT INTO bot_logs (id, ts, level, event, detail)
VALUES (?, ?, ?, ?, ?)
`, id, now, level, event, nullStr(detail))
	return err
}

// RecordPnL appends a pnl ledger entry (negative = loss).
func (s *Store) RecordPnL(ctx context.Context, kind string, amountUSD decimal.Decimal, detail string) error {
	now := time.Now().UTC()
	if s.dialect == dialectPostgres {
		_, err := s.exec(ctx, `
INSERT INTO pnl_ledger (ts, kind, amount_usd, detail) VALUES (?, ?, ?, ?)
`, now, kind, amountUSD.String(), nullStr(detail))
		return err
	}
	id, err := s.nextID(ctx, "pnl_ledger_id_seq")
	if err != nil {
		return err
	}
	_, err = s.exec(ctx, `
INSERT INTO pnl_ledger (id, ts, kind, amount_usd, detail)
VALUES (?, ?, ?, ?, ?)
`, id, now, kind, amountUSD.String(), nullStr(detail))
	return err
}

func (s *Store) nextID(ctx context.Context, seq string) (int64, error) {
	// DuckDB sequences only.
	var q string
	switch seq {
	case "price_snapshots_id_seq":
		q = "SELECT nextval('price_snapshots_id_seq')"
	case "bot_logs_id_seq":
		q = "SELECT nextval('bot_logs_id_seq')"
	case "pnl_ledger_id_seq":
		q = "SELECT nextval('pnl_ledger_id_seq')"
	default:
		return 0, fmt.Errorf("unknown sequence %s", seq)
	}
	row := s.db.QueryRowContext(ctx, q)
	var id int64
	if err := row.Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// SumPnLSince sums amount_usd since t.
func (s *Store) SumPnLSince(ctx context.Context, since time.Time) (decimal.Decimal, error) {
	row := s.queryRow(ctx, `
SELECT COALESCE(SUM(`+s.castFloat("amount_usd")+`), 0) FROM pnl_ledger WHERE ts >= ?
`, since.UTC())
	var v float64
	if err := row.Scan(&v); err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromFloat(v), nil
}

// MarketExposureUSD returns active exposure without double-counting.
// dry_run orders already create positions, so only live/pending exchange orders
// (not yet reflected as positions) are added on top of positions.
func (s *Store) MarketExposureUSD(ctx context.Context, marketSlug string) (decimal.Decimal, error) {
	pos, err := s.PositionUSD(ctx, marketSlug)
	if err != nil {
		return decimal.Zero, err
	}
	// Exclude dry_run: those are already in positions (optimistic fill).
	row := s.queryRow(ctx, `
SELECT COALESCE(SUM(`+s.castFloat("size_usd")+`), 0) FROM orders
WHERE market_slug = ? AND status IN ('pending', 'live', 'open')
`, marketSlug)
	var orderV float64
	if err := row.Scan(&orderV); err != nil {
		return decimal.Zero, err
	}
	return pos.Add(decimal.NewFromFloat(orderV)), nil
}

// SetClosePrice sets close_price only if currently null.
func (s *Store) SetClosePrice(ctx context.Context, slug, closePrice string) error {
	q := `
UPDATE markets SET close_price = ?, updated_at = ?
WHERE slug = ? AND close_price IS NULL`
	if s.dialect == dialectDuckDB {
		q = `
UPDATE markets SET close_price = ?, updated_at = ?
WHERE slug = ? AND (close_price IS NULL OR close_price = '')`
	}
	_, err := s.exec(ctx, q, closePrice, time.Now().UTC(), slug)
	return err
}

// GetClosePrice returns stored close price if any.
func (s *Store) GetClosePrice(ctx context.Context, slug string) (decimal.Decimal, bool, error) {
	row := s.queryRow(ctx, `SELECT close_price FROM markets WHERE slug = ?`, slug)
	var cp sql.NullString
	if err := row.Scan(&cp); err != nil {
		if err == sql.ErrNoRows {
			return decimal.Zero, false, nil
		}
		return decimal.Zero, false, err
	}
	if !cp.Valid || cp.String == "" {
		return decimal.Zero, false, nil
	}
	d, err := decimal.NewFromString(cp.String)
	if err != nil {
		return decimal.Zero, false, err
	}
	return d, true, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
