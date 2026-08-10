package controllers

import (
	"net/http"
	"time"
	"weather-backend/config"
	"weather-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// -------------------------------------------------------------------------
// Helper: cek apakah sebuah role memiliki permission tertentu.
// Dibaca langsung dari relasi Role -> Permissions (tabel role_permissions),
// bukan hardcode nama role. Jadi role apapun (admin/user/role baru) yang
// diberi permission ini lewat halaman RBAC otomatis dapat aksesnya.
// -------------------------------------------------------------------------
func roleHasPermission(roleName string, permissionName string) bool {
	if roleName == "" {
		return false
	}
	var role models.Role
	if err := config.DB.Preload("Permissions").Where("name = ?", roleName).First(&role).Error; err != nil {
		return false
	}
	for _, p := range role.Permissions {
		if p.Name == permissionName {
			return true
		}
	}
	return false
}

// GET /api/public/weather-predictions (Publik / Tanpa Login)
// Hanya menampilkan data publik murni. Data yang dibuat lewat AI saat user
// SEDANG LOGIN (predict-and-save, model_version = "lstm-enso-v2-30years")
// TIDAK ditampilkan di sini, karena itu dianggap data milik/aktivitas user
// yang bersangkutan, bukan data publik.
func GetPublicWeatherPredictions() gin.HandlerFunc {
	return func(c *gin.Context) {
		var predictions []models.WeatherPrediction
		query := config.DB.Preload("Station").Preload("User")

		// Filter opsional berdasarkan station_id
		if stationIDStr := c.Query("station_id"); stationIDStr != "" {
			parsedStationID, err := uuid.Parse(stationIDStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": "station_id tidak valid"})
				return
			}
			query = query.Where("station_id = ?", models.UUIDBinary(parsedStationID))
		}

		// Sembunyikan:
		// 1. Data dummy internal seeder ("lstm-v0.1-dummy")
		// 2. Data hasil AI yang disimpan saat user LOGIN ("lstm-enso-v2-30years")
		// Yang lolos hanya data dengan model_version NULL/kosong (data publik murni).
		excludedVersions := []string{"lstm-v0.1-dummy", "lstm-enso-v2-30years"}
		query = query.Where(
			"model_version IS NULL OR model_version NOT IN (?)",
			excludedVersions,
		)

		if err := query.Order("prediction_date desc").Find(&predictions).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal mengambil data weather predictions"})
			return
		}
		c.JSON(http.StatusOK, predictions)
	}
}

// GET /api/admin/weather-predictions
// Akses ditentukan MURNI oleh permission "weather-predictions:view-all":
//   - Punya permission itu  -> lihat SEMUA data (siapapun rolenya).
//   - Tidak punya           -> hanya lihat data milik user_id sendiri,
//                              berapapun/apapun query param yang dikirim.
func GetWeatherPredictions() gin.HandlerFunc {
	return func(c *gin.Context) {
		var predictions []models.WeatherPrediction
		query := config.DB.Preload("Station").Preload("User")

		userRole, _ := c.Get("role")
		currentUserID, _ := c.Get("user_id")

		roleNameStr, _ := userRole.(string)
		canViewAll := roleHasPermission(roleNameStr, "weather-predictions:view-all")

		if canViewAll {
			// Boleh lihat semua data. Kalau tetap kirim ?user_id=xxx,
			// itu dianggap filter opsional (mis. admin mau lihat punya user tertentu saja).
			if userIDStr := c.Query("user_id"); userIDStr != "" {
				parsedUserID, err := uuid.Parse(userIDStr)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"message": "user_id tidak valid"})
					return
				}
				query = query.Where("user_id = ?", models.UUIDBinary(parsedUserID))
			}
		} else {
			// Tidak punya izin view-all -> WAJIB dikunci ke user_id sendiri.
			// Query param user_id dari luar (kalau ada) DIABAIKAN sepenuhnya,
			// supaya tidak bisa dipakai untuk mengintip data user lain.
			if currentUserID == nil {
				c.JSON(http.StatusForbidden, gin.H{"message": "Akses ditolak"})
				return
			}
			parsedCurrentID, err := uuid.Parse(currentUserID.(string))
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"message": "user_id pada token tidak valid"})
				return
			}
			query = query.Where("user_id = ?", models.UUIDBinary(parsedCurrentID))
		}

		// View Per ID (jika ada parameter query ?id=xxx)
		if idStr := c.Query("id"); idStr != "" {
			parsedID, err := uuid.Parse(idStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": "id tidak valid"})
				return
			}
			query = query.Where("id = ?", models.UUIDBinary(parsedID))
		}

		// Filter opsional berdasarkan station_id
		if stationIDStr := c.Query("station_id"); stationIDStr != "" {
			parsedStationID, err := uuid.Parse(stationIDStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": "station_id tidak valid"})
				return
			}
			query = query.Where("station_id = ?", models.UUIDBinary(parsedStationID))
		}

		if err := query.Order("prediction_date desc").Find(&predictions).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal mengambil data weather predictions"})
			return
		}
		c.JSON(http.StatusOK, predictions)
	}
}

