package repositories

import (
	"context"
	"database/sql"
	"errors"

	"weather-backend/models"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

// StationItem: bentuk JSON sama dengan models.Station lama (tanpa creator/updater).
type StationItem struct {
	ID           models.UUIDBinary  `json:"id"`
	StationCode  string             `json:"station_code"`
	StationName  string             `json:"station_name"`
	Latitude     float64            `json:"latitude"`
	Longitude    float64            `json:"longitude"`
	Region       *string            `json:"region"`
	DataSource   *string            `json:"data_source"`
	StatusActive bool               `json:"status_active"`
	CreatedBy    *models.UUIDBinary `json:"created_by"`
	UpdatedBy    *models.UUIDBinary `json:"updated_by"`
}

// StationWrite: data untuk create/update. StatusActive nil = default (create: aktif, update: tidak diubah).
type StationWrite struct {
	StationCode  string
	StationName  string
	Latitude     float64
	Longitude    float64
	Region       *string
	DataSource   *string
	StatusActive *bool
	ActorID      *models.UUIDBinary // user yang melakukan aksi, boleh nil
}

type StationRepository struct{ db DBTX }

func NewStationRepository(db DBTX) *StationRepository { return &StationRepository{db: db} }

const stationColumns = `id, station_code, station_name, latitude, longitude,
	region, data_source, status_active, created_by, updated_by`

func scanStation(sc interface{ Scan(...any) error }) (*StationItem, error) {
	var s StationItem
	err := sc.Scan(&s.ID, &s.StationCode, &s.StationName, &s.Latitude, &s.Longitude,
		&s.Region, &s.DataSource, &s.StatusActive, &s.CreatedBy, &s.UpdatedBy)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// IsForeignKeyViolation true jika error MySQL 1451 (masih dipakai tabel lain)
// atau 1452 (referensi ke data yang tidak ada).
func IsForeignKeyViolation(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && (me.Number == 1451 || me.Number == 1452)
}

func (r *StationRepository) List(ctx context.Context) ([]StationItem, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+stationColumns+` FROM stations ORDER BY station_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []StationItem{}
	for rows.Next() {
		s, err := scanStation(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *s)
	}
	return list, rows.Err()
}

func (r *StationRepository) Get(ctx context.Context, id models.UUIDBinary) (*StationItem, error) {
	s, err := scanStation(r.db.QueryRowContext(ctx,
		`SELECT `+stationColumns+` FROM stations WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

func (r *StationRepository) Create(ctx context.Context, in StationWrite) (*StationItem, error) {
	id := models.UUIDBinary(uuid.New()) // hook BeforeCreate GORM tidak ada lagi
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO stations
		  (id, station_code, station_name, latitude, longitude, region, data_source, status_active, created_by, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, COALESCE(?, 1), ?, ?)`,
		id, in.StationCode, in.StationName, in.Latitude, in.Longitude,
		in.Region, in.DataSource, in.StatusActive, in.ActorID, in.ActorID)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

// Update menimpa semua field (region/data_source nil = NULL, sama seperti GORM Save).
// status_active dan updated_by hanya berubah jika nilainya dikirim.
func (r *StationRepository) Update(ctx context.Context, id models.UUIDBinary, in StationWrite) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE stations SET
		  station_code = ?, station_name = ?, latitude = ?, longitude = ?,
		  region = ?, data_source = ?,
		  status_active = COALESCE(?, status_active),
		  updated_by = COALESCE(?, updated_by)
		WHERE id = ?`,
		in.StationCode, in.StationName, in.Latitude, in.Longitude,
		in.Region, in.DataSource, in.StatusActive, in.ActorID, id)
	return err
}

// Delete mengembalikan false jika station tidak ada.
func (r *StationRepository) Delete(ctx context.Context, id models.UUIDBinary) (bool, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM stations WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}