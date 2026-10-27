package config

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// SQLDB adalah satu-satunya koneksi database yang dipakai aplikasi (SQL murni).
var SQLDB *sql.DB

// buildDSN menyusun DSN MySQL dari environment variable.
// loc=UTC dipakai agar waktu yang dikirim/dibaca konsisten,
// terutama untuk kolom seperti expired_at dan revoked_at.
func buildDSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=UTC",
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_NAME"),
	)
}

// ConnectDB membuka koneksi database, mengatur connection pool,
// lalu melakukan ping untuk memastikan database benar-benar bisa diakses.
func ConnectDB() {
	var err error

	SQLDB, err = sql.Open("mysql", buildDSN())
	if err != nil {
		log.Fatalf("Gagal membuka koneksi database: %v", err)
	}

	// Batasi jumlah koneksi agar tidak membebani database.
	SQLDB.SetMaxOpenConns(25)
	SQLDB.SetMaxIdleConns(10)
	SQLDB.SetConnMaxLifetime(30 * time.Minute)

	// Ping dengan timeout supaya aplikasi tidak menggantung jika DB tidak merespons.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := SQLDB.PingContext(ctx); err != nil {
		log.Fatalf("Ping database gagal: %v", err)
	}

	fmt.Println("Berhasil terhubung ke database MySQL!")
}

// CloseDB menutup koneksi database saat aplikasi berhenti.
// Aman dipanggil meskipun SQLDB belum terinisialisasi.
func CloseDB() {
	if SQLDB != nil {
		_ = SQLDB.Close()
	}
}