// GET /api/admin/weather-predictions/:id (Show Detail berdasarkan URL Path Parameter)
// Kalau tidak punya "weather-predictions:view-all", hanya boleh lihat detail miliknya sendiri
// (mencegah IDOR: user lain menebak/scan UUID untuk mengintip data orang lain).
func GetWeatherPredictionByID() gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		parsedID, err := uuid.Parse(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "ID tidak valid"})
			return
		}

		var prediction models.WeatherPrediction
		if err := config.DB.Preload("Station").Preload("User").Where("id = ?", models.UUIDBinary(parsedID)).First(&prediction).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"message": "Weather prediction tidak ditemukan"})
			return
		}

		userRole, _ := c.Get("role")
		currentUserID, _ := c.Get("user_id")
		roleNameStr, _ := userRole.(string)
		canViewAll := roleHasPermission(roleNameStr, "weather-predictions:view-all")

		if !canViewAll {
			if currentUserID == nil || uuid.UUID(prediction.UserID).String() != currentUserID.(string) {
				c.JSON(http.StatusForbidden, gin.H{"message": "Anda tidak memiliki akses ke data ini"})
				return
			}
		}

		c.JSON(http.StatusOK, prediction)
	}
}

// WeatherPredictionInput
//
// CATATAN PENTING soal binding:"required" pada field numerik (float64):
// Gin/validator menganggap nilai NOL pada tipe numerik sebagai "kosong",
// sehingga field seperti Rainfall=0.0 (artinya: tidak hujan, nilai yang SAH
// dan sering terjadi) akan ditolak validator meskipun field itu benar-benar
// dikirim oleh client. Karena itu binding:"required" HANYA dipasang pada
// field string yang memang tidak boleh kosong (UserID, StationID,
// PredictionDate, StatusWeather) -- bukan pada field angka yang boleh nol.
type WeatherPredictionInput struct {
	UserID         string `json:"user_id" binding:"required"`
	StationID      string `json:"station_id" binding:"required"`
	PredictionDate string `json:"prediction_date" binding:"required"` // format: "2006-01-02" atau RFC3339

	Temperature float64 `json:"temperature"`
	Humidity    float64 `json:"humidity"`
	Rainfall    float64 `json:"rainfall"` // 0.0 = tidak hujan, nilai valid -- JANGAN diberi binding:"required"
	AirPressure float64 `json:"air_pressure"`

	WindSpeed     *float64 `json:"wind_speed"`
	WindDirection *float64 `json:"wind_direction"`

	// Parameter El Niño & La Niña
	EnsoIndex    *float64 `json:"enso_index"`
	ClimatePhase *string  `json:"climate_phase"`

	StatusWeather   string   `json:"status_weather" binding:"required"`
	ConfidenceScore *float64 `json:"confidence_score"`

	// Sebelumnya RmseMetric/json:"rmse_metric". Direname mengikuti kolom DB
	// uncertainty_metric -- lihat catatan di models/weather_prediction.go.
	UncertaintyMetric *float64 `json:"uncertainty_metric"`

	ModelVersion      *string `json:"model_version"`
	PredictionHorizon *int    `json:"prediction_horizon"`
}

