package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Gary05CX/polymarket/internal/domain"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type Store struct {
	db      *sql.DB
	driver  string
	mu      sync.Mutex
	lastRej map[string]string
}

func Open(driver, dsn, sqlRoot string) (*Store, error) {
	drv := driver
	name := "pgx"
	if driver == "duckdb" {
		name = "duckdb"
		if err := registerDuckDB(); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open(name, dsn)
	if err != nil {
		return nil, err
	}
	if driver == "duckdb" {
		db.SetMaxOpenConns(1)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db, driver: drv, lastRej: map[string]string{}}
	if err := s.migrate(sqlRoot); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(sqlRoot string) error {
	dir := filepath.Join(sqlRoot, s.driver)
	if s.driver == "postgres" {
		dir = filepath.Join(sqlRoot, "postgres")
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var files []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		if _, err := s.db.Exec(string(b)); err != nil {
			return fmt.Errorf("migrate %s: %w", f, err)
		}
	}
	return nil
}

func (s *Store) exec(query string, args ...any) error {
	if s.driver == "duckdb" {
		s.mu.Lock()
		defer s.mu.Unlock()
		query = rebind(query)
	}
	_, err := s.db.Exec(query, args...)
	return err
}

func (s *Store) queryRow(query string, args ...any) *sql.Row {
	if s.driver == "duckdb" {
		s.mu.Lock()
		defer s.mu.Unlock()
		query = rebind(query)
	}
	return s.db.QueryRow(query, args...)
}

func rebind(q string) string {
	n := 1
	for {
		old := "$" + strconv.Itoa(n)
		if !strings.Contains(q, old) {
			break
		}
		q = strings.Replace(q, old, "?", 1)
		n++
	}
	return q
}

func NewID() string {
	var b [16]byte
	_, _ = time.Now().UTC().MarshalBinary()
	n := time.Now().UnixNano()
	for i := 0; i < 8; i++ {
		b[i] = byte(n >> (8 * i))
	}
	for i := 8; i < 16; i++ {
		b[i] = byte(n >> (4 * (i - 8)))
	}
	return hex.EncodeToString(b[:])
}

func (s *Store) InsertRun(id, mode, strategyID, source string, cfg any, maxWindows int) error {
	host, _ := os.Hostname()
	raw, _ := json.Marshal(cfg)
	return s.exec(`INSERT INTO runs (id, started_at, run_mode, strategy_id, strategy_source, config_json, hostname, max_windows)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, id, time.Now().UTC(), mode, strategyID, source, string(raw), host, maxWindows)
}

func (s *Store) EndRun(id string) error {
	return s.exec(`UPDATE runs SET ended_at=$1 WHERE id=$2`, time.Now().UTC(), id)
}

func (s *Store) UpsertMarket(m domain.Market) error {
	return s.exec(`INSERT INTO markets (slug, asset, condition_id, window_start, window_end, up_token_id, down_token_id, min_order_size, tick_size, fee_rate, seconds_delay, resolution_source)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (slug) DO UPDATE SET
  condition_id=EXCLUDED.condition_id, up_token_id=EXCLUDED.up_token_id, down_token_id=EXCLUDED.down_token_id,
  min_order_size=EXCLUDED.min_order_size, tick_size=EXCLUDED.tick_size, fee_rate=EXCLUDED.fee_rate,
  seconds_delay=EXCLUDED.seconds_delay, resolution_source=EXCLUDED.resolution_source`,
		m.Slug, m.Asset, m.ConditionID, m.WindowStart.UTC(), m.WindowEnd.UTC(), m.UpTokenID, m.DownTokenID,
		m.MinOrderSize, m.TickSize, nullZero(m.FeeRate), m.SecondsDelay, m.ResolutionSource)
}

func (s *Store) InsertTick(runID, slug, source string, ts time.Time, price float64) error {
	return s.exec(`INSERT INTO price_ticks (id, run_id, market_slug, source, ts, price) VALUES ($1,$2,$3,$4,$5,$6)`,
		NewID(), runID, slug, source, ts.UTC(), price)
}

func (s *Store) InsertSignal(runID, mode string, sig *domain.Signal) error {
	feat, _ := json.Marshal(sig.Features)
	return s.exec(`INSERT INTO signals (id, run_id, strategy_id, strategy_source, run_mode, market_slug, side, token_id, confidence, score, features_json, reason, pm_price, binance_price, window_open_px, seconds_left, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		NewID(), runID, sig.StrategyID, sig.StrategySource, mode, sig.MarketSlug, sig.Side, sig.TokenID,
		sig.Confidence, sig.Score, string(feat), sig.Reason, sig.PMPrice, sig.BinancePrice, sig.WindowOpenPx, sig.SecondsLeft, time.Now().UTC())
}

func (s *Store) InsertRejected(runID, mode, strategyID, source string, r domain.Reject) error {
	key := runID + "|" + r.Slug
	if s.lastRej[key] == r.ReasonCode {
		return nil
	}
	s.lastRej[key] = r.ReasonCode
	feat, _ := json.Marshal(r.Features)
	return s.exec(`INSERT INTO rejected_signals (id, run_id, strategy_id, strategy_source, run_mode, market_slug, reason_code, reason, features_json, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		NewID(), runID, strategyID, source, mode, r.Slug, r.ReasonCode, r.Reason, string(feat), time.Now().UTC())
}

func (s *Store) HasOrder(runID, slug string) bool {
	var n int
	err := s.queryRow(`SELECT count(*) FROM orders WHERE run_id=$1 AND market_slug=$2`, runID, slug).Scan(&n)
	return err == nil && n > 0
}

func (s *Store) InsertOrder(o domain.Order) error {
	return s.exec(`INSERT INTO orders (id, run_id, strategy_id, strategy_source, run_mode, market_slug, side, token_id, intended_price, intended_shares, limit_price, notional_usdc, is_min_size, status, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		o.ID, o.RunID, o.StrategyID, o.StrategySource, o.RunMode, o.MarketSlug, o.Side, o.TokenID,
		o.IntendedPrice, o.IntendedShares, o.LimitPrice, o.NotionalUSDC, o.IsMinSize, o.Status, time.Now().UTC())
}

func (s *Store) InsertFill(f domain.Fill) error {
	return s.exec(`INSERT INTO fills (id, order_id, run_id, market_slug, fill_model, price, shares, fee_usdc, fee_rate, liquidity_flag, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		f.ID, f.OrderID, f.RunID, f.MarketSlug, f.FillModel, f.Price, f.Shares, f.FeeUSDC, f.FeeRate, f.LiquidityFlag, time.Now().UTC())
}

func (s *Store) UpsertPosition(runID, slug, side string, shares, avg float64) error {
	return s.exec(`INSERT INTO positions (id, run_id, market_slug, side, shares, avg_price)
VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT (run_id, market_slug) DO UPDATE SET shares=EXCLUDED.shares, avg_price=EXCLUDED.avg_price, side=EXCLUDED.side`,
		NewID(), runID, slug, side, shares, avg)
}

func (s *Store) UpsertResolution(slug, source, winner string, ptb, final float64, raw any) error {
	b, _ := json.Marshal(raw)
	return s.exec(`INSERT INTO resolutions (id, market_slug, source, winner, price_to_beat, final_price, resolved_at, raw_json)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT (market_slug, source) DO UPDATE SET winner=EXCLUDED.winner, price_to_beat=EXCLUDED.price_to_beat, final_price=EXCLUDED.final_price, resolved_at=EXCLUDED.resolved_at, raw_json=EXCLUDED.raw_json`,
		NewID(), slug, source, winner, nullZero(ptb), nullZero(final), time.Now().UTC(), string(b))
}

func (s *Store) InsertPnL(runID, mode, strategyID, slug, orderID, fillID, fillModel string, shares, entry, exit, fee float64, win bool) error {
	pnl := shares*(exit-entry) - fee
	return s.exec(`INSERT INTO pnl_ledger (id, order_id, fill_id, run_id, run_mode, strategy_id, market_slug, shares, entry_price, exit_value, fee_usdc, pnl_usdc, is_win, fill_model)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		NewID(), orderID, fillID, runID, mode, strategyID, slug, shares, entry, exit, fee, pnl, win, fillModel)
}

type OpenOrder struct {
	ID, RunID, StrategyID, Mode, Slug, Side, FillID string
	Shares, Entry, Fee                              float64
	FillModel                                       string
}

func (s *Store) OpenFills(runID string) ([]OpenOrder, error) {
	q := `SELECT o.id, o.run_id, o.strategy_id, o.run_mode, o.market_slug, o.side, f.id, f.shares, f.price, f.fee_usdc, f.fill_model
FROM orders o JOIN fills f ON f.order_id=o.id
WHERE o.run_id=$1 AND NOT EXISTS (SELECT 1 FROM pnl_ledger p WHERE p.order_id=o.id)`
	if s.driver == "duckdb" {
		s.mu.Lock()
		defer s.mu.Unlock()
		q = rebind(q)
	}
	rows, err := s.db.Query(q, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OpenOrder
	for rows.Next() {
		var o OpenOrder
		if err := rows.Scan(&o.ID, &o.RunID, &o.StrategyID, &o.Mode, &o.Slug, &o.Side, &o.FillID, &o.Shares, &o.Entry, &o.Fee, &o.FillModel); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) Summary(ctx context.Context, runID string) (n int, winRate, pnl float64, err error) {
	_ = ctx
	err = s.queryRow(`SELECT count(*), COALESCE(avg(CASE WHEN is_win THEN 1.0 ELSE 0.0 END),0), COALESCE(sum(pnl_usdc),0) FROM pnl_ledger WHERE run_id=$1`, runID).
		Scan(&n, &winRate, &pnl)
	return
}

func nullZero(v float64) any {
	if v == 0 {
		return nil
	}
	return v
}
