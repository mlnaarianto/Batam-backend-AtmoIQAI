package config

import (
	"fmt"
	"log"
	"os"

	"weather-backend/models"

	"github.com/joho/godotenv"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var DB *gorm.DB

func ConnectDB() {
	// Memuat file .env
	err := godotenv.Load()
	if err != nil {
		log.Println("Peringatan: File .env tidak ditemukan, menggunakan environment sistem default.")
	}

	// Menyusun string DSN secara dinamis dari file .env
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_NAME"),
	)

	var dbErr error
	DB, dbErr = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if dbErr != nil {
		log.Fatalf("Gagal terhubung ke database MySQL: %v", dbErr)
	}

	// Auto Migrate Model (Otomatis membuat tabel dengan format UUID)
	err = DB.AutoMigrate(
		&models.User{},
		&models.Role{},
		&models.Permission{},
		&models.PersonalAccessToken{},
		&models.Station{},           // <--- Tambahkan di sini
		&models.WeatherPrediction{}, // <--- Tambahkan setelah Station
	)
	if err != nil {
		log.Fatalf("Gagal melakukan Auto Migrate database: %v", err)
	}

	fmt.Println("Berhasil terhubung ke database MySQL & migrasi otomatis selesai!")
}
