package repositories

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"weather-backend/models"
)

// Versi model yang disembunyikan dari endpoint publik.
var publicExcludedVersions = []any{"lstm-v0.1-dummy", "lstm-enso-v2-30years"}

// PredictionUser sengaja hanya berisi data aman (tanpa password dan email).
type PredictionUser struct {
	ID       models.UUIDBinary `json:"id"`
	Username string            `json:"username"`
	Name     string            `json:"name"`
}

type PredictionItem struct {
	ID                models.UUIDBinary `json:"id"`
	UserID            models.UUIDBinary `json:"user_id"`
	StationID         models.UUIDBinary `json:"station_id"`
	PredictionDate    time.Time         `json:"prediction_date"`
	Temperature       float64           `json:"temperature"`
	Humidity          float64           `json:"humidity"`
	Rainfall          float64           `json:"rainfall"`
	AirPressure       float64           `json:"air_pressure"`
	WindSpeed         *float64          `json:"wind_speed"`
	WindDirection     *float64          `json:"wind_direction"`
	EnsoIndex         *float64          `json:"enso_index"`
	ClimatePhase      *string           `json:"climate_phase"`
	StatusWeather     string            `json:"status_weather"`
	ConfidenceScore   *float64          `json:"confidence_score"`
	UncertaintyMetric *float64          `json:"uncertainty_metric"`
	ModelVersion      *string           `json:"model_version"`
	PredictionHorizon *int              `json:"prediction_horizon"`

	User    *PredictionUser `json:"user,omitempty"`
	Station *StationItem    `json:"station,omitempty"`
}

// PredictionWrite: data untuk create/update (tanpa ID dan relasi).
type PredictionWrite struct {
	UserID            models.UUIDBinary
	StationID         models.UUIDBinary
	PredictionDate    time.Time
	Temperature       float64
	Humidity          float64
	Rainfall          float64
	AirPressure       float64
	WindSpeed         *float64
	WindDirection     *float64
	EnsoIndex         *float64
	ClimatePhase      *string
	StatusWeather     string
	ConfidenceScore   *float64
	UncertaintyMetric *float64
	ModelVersion      *string
	PredictionHorizon *int
}

// PredictionFilter: semua field opsional. Hanya fragmen SQL tetap yang dirangkai,
// nilai selalu lewat placeholder.
type PredictionFilter struct {
	ID         *models.UUIDBinary
	UserID     *models.UUIDBinary
	StationID  *models.UUIDBinary
	PublicOnly bool
}

