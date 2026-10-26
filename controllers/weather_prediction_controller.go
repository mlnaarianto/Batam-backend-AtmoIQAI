package controllers

import (
	"errors"
	"net/http"
	"time"

	"weather-backend/config"
	"weather-backend/models"
	"weather-backend/repositories"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const permViewAllPredictions = "weather-predictions:view-all"

func parseBinaryUUID(s string) (models.UUIDBinary, error) {
	p, err := uuid.Parse(s)
	return models.UUIDBinary(p), err
}

// GET /api/public/weather-predictions (Publik / Tanpa Login)
// Hanya data publik murni. Data hasil AI yang dibuat saat user LOGIN
// (model_version "lstm-enso-v2-30years") dan data dummy seeder ("lstm-v0.1-dummy")
// tidak ditampilkan di sini.
func GetPublicWeatherPredictions() gin.HandlerFunc {
	return func(c *gin.Context) {
		f := repositories.PredictionFilter{PublicOnly: true}

		if s := c.Query("station_id"); s != "" {
			id, err := parseBinaryUUID(s)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": "station_id tidak valid"})
				return
			}
			f.StationID = &id
		}

		predictions, err := repositories.NewPredictionRepository(config.SQLDB).List(c.Request.Context(), f)
		if err != nil {
			config.Log.Error("Gagal mengambil data weather predictions publik", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal mengambil data weather predictions"})
			return
		}
		c.JSON(http.StatusOK, predictions)
	}
}

// GET /api/admin/weather-predictions
// Akses ditentukan oleh permission "weather-predictions:view-all":
//   - Punya permission itu -> lihat SEMUA data (query ?user_id= boleh dipakai sebagai filter).
//   - Tidak punya          -> hanya data milik user_id sendiri, query ?user_id= DIABAIKAN.
func GetWeatherPredictions() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		uid := actorID(c)
		if uid == nil {
			c.JSON(http.StatusForbidden, gin.H{"message": "Akses ditolak"})
			return
		}

		canViewAll, err := repositories.NewPermissionRepository(config.SQLDB).
			HasPermission(ctx, *uid, permViewAllPredictions)
		if err != nil {
			config.Log.Error("Gagal cek permission view-all", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Terjadi kesalahan server"})
			return
		}

		f := repositories.PredictionFilter{}
		if canViewAll {
			if s := c.Query("user_id"); s != "" {
				id, err := parseBinaryUUID(s)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"message": "user_id tidak valid"})
					return
				}
				f.UserID = &id
			}
		} else {
			f.UserID = uid // dikunci ke diri sendiri
		}

		// View Per ID lewat query ?id=xxx
		if s := c.Query("id"); s != "" {
			id, err := parseBinaryUUID(s)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": "id tidak valid"})
				return
			}
			f.ID = &id
		}

		if s := c.Query("station_id"); s != "" {
			id, err := parseBinaryUUID(s)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": "station_id tidak valid"})
				return
			}
			f.StationID = &id
		}

		predictions, err := repositories.NewPredictionRepository(config.SQLDB).List(ctx, f)
		if err != nil {
			config.Log.Error("Gagal mengambil data weather predictions", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal mengambil data weather predictions"})
			return
		}
		c.JSON(http.StatusOK, predictions)
	}
}

// GET /api/admin/weather-predictions/:id
// Tanpa "weather-predictions:view-all", hanya boleh melihat detail miliknya sendiri
// (mencegah IDOR: menebak UUID untuk mengintip data orang lain).
func GetWeatherPredictionByID() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		id, err := parseBinaryUUID(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "ID tidak valid"})
			return
		}

		prediction, err := repositories.NewPredictionRepository(config.SQLDB).Get(ctx, id)
		if errors.Is(err, repositories.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": "Weather prediction tidak ditemukan"})
			return
		}
		if err != nil {
			config.Log.Error("Gagal mengambil weather prediction", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal mengambil weather prediction"})
			return
		}

		uid := actorID(c)
		if uid == nil {
			c.JSON(http.StatusForbidden, gin.H{"message": "Akses ditolak"})
			return
		}
		canViewAll, err := repositories.NewPermissionRepository(config.SQLDB).
			HasPermission(ctx, *uid, permViewAllPredictions)
		if err != nil {
			config.Log.Error("Gagal cek permission view-all", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Terjadi kesalahan server"})
			return
		}
		if !canViewAll && prediction.UserID != *uid {
			c.JSON(http.StatusForbidden, gin.H{"message": "Anda tidak memiliki akses ke data ini"})
			return
		}

		c.JSON(http.StatusOK, prediction)
	}
}

