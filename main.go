package main

import (
	"log"
	"os"
	"weather-backend/config"
	"weather-backend/database"
	"weather-backend/middlewares"
	"weather-backend/routes"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	// 0. Muat file .env di awal sebelum konfigurasi database atau seeder
	err := godotenv.Load()
	if err != nil {
		log.Println("Peringatan: Gagal memuat file .env, menggunakan variabel environment sistem.")
	}

	// 1. Inisialisasi Zap Logger di config
	config.InitLogger()
	defer config.Log.Sync() // Memastikan sisa buffer log ter-flush saat aplikasi mati

	config.Log.Info("Aplikasi sedang diinisialisasi...")

	// 2. Koneksi Database
	config.ConnectDB()

	// 3. Jalankan Seluruh Seeder HANYA jika diminta secara eksplisit lewat env var.
	// Ini penting supaya perubahan RBAC (Role & Permission) yang sudah diatur
	// manual lewat UI TIDAK ke-reset setiap kali server di-restart/`go run .` lagi.
	// Cara pakai: set RUN_SEEDER=true di file .env atau environment saat memang
	// ingin menjalankan/reset seeder (misal setup awal project).
	if os.Getenv("RUN_SEEDER") == "true" {
		config.Log.Info("RUN_SEEDER=true terdeteksi, menjalankan seeder...")
		database.RunAllSeeders()
		config.Log.Info("Seeder selesai dijalankan.")
	} else {
		config.Log.Info("Seeder dilewati (RUN_SEEDER tidak diset ke true).")
	}

	r := gin.Default()

	// 4. Gunakan Middleware CORS
	r.Use(middlewares.CORSMiddleware())

	// 5. Daftarkan Rute API
	routes.SetupRouter(r)

	// 6. Jalankan Server
	config.Log.Info("Server berhasil berjalan", zap.String("port", ":8080"))
	r.Run(":8080")
}