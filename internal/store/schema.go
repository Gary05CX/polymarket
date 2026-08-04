package store

import _ "embed"

// SchemaDuckDB is the embedded DuckDB DDL.
//
//go:embed schema_duckdb.sql
var SchemaDuckDB string