// WeatherPredictionInput
//
// CATATAN soal binding:"required" pada field numerik (float64): validator menganggap
// nilai NOL sebagai "kosong", sehingga Rainfall=0.0 (tidak hujan, nilai sah) akan ditolak.
// Karena itu "required" hanya dipasang pada field string yang memang tidak boleh kosong.
type WeatherPredictionInput struct {
	UserID         string `json:"user_id" binding:"required"`
	StationID      string `json:"station_id" binding:"required"`
	PredictionDate string `json:"prediction_date" binding:"required"` // "2006-01-02" atau RFC3339

	Temperature float64 `json:"temperature"`
	Humidity    float64 `json:"humidity"`
	Rainfall    float64 `json:"rainfall"` // 0.0 = tidak hujan, JANGAN diberi binding:"required"
	AirPressure float64 `json:"air_pressure"`

	WindSpeed     *float64 `json:"wind_speed"`
	WindDirection *float64 `json:"wind_direction"`

	// Parameter El Niño & La Niña
	EnsoIndex    *float64 `json:"enso_index"`
	ClimatePhase *string  `json:"climate_phase"`

	StatusWeather   string   `json:"status_weather" binding:"required"`
	ConfidenceScore *float64 `json:"confidence_score"`

	// Mengikuti kolom DB uncertainty_metric (sebelumnya rmse_metric).
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

// toWrite memvalidasi UUID dan tanggal, lalu mengubah input menjadi data tulis.
func (in WeatherPredictionInput) toWrite() (repositories.PredictionWrite, string) {
	userID, err := parseBinaryUUID(in.UserID)
	if err != nil {
		return repositories.PredictionWrite{}, "user_id tidak valid"
	}
	stationID, err := parseBinaryUUID(in.StationID)
	if err != nil {
		return repositories.PredictionWrite{}, "station_id tidak valid"
	}
	date, err := parsePredictionDate(in.PredictionDate)
	if err != nil {
		return repositories.PredictionWrite{}, "prediction_date tidak valid, gunakan format YYYY-MM-DD atau RFC3339"
	}
	return repositories.PredictionWrite{
		UserID:            userID,
		StationID:         stationID,
		PredictionDate:    date,
		Temperature:       in.Temperature,
		Humidity:          in.Humidity,
		Rainfall:          in.Rainfall,
		AirPressure:       in.AirPressure,
		WindSpeed:         in.WindSpeed,
		WindDirection:     in.WindDirection,
		EnsoIndex:         in.EnsoIndex,
		ClimatePhase:      in.ClimatePhase,
		StatusWeather:     in.StatusWeather,
		ConfidenceScore:   in.ConfidenceScore,
		UncertaintyMetric: in.UncertaintyMetric,
		ModelVersion:      in.ModelVersion,
		PredictionHorizon: in.PredictionHorizon,
	}, ""
}

// POST /api/admin/weather-predictions
func CreateWeatherPrediction() gin.HandlerFunc {
	return func(c *gin.Context) {
		var input WeatherPredictionInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "Input tidak valid", "error": err.Error()})
			return
		}

		data, msg := input.toWrite()
		if msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": msg})
			return
		}

		prediction, err := repositories.NewPredictionRepository(config.SQLDB).Create(c.Request.Context(), data)
		if repositories.IsForeignKeyViolation(err) {
			c.JSON(http.StatusBadRequest, gin.H{"message": "user_id atau station_id tidak ditemukan"})
			return
		}
		if err != nil {
			config.Log.Error("Gagal membuat weather prediction", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal membuat weather prediction"})
			return
		}
		c.JSON(http.StatusCreated, prediction)
	}
}

// PUT /api/admin/weather-predictions/:id
func UpdateWeatherPrediction() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		id, err := parseBinaryUUID(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "ID tidak valid"})
			return
		}

		repo := repositories.NewPredictionRepository(config.SQLDB)

		if _, err := repo.Get(ctx, id); errors.Is(err, repositories.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": "Weather prediction tidak ditemukan"})
			return
		} else if err != nil {
			config.Log.Error("Gagal membaca weather prediction", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal update weather prediction"})
			return
		}

		var input WeatherPredictionInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "Input tidak valid", "error": err.Error()})
			return
		}

		data, msg := input.toWrite()
		if msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": msg})
			return
		}

		if err := repo.Update(ctx, id, data); err != nil {
			if repositories.IsForeignKeyViolation(err) {
				c.JSON(http.StatusBadRequest, gin.H{"message": "user_id atau station_id tidak ditemukan"})
				return
			}
			config.Log.Error("Gagal update weather prediction", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal update weather prediction"})
			return
		}

		prediction, err := repo.Get(ctx, id)
		if err != nil {
			config.Log.Error("Gagal membaca weather prediction setelah update", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal update weather prediction"})
			return
		}
		c.JSON(http.StatusOK, prediction)
	}
}

// DELETE /api/admin/weather-predictions/:id
func DeleteWeatherPrediction() gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := parseBinaryUUID(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "ID tidak valid"})
			return
		}

		deleted, err := repositories.NewPredictionRepository(config.SQLDB).Delete(c.Request.Context(), id)
		if err != nil {
			config.Log.Error("Gagal hapus weather prediction", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal hapus weather prediction"})
			return
		}
		if !deleted {
			c.JSON(http.StatusNotFound, gin.H{"message": "Weather prediction tidak ditemukan"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Weather prediction berhasil dihapus"})
	}
}