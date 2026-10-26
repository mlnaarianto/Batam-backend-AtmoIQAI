package database

import (
	"context"
	"log"
	"time"

	"weather-backend/config"
)

// RunAllSeeders menjalankan semua seeder dalam SATU transaksi:
// kalau satu langkah gagal, tidak ada data yang tersimpan.
func RunAllSeeders() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tx, err := config.SQLDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op jika sudah Commit

	if err := SeedRoles(ctx, tx); err != nil {
		return err
	}
	if err := SeedPermissions(ctx, tx); err != nil {
		return err
	}
	if err := SeedUsers(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	log.Println("Seluruh database seeder berhasil dijalankan!")
	return nil
}