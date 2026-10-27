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
	// 0. Muat .env paling awal agar semua konfigurasi membaca environment yang benar.
	if err := godotenv.Load(); err != nil {
		log.Println("Peringatan: Gagal memuat file .env, menggunakan variabel environment sistem.")
	}

	// 1. Inisialisasi logger terpusat.
	config.InitLogger()
	defer config.Log.Sync()

	config.Log.Info("Aplikasi sedang diinisialisasi...")

	// 2. Inisialisasi JWT setelah .env dimuat karena membutuhkan secret dari environment.
	if err := middlewares.InitJWT(); err != nil {
		config.Log.Fatal("Konfigurasi JWT tidak valid", zap.Error(err))
	}

	// 3. Hubungkan ke database dan pastikan koneksi ditutup saat aplikasi berhenti.
	config.ConnectDB()
	defer config.CloseDB()

	// 4. Jalankan seeder hanya jika RUN_SEEDER=true.
	// Seeder berjalan dalam satu transaksi, jadi jika gagal tidak ada data yang tersimpan.
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

	// 5. Jangan percayai header X-Forwarded-For dari client.
	// Dengan begitu, c.ClientIP() memakai IP koneksi langsung (RemoteAddr).
	if err := r.SetTrustedProxies(nil); err != nil {
		config.Log.Fatal("Gagal mengatur trusted proxies", zap.Error(err))
	}

	// 6. Aktifkan middleware CORS.
	r.Use(middlewares.CORSMiddleware())

	// 7. Daftarkan semua route API.
	routes.SetupRouter(r)

	// 8. Tentukan port server. Jika PORT kosong, gunakan 8080.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	config.Log.Info("Server berhasil berjalan", zap.String("port", ":"+port))
	if err := r.Run(":" + port); err != nil {
		config.Log.Fatal("Server berhenti karena error", zap.Error(err))
	}
}