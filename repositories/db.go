package repositories

import (
	"context"
	"database/sql"
	"errors"
)

// ErrNotFound adalah error sentinel yang dikembalikan repository ketika
// query yang diharapkan menghasilkan satu baris ternyata tidak menemukan
// apa pun (mis. sql.ErrNoRows).
//
// Kenapa pakai error sentinel, bukan error string biasa?
//   - Pemanggil bisa mengecek dengan `errors.Is(err, repositories.ErrNotFound)`
//     tanpa bergantung pada isi pesan.
//   - Layer controller jadi konsisten: setiap "tidak ditemukan" diterjemahkan
//     menjadi HTTP 404, terlepas dari query SQL-nya apa.
//   - Menghindari kebocoran detail implementasi (mis. nama tabel) ke pesan error.
var ErrNotFound = errors.New("data tidak ditemukan")

// DBTX adalah interface minimal yang dibutuhkan repository untuk menjalankan query.
//
// Tujuannya agar satu repository bisa dipakai baik dengan *sql.DB (query
// langsung) maupun dengan *sql.Tx (di dalam transaksi) tanpa perubahan kode.
// Kedua tipe tersebut sudah memenuhi interface ini secara otomatis, karena
// sama-sama menyediakan:
//
//   - ExecContext      -> untuk INSERT/UPDATE/DELETE
//   - QueryContext     -> untuk SELECT banyak baris
//   - QueryRowContext  -> untuk SELECT satu baris
//
// Dengan pola ini:
//   - Operasi biasa: `NewXxxRepository(config.SQLDB)`
//   - Operasi dalam transaksi: `NewXxxRepository(tx)`
//
// Repository tidak perlu tahu (dan tidak boleh peduli) apakah dirinya
// sedang berjalan di dalam transaksi atau tidak; itu tanggung jawab
// pemanggil (controller/service).
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}