func parsePredictionDate(raw string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", raw)
}

// POST /api/admin/weather-predictions
func CreateWeatherPrediction() gin.HandlerFunc {
	return func(c *gin.Context) {
		var input WeatherPredictionInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "Input tidak valid", "error": err.Error()})
			return
		}

		parsedUserID, err := uuid.Parse(input.UserID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "user_id tidak valid"})
			return
		}
		parsedStationID, err := uuid.Parse(input.StationID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "station_id tidak valid"})
			return
		}
		predictionDate, err := parsePredictionDate(input.PredictionDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "prediction_date tidak valid, gunakan format YYYY-MM-DD atau RFC3339"})
			return
		}

		prediction := models.WeatherPrediction{
			UserID:            models.UUIDBinary(parsedUserID),
			StationID:         models.UUIDBinary(parsedStationID),
			PredictionDate:    predictionDate,
			Temperature:       input.Temperature,
			Humidity:          input.Humidity,
			Rainfall:          input.Rainfall,
			AirPressure:       input.AirPressure,
			WindSpeed:         input.WindSpeed,
			WindDirection:     input.WindDirection,
			EnsoIndex:         input.EnsoIndex,
			ClimatePhase:      input.ClimatePhase,
			StatusWeather:     input.StatusWeather,
			ConfidenceScore:   input.ConfidenceScore,
			UncertaintyMetric: input.UncertaintyMetric,
			ModelVersion:      input.ModelVersion,
			PredictionHorizon: input.PredictionHorizon,
		}

		if err := config.DB.Create(&prediction).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal membuat weather prediction", "error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, prediction)
	}
}

// PUT /api/admin/weather-predictions/:id
func UpdateWeatherPrediction() gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		parsedID, err := uuid.Parse(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "ID tidak valid"})
			return
		}

		var prediction models.WeatherPrediction
		if err := config.DB.Where("id = ?", models.UUIDBinary(parsedID)).First(&prediction).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"message": "Weather prediction tidak ditemukan"})
			return
		}

		var input WeatherPredictionInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "Input tidak valid", "error": err.Error()})
			return
		}

		parsedUserID, err := uuid.Parse(input.UserID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "user_id tidak valid"})
			return
		}
		parsedStationID, err := uuid.Parse(input.StationID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "station_id tidak valid"})
			return
		}
		predictionDate, err := parsePredictionDate(input.PredictionDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "prediction_date tidak valid"})
			return
		}

		prediction.UserID = models.UUIDBinary(parsedUserID)
		prediction.StationID = models.UUIDBinary(parsedStationID)
		prediction.PredictionDate = predictionDate
		prediction.Temperature = input.Temperature
		prediction.Humidity = input.Humidity
		prediction.Rainfall = input.Rainfall
		prediction.AirPressure = input.AirPressure
		prediction.WindSpeed = input.WindSpeed
		prediction.WindDirection = input.WindDirection
		prediction.EnsoIndex = input.EnsoIndex
		prediction.ClimatePhase = input.ClimatePhase
		prediction.StatusWeather = input.StatusWeather
		prediction.ConfidenceScore = input.ConfidenceScore
		prediction.UncertaintyMetric = input.UncertaintyMetric
		prediction.ModelVersion = input.ModelVersion
		prediction.PredictionHorizon = input.PredictionHorizon

		if err := config.DB.Save(&prediction).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal update weather prediction", "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, prediction)
	}
}

// DELETE /api/admin/weather-predictions/:id
func DeleteWeatherPrediction() gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		parsedID, err := uuid.Parse(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "ID tidak valid"})
			return
		}

		result := config.DB.Where("id = ?", models.UUIDBinary(parsedID)).Delete(&models.WeatherPrediction{})
		if result.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal hapus weather prediction", "error": result.Error.Error()})
			return
		}
		if result.RowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"message": "Weather prediction tidak ditemukan"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Weather prediction berhasil dihapus"})
	}
}