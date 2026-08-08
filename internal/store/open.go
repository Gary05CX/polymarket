package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/duckdb/duckdb-go/v2"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// OpenOptions configures which database backend to use.
type OpenOptions struct {
	// Driver: "duckdb" (default) or "postgres" / "postgresql".
	Driver string
	// DuckDBPath is the file path when Driver is duckdb.
	DuckDBPath string
	// PostgresDSN is a libpq/pgx URL, e.g.
	// postgres://user:pass@localhost:5432/polymarket?sslmode=disable
	PostgresDSN string
}

// OpenFromOptions opens DuckDB or PostgreSQL based on Driver.
func OpenFromOptions(opt OpenOptions) (*Store, error) {
	driver := strings.ToLower(strings.TrimSpace(opt.Driver))
	if driver == "" {
		driver = "duckdb"
	}
	switch driver {
	case "duckdb":
		return openDuckDB(opt.DuckDBPath)
	case "postgres", "postgresql", "pg":
		return openPostgres(opt.PostgresDSN)
	default:
		return nil, fmt.Errorf("unsupported DB_DRIVER %q (use duckdb or postgres)", opt.Driver)
	}
}

// Open is the legacy DuckDB helper (kept for compatibility).
func Open(path string, schemaSQL string) (*Store, error) {
	s, err := openDuckDB(path)
	if err != nil {
		return nil, err
	}
	if schemaSQL != "" && schemaSQL != SchemaDuckDB {
		if _, err := s.db.Exec(schemaSQL); err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("apply schema: %w", err)
		}
	}
	return s, nil
}

func openDuckDB(path string) (*Store, error) {
	if path == "" {
		path = "data/bot.duckdb"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir data: %w", err)
	}
	db, err := sqlOpen("duckdb", path)
	if err != nil {
		return nil, fmt.Errorf("open duckdb: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping duckdb: %w", err)
	}
	s := &Store{db: db, dialect: dialectDuckDB}
	if _, err := db.Exec(SchemaDuckDB); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply duckdb schema: %w", err)
	}
	for _, q := range []string{
		`ALTER TABLE markets ADD COLUMN IF NOT EXISTS settled_at TIMESTAMP`,
		`ALTER TABLE markets ADD COLUMN IF NOT EXISTS settle_outcome VARCHAR`,
		`ALTER TABLE markets ADD COLUMN IF NOT EXISTS settle_pnl_usd VARCHAR`,
		`ALTER TABLE markets ADD COLUMN IF NOT EXISTS close_price VARCHAR`,
		`ALTER TABLE orders ADD COLUMN IF NOT EXISTS dry_run BOOLEAN DEFAULT TRUE`,
		`ALTER TABLE orders ADD COLUMN IF NOT EXISTS error_message VARCHAR`,
		`ALTER TABLE orders ADD COLUMN IF NOT EXISTS settle_pnl_usd VARCHAR`,
	} {
		if _, err := db.Exec(q); err != nil {
			_, _ = db.Exec(strings.Replace(q, " IF NOT EXISTS", "", 1))
		}
	}
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_dry_run ON orders(dry_run)`)
	// Best-effort backfill for older rows
	_, _ = db.Exec(`
UPDATE orders SET dry_run = FALSE
WHERE COALESCE(dry_run, TRUE) = TRUE
  AND (
    status IN ('error', 'cancelled', 'live', 'pending', 'open', 'filled', 'submitted', 'settled', 'matched')
    OR (clob_order_id IS NOT NULL AND clob_order_id != '' AND clob_order_id NOT LIKE 'dry-%')
  )
`)
	_, _ = db.Exec(`UPDATE orders SET dry_run = TRUE WHERE status LIKE 'dry%'`)
	return s, nil
}

func openPostgres(dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("DATABASE_URL is required when DB_DRIVER=postgres")
	}
	db, err := sqlOpen("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	s := &Store{db: db, dialect: dialectPostgres}
	if _, err := db.Exec(SchemaPostgres); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply postgres schema: %w", err)
	}
	// Idempotent column adds for older installs
	for _, q := range []string{
		`ALTER TABLE markets ADD COLUMN IF NOT EXISTS close_price NUMERIC(36, 18)`,
		`ALTER TABLE markets ADD COLUMN IF NOT EXISTS settled_at TIMESTAMPTZ`,
		`ALTER TABLE markets ADD COLUMN IF NOT EXISTS settle_outcome TEXT`,
		`ALTER TABLE markets ADD COLUMN IF NOT EXISTS settle_pnl_usd NUMERIC(36, 18)`,
		`ALTER TABLE orders ADD COLUMN IF NOT EXISTS dry_run BOOLEAN NOT NULL DEFAULT TRUE`,
		`ALTER TABLE orders ADD COLUMN IF NOT EXISTS error_message TEXT`,
		`ALTER TABLE orders ADD COLUMN IF NOT EXISTS settle_pnl_usd NUMERIC(36, 18)`,
	} {
		_, _ = db.Exec(q)
	}
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_dry_run ON orders(dry_run)`)
	// Best-effort backfill: infer live attempts from status / clob id
	_, _ = db.Exec(`
UPDATE orders SET dry_run = FALSE
WHERE dry_run IS TRUE
  AND (
    status IN ('error', 'cancelled', 'live', 'pending', 'open', 'filled', 'submitted', 'settled', 'matched')
    OR (clob_order_id IS NOT NULL AND clob_order_id <> '' AND clob_order_id NOT LIKE 'dry-%')
  )
`)
	_, _ = db.Exec(`UPDATE orders SET dry_run = TRUE WHERE status LIKE 'dry%'`)
	return s, nil
}
