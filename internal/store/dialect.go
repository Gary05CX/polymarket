package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// sqlOpen is a thin alias so open.go can stay tidy.
func sqlOpen(driver, dsn string) (*sql.DB, error) {
	return sql.Open(driver, dsn)
}

type dialect int

const (
	dialectDuckDB dialect = iota
	dialectPostgres
)

// rebind converts ? placeholders to $1,$2,... for PostgreSQL.
func (s *Store) rebind(q string) string {
	if s == nil || s.dialect != dialectPostgres {
		return q
	}
	var b strings.Builder
	b.Grow(len(q) + 8)
	n := 0
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteByte(q[i])
	}
	return b.String()
}

// castFloat SQL fragment for summing/casting numeric-as-text or numeric columns.
func (s *Store) castFloat(expr string) string {
	if s != nil && s.dialect == dialectPostgres {
		return "CAST(" + expr + " AS DOUBLE PRECISION)"
	}
	return "CAST(" + expr + " AS DOUBLE)"
}

func (s *Store) exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, s.rebind(q), args...)
}

func (s *Store) query(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return s.db.QueryContext(ctx, s.rebind(q), args...)
}

func (s *Store) queryRow(ctx context.Context, q string, args ...any) *sql.Row {
	return s.db.QueryRowContext(ctx, s.rebind(q), args...)
}
