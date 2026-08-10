package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type WeatherPrediction struct {
	ID             UUIDBinary `json:"id" gorm:"type:binary(16);primaryKey;not null"`
	UserID         UUIDBinary `json:"user_id" gorm:"type:binary(16);not null"`
	StationID      UUIDBinary `json:"station_id" gorm:"type:binary(16);not null"`
	PredictionDate time.Time  `json:"prediction_date" gorm:"not null"`
	Temperature    float64    `json:"temperature" gorm:"type:decimal(5,2);not null"`
	Humidity       float64    `json:"humidity" gorm:"type:decimal(5,2);not null"`
	Rainfall       float64    `json:"rainfall" gorm:"type:decimal(6,2);not null"`
	AirPressure    float64    `json:"air_pressure" gorm:"type:decimal(6,2);not null"`
	WindSpeed      *float64   `json:"wind_speed" gorm:"type:decimal(5,2)"`
	WindDirection  *float64   `json:"wind_direction" gorm:"type:decimal(5,2)"`

	// Tambahan Parameter El Niño & La Niña
	EnsoIndex    *float64 `json:"enso_index" gorm:"type:decimal(4,2)"`
	ClimatePhase *string  `json:"climate_phase" gorm:"type:varchar(20)"`

	StatusWeather   string   `json:"status_weather" gorm:"not null"`
	ConfidenceScore *float64 `json:"confidence_score" gorm:"type:decimal(5,2)"`

	// Sebelumnya bernama RmseMetric/rmse_metric. Direname karena isinya
	// BUKAN RMSE klasik (yang butuh perbandingan dengan data cuaca aktual),
	// melainkan standar deviasi hasil MC Dropout -- ukuran ketidakstabilan
	// model per-prediksi. GORM otomatis memetakan nama field Go ini ke
	// kolom SQL "uncertainty_metric" (snake_case), sesuai migrasi
	// 002_rename_rmse_metric.sql.
	UncertaintyMetric *float64 `json:"uncertainty_metric" gorm:"type:decimal(6,4)"`

	ModelVersion      *string `json:"model_version"`
	PredictionHorizon *int    `json:"prediction_horizon"`

	// Relasi
	User    *User    `json:"user,omitempty" gorm:"foreignKey:UserID;references:ID"`
	Station *Station `json:"station,omitempty" gorm:"foreignKey:StationID;references:ID"`
}

// Hook GORM: Otomatis generate UUID v4 baru jika ID masih kosong
func (w *WeatherPrediction) BeforeCreate(tx *gorm.DB) (err error) {
	if uuid.UUID(w.ID) == uuid.Nil {
		w.ID = UUIDBinary(uuid.New())
	}
	return
}