func (f PredictionFilter) where() (string, []any) {
	var conds []string
	var args []any
	if f.ID != nil {
		conds = append(conds, "wp.id = ?")
		args = append(args, *f.ID)
	}
	if f.UserID != nil {
		conds = append(conds, "wp.user_id = ?")
		args = append(args, *f.UserID)
	}
	if f.StationID != nil {
		conds = append(conds, "wp.station_id = ?")
		args = append(args, *f.StationID)
	}
	if f.PublicOnly {
		conds = append(conds, "(wp.model_version IS NULL OR wp.model_version NOT IN (?, ?))")
		args = append(args, publicExcludedVersions...)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

type PredictionRepository struct{ db DBTX }

func NewPredictionRepository(db DBTX) *PredictionRepository { return &PredictionRepository{db: db} }

const selectPrediction = `
	SELECT
	  wp.id, wp.user_id, wp.station_id, wp.prediction_date,
	  wp.temperature, wp.humidity, wp.rainfall, wp.air_pressure,
	  wp.wind_speed, wp.wind_direction, wp.enso_index, wp.climate_phase,
	  wp.status_weather, wp.confidence_score, wp.uncertainty_metric,
	  wp.model_version, wp.prediction_horizon,
	  u.id, u.username, u.name,
	  s.id, s.station_code, s.station_name, s.latitude, s.longitude,
	  s.region, s.data_source, s.status_active, s.created_by, s.updated_by
	FROM weather_predictions wp
	LEFT JOIN users u ON u.id = wp.user_id
	LEFT JOIN stations s ON s.id = wp.station_id`

func scanPrediction(sc interface{ Scan(...any) error }) (*PredictionItem, error) {
	var p PredictionItem
	var (
		uID                         *models.UUIDBinary
		uUsername, uName            *string
		sID, sCreatedBy, sUpdatedBy *models.UUIDBinary
		sCode, sName                *string
		sLat, sLon                  *float64
		sRegion, sSource            *string
		sActive                     *bool
	)
	err := sc.Scan(
		&p.ID, &p.UserID, &p.StationID, &p.PredictionDate,
		&p.Temperature, &p.Humidity, &p.Rainfall, &p.AirPressure,
		&p.WindSpeed, &p.WindDirection, &p.EnsoIndex, &p.ClimatePhase,
		&p.StatusWeather, &p.ConfidenceScore, &p.UncertaintyMetric,
		&p.ModelVersion, &p.PredictionHorizon,
		&uID, &uUsername, &uName,
		&sID, &sCode, &sName, &sLat, &sLon,
		&sRegion, &sSource, &sActive, &sCreatedBy, &sUpdatedBy,
	)
	if err != nil {
		return nil, err
	}
	if uID != nil {
		p.User = &PredictionUser{ID: *uID, Username: deref(uUsername), Name: deref(uName)}
	}
	if sID != nil {
		p.Station = &StationItem{
			ID: *sID, StationCode: deref(sCode), StationName: deref(sName),
			Latitude: derefF(sLat), Longitude: derefF(sLon),
			Region: sRegion, DataSource: sSource, StatusActive: sActive != nil && *sActive,
			CreatedBy: sCreatedBy, UpdatedBy: sUpdatedBy,
		}
	}
	return &p, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefF(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

func (r *PredictionRepository) List(ctx context.Context, f PredictionFilter) ([]PredictionItem, error) {
	where, args := f.where()
	rows, err := r.db.QueryContext(ctx,
		selectPrediction+where+` ORDER BY wp.prediction_date DESC, wp.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []PredictionItem{}
	for rows.Next() {
		p, err := scanPrediction(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *p)
	}
	return list, rows.Err()
}

func (r *PredictionRepository) Get(ctx context.Context, id models.UUIDBinary) (*PredictionItem, error) {
	p, err := scanPrediction(r.db.QueryRowContext(ctx, selectPrediction+` WHERE wp.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

func (r *PredictionRepository) Create(ctx context.Context, in PredictionWrite) (*PredictionItem, error) {
	id := models.NewUUIDBinary() // UUID v7
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO weather_predictions
		  (id, user_id, station_id, prediction_date, temperature, humidity, rainfall, air_pressure,
		   wind_speed, wind_direction, enso_index, climate_phase, status_weather,
		   confidence_score, uncertainty_metric, model_version, prediction_horizon)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.UserID, in.StationID, in.PredictionDate, in.Temperature, in.Humidity, in.Rainfall, in.AirPressure,
		in.WindSpeed, in.WindDirection, in.EnsoIndex, in.ClimatePhase, in.StatusWeather,
		in.ConfidenceScore, in.UncertaintyMetric, in.ModelVersion, in.PredictionHorizon)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

// Update menimpa semua field (field opsional yang nil menjadi NULL, sama seperti GORM Save).
func (r *PredictionRepository) Update(ctx context.Context, id models.UUIDBinary, in PredictionWrite) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE weather_predictions SET
		  user_id = ?, station_id = ?, prediction_date = ?,
		  temperature = ?, humidity = ?, rainfall = ?, air_pressure = ?,
		  wind_speed = ?, wind_direction = ?, enso_index = ?, climate_phase = ?,
		  status_weather = ?, confidence_score = ?, uncertainty_metric = ?,
		  model_version = ?, prediction_horizon = ?
		WHERE id = ?`,
		in.UserID, in.StationID, in.PredictionDate,
		in.Temperature, in.Humidity, in.Rainfall, in.AirPressure,
		in.WindSpeed, in.WindDirection, in.EnsoIndex, in.ClimatePhase,
		in.StatusWeather, in.ConfidenceScore, in.UncertaintyMetric,
		in.ModelVersion, in.PredictionHorizon, id)
	return err
}

// Delete mengembalikan false jika data tidak ada.
func (r *PredictionRepository) Delete(ctx context.Context, id models.UUIDBinary) (bool, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM weather_predictions WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}