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

// SQLDB adalah satu-satunya koneksi database (SQL murni).
var SQLDB *sql.DB

func buildDSN() string {
	// loc=UTC: waktu dikirim dan dibaca dalam UTC supaya expired_at dan revoked_at konsisten
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=UTC",
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_NAME"),
	)
}

func ConnectDB() {
	var err error

	SQLDB, err = sql.Open("mysql", buildDSN())
	if err != nil {
		log.Fatalf("Gagal membuka koneksi database: %v", err)
	}
	SQLDB.SetMaxOpenConns(25)
	SQLDB.SetMaxIdleConns(10)
	SQLDB.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := SQLDB.PingContext(ctx); err != nil {
		log.Fatalf("Ping database gagal: %v", err)
	}

	fmt.Println("Berhasil terhubung ke database MySQL!")
}

func CloseDB() {
	if SQLDB != nil {
		_ = SQLDB.Close()
	}
}