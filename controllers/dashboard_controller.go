package controllers

import (
	"net/http"

	"weather-backend/config"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GetDashboardStats mengembalikan ringkasan angka untuk halaman dashboard admin.
// Saat ini endpoint ini hanya menghitung jumlah stasiun dan jumlah prediksi cuaca,
// lalu menambahkan status statis (AI server & database) yang ditampilkan di UI.
//
// Catatan:
//   - Endpoint ini diproteksi oleh AuthMiddleware + RequirePermission("view-weather")
//     (lihat routes.go), jadi hanya user yang berhak yang bisa memanggilnya.
//   - Dikembalikan sebagai gin.HandlerFunc agar konsisten dengan handler lain
//     yang dibuat lewat pola factory function.
func GetDashboardStats() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Pakai context dari request agar query otomatis dibatalkan jika
		// client memutus koneksi atau request di-timeout oleh middleware.
		ctx := c.Request.Context()

		// Dua counter ini akan dipakai untuk mengisi response.
		var totalStations, totalPredictions int64

		// Hitung total stasiun. Kalau gagal, log error tapi jangan gagalkan
		// seluruh request — dashboard tetap bisa tampil dengan angka default 0.
		if err := config.SQLDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM stations`).Scan(&totalStations); err != nil {
			config.Log.Error("Gagal menghitung stations", zap.Error(err))
		}

		// Hitung total prediksi cuaca. Sama seperti di atas: error hanya di-log,
		// karena dashboard bersifat informatif dan tidak kritis.
		if err := config.SQLDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM weather_predictions`).Scan(&totalPredictions); err != nil {
			config.Log.Error("Gagal menghitung weather_predictions", zap.Error(err))
		}

		// Response sengaja mengembalikan struktur flat agar mudah dikonsumsi
		// oleh frontend (tidak perlu mapping nested).
		// "ai_server_status" dan "database_status" saat ini hardcoded; jika nanti
		// perlu health check nyata, tinggal ganti dengan hasil pengecekan aktual.
		c.JSON(http.StatusOK, gin.H{
			"total_stations":    totalStations,
			"total_predictions": totalPredictions,
			"ai_server_status":  "Aktif",
			"database_status":   "Terhubung",
			"message":           "Statistik dashboard berhasil dimuat",
		})
	}
}