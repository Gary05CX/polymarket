// Package store provides DuckDB persistence for the bot.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Store wraps a DuckDB connection.
type Store struct {
	db *sql.DB
}

// Market is a discovered up/down market window.
type Market struct {
	Slug         string
	ConditionID  string
	UpTokenID    string
	DownTokenID  string
	Asset        string
	Timeframe    string
	WindowStart  int64
	EndTime      *time.Time
	EventStart   *time.Time
	OpenPrice    *string
	Title        string
	DiscoveredAt time.Time
	UpdatedAt    time.Time
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

// Open opens (or creates) DuckDB at path and applies schema.
func Open(path string, schemaSQL string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir data: %w", err)
	}
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}
	db.SetMaxOpenConns(1) // DuckDB is happiest single-writer
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping duckdb: %w", err)
	}
	s := &Store{db: db}
	if schemaSQL != "" {
		if _, err := db.Exec(schemaSQL); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("apply schema: %w", err)
		}
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
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
	_, err := s.db.ExecContext(ctx, q,
		m.Slug, m.ConditionID, m.UpTokenID, m.DownTokenID, m.Asset, m.Timeframe,
		m.WindowStart, endTime, eventStart, open, m.Title, discovered, now,
	)
	return err
}

// SetOpenPrice sets open_price only if currently null.
func (s *Store) SetOpenPrice(ctx context.Context, slug, openPrice string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE markets SET open_price = ?, updated_at = ?
WHERE slug = ? AND (open_price IS NULL OR open_price = '')
`, openPrice, time.Now().UTC(), slug)
	return err
}

// GetMarket loads a market by slug.
func (s *Store) GetMarket(ctx context.Context, slug string) (*Market, error) {
	const q = `
SELECT slug, condition_id, up_token_id, down_token_id, asset, timeframe,
       window_start, end_time, event_start, open_price, title, discovered_at, updated_at
FROM markets WHERE slug = ?
`
	row := s.db.QueryRowContext(ctx, q, slug)
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
	_, err := s.db.ExecContext(ctx, q,
		o.ID, o.MarketSlug, o.TokenID, o.Strategy, o.Side, o.Price, o.Size, o.SizeUSD,
		o.Status, nullStr(o.CLOBOrderID), nullStr(o.Reason), now, now,
	)
	return err
}

// UpdateOrderStatus updates status / clob id.
func (s *Store) UpdateOrderStatus(ctx context.Context, id, status, clobOrderID string) error {
	_, err := s.db.ExecContext(ctx, `
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
	rows, err := s.db.QueryContext(ctx, q)
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
	row := s.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(CAST(size_usd AS DOUBLE)), 0) FROM positions WHERE market_slug = ?
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
	_, err = s.db.ExecContext(ctx, `
UPDATE positions SET size = ?, avg_price = ?, size_usd = ?, updated_at = ?
WHERE market_slug = ? AND token_id = ?
`, newSize.String(), newAvg.String(), newUSD.String(), time.Now().UTC(), marketSlug, tokenID)
	return err
}

func (s *Store) insertPosition(ctx context.Context, marketSlug, tokenID string, size, price, usd decimal.Decimal) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO positions (market_slug, token_id, size, avg_price, size_usd, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
`, marketSlug, tokenID, size.String(), price.String(), usd.String(), time.Now().UTC())
	return err
}

// GetPosition loads one position.
func (s *Store) GetPosition(ctx context.Context, marketSlug, tokenID string) (*Position, error) {
	row := s.db.QueryRowContext(ctx, `
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

// CountOpenMarkets counts markets with non-zero position or open orders.
func (s *Store) CountOpenMarkets(ctx context.Context) (int, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT COUNT(DISTINCT market_slug) FROM (
  SELECT market_slug FROM positions WHERE CAST(size_usd AS DOUBLE) > 0
  UNION
  SELECT market_slug FROM orders WHERE status IN ('pending', 'live', 'open', 'dry_run')
)
`)
	var n int
	err := row.Scan(&n)
	return n, err
}

// InsertSnapshot appends a price snapshot.
func (s *Store) InsertSnapshot(ctx context.Context, snap Snapshot) error {
	id, err := s.nextID(ctx, "price_snapshots_id_seq")
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO price_snapshots (
  id, ts, asset, spot, market_slug, mid_up, mid_down, fair_up, fair_down, open_price
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, id, time.Now().UTC(), snap.Asset, nullStr(snap.Spot), nullStr(snap.MarketSlug),
		nullStr(snap.MidUp), nullStr(snap.MidDown),
		nullStr(snap.FairUp), nullStr(snap.FairDown), nullStr(snap.OpenPrice))
	return err
}

// LogEvent writes to bot_logs.
func (s *Store) LogEvent(ctx context.Context, level, event, detail string) error {
	id, err := s.nextID(ctx, "bot_logs_id_seq")
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO bot_logs (id, ts, level, event, detail)
VALUES (?, ?, ?, ?, ?)
`, id, time.Now().UTC(), level, event, nullStr(detail))
	return err
}

// RecordPnL appends a pnl ledger entry (negative = loss).
func (s *Store) RecordPnL(ctx context.Context, kind string, amountUSD decimal.Decimal, detail string) error {
	id, err := s.nextID(ctx, "pnl_ledger_id_seq")
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO pnl_ledger (id, ts, kind, amount_usd, detail)
VALUES (?, ?, ?, ?, ?)
`, id, time.Now().UTC(), kind, amountUSD.String(), nullStr(detail))
	return err
}

func (s *Store) nextID(ctx context.Context, seq string) (int64, error) {
	// Sequence name cannot be a bound parameter in DuckDB; embed known names only.
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
	row := s.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(CAST(amount_usd AS DOUBLE)), 0) FROM pnl_ledger WHERE ts >= ?
`, since.UTC())
	var v float64
	if err := row.Scan(&v); err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromFloat(v), nil
}

// MarketExposureUSD returns position + open order notional for a market.
func (s *Store) MarketExposureUSD(ctx context.Context, marketSlug string) (decimal.Decimal, error) {
	pos, err := s.PositionUSD(ctx, marketSlug)
	if err != nil {
		return decimal.Zero, err
	}
	row := s.db.QueryRowContext(ctx, `
SELECT COALESCE(SUM(CAST(size_usd AS DOUBLE)), 0) FROM orders
WHERE market_slug = ? AND status IN ('pending', 'live', 'open', 'dry_run')
`, marketSlug)
	var orderV float64
	if err := row.Scan(&orderV); err != nil {
		return decimal.Zero, err
	}
	return pos.Add(decimal.NewFromFloat(orderV)), nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
