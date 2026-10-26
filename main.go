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
	// 0. Muat file .env paling awal, sebelum yang lain membaca environment
	if err := godotenv.Load(); err != nil {
		log.Println("Peringatan: Gagal memuat file .env, menggunakan variabel environment sistem.")
	}

	// 1. Inisialisasi Zap Logger
	config.InitLogger()
	defer config.Log.Sync()

	config.Log.Info("Aplikasi sedang diinisialisasi...")

	// 2. Inisialisasi JWT (harus setelah .env dimuat)
	if err := middlewares.InitJWT(); err != nil {
		config.Log.Fatal("Konfigurasi JWT tidak valid", zap.Error(err))
	}

	// 3. Koneksi Database
	config.ConnectDB()
	defer config.CloseDB()

	// 4. Seeder hanya jika diminta eksplisit lewat env var.
	// Seeder berjalan dalam satu transaksi: kalau gagal, tidak ada data yang tersimpan.
	if os.Getenv("RUN_SEEDER") == "true" {
		config.Log.Info("RUN_SEEDER=true terdeteksi, menjalankan seeder...")
		if err := database.RunAllSeeders(); err != nil {
			config.Log.Fatal("Seeder gagal dijalankan", zap.Error(err))
		}
		config.Log.Info("Seeder selesai dijalankan.")
	} else {
		config.Log.Info("Seeder dilewati (RUN_SEEDER tidak diset ke true).")
	}

	r := gin.Default()

	// BARU: jangan percayai header X-Forwarded-For dari client.
	// c.ClientIP() akan memakai IP koneksi langsung (RemoteAddr).
	if err := r.SetTrustedProxies(nil); err != nil {
		config.Log.Fatal("Gagal mengatur trusted proxies", zap.Error(err))
	}

	// 5. Middleware CORS
	r.Use(middlewares.CORSMiddleware())

	// 6. Daftarkan Rute API
	routes.SetupRouter(r)

	// 7. Jalankan Server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	config.Log.Info("Server berhasil berjalan", zap.String("port", ":"+port))
	if err := r.Run(":" + port); err != nil {
		config.Log.Fatal("Server berhenti karena error", zap.Error(err))
	}
}