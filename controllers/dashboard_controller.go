package controllers

import (
	"net/http"

	"weather-backend/config"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GET /api/admin/dashboard-stats
func GetDashboardStats() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		var totalStations, totalPredictions int64

		if err := config.SQLDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM stations`).Scan(&totalStations); err != nil {
			config.Log.Error("Gagal menghitung stations", zap.Error(err))
		}
		if err := config.SQLDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM weather_predictions`).Scan(&totalPredictions); err != nil {
			config.Log.Error("Gagal menghitung weather_predictions", zap.Error(err))
		}

		c.JSON(http.StatusOK, gin.H{
			"total_stations":    totalStations,
			"total_predictions": totalPredictions,
			"ai_server_status":  "Aktif",
			"database_status":   "Terhubung",
			"message":           "Statistik dashboard berhasil dimuat",
		})
	}
}