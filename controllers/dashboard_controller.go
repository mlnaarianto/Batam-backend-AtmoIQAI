package controllers

import (
	"net/http"
	"weather-backend/config"
	"weather-backend/models"

	"github.com/gin-gonic/gin"
)

// GET /api/admin/dashboard-stats
func GetDashboardStats() gin.HandlerFunc {
	return func(c *gin.Context) {
		var totalStations int64
		var totalPredictions int64

		// Hitung jumlah stasiun aktif/total di database
		if err := config.DB.Model(&models.Station{}).Count(&totalStations).Error; err != nil {
			totalStations = 0
		}

		// Hitung jumlah total data prediksi cuaca AI yang tersimpan
		if err := config.DB.Model(&models.WeatherPrediction{}).Count(&totalPredictions).Error; err != nil {
			totalPredictions = 0
		}

		c.JSON(http.StatusOK, gin.H{
			"total_stations":      totalStations,
			"total_predictions":   totalPredictions,
			"ai_server_status":    "Aktif",
			"database_status":     "Terhubung",
			"message":             "Statistik dashboard berhasil dimuat",
		})
	}
}