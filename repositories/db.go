package repositories

import (
	"context"
	"database/sql"
	"errors"
)

var ErrNotFound = errors.New("data tidak ditemukan")

// DBTX dipenuhi oleh *sql.DB maupun *sql.Tx, jadi repository bisa dipakai di dalam transaksi.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}