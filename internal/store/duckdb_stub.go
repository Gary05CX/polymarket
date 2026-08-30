package store

import "fmt"

func registerDuckDB() error {
	return fmt.Errorf("duckdb not linked in this build; use postgres (default)")
}
