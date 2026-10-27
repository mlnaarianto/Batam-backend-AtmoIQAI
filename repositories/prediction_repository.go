package repositories

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"weather-backend/models"
)

// publicExcludedVersions adalah daftar model_version yang TIDAK boleh
// muncul di endpoint publik (GET /api/public/weather-predictions).
//
// Kenapa disembunyikan?
//   - "lstm-v0.1-dummy"        : data contoh hasil seeder, bukan prediksi nyata.
//   - "lstm-enso-v2-30years"   : hasil AI yang dihasilkan saat user login,
//     bersifat privat milik user tersebut.
//
// Daftar ini dipakai di PredictionFilter.where() untuk menyusun klausa
// `model_version NOT IN (?, ?)`. Kalau nanti ada versi model baru yang
// juga harus disembunyikan, tinggal tambahkan di sini — tidak perlu
// mengubah query di banyak tempat.
var publicExcludedVersions = []any{"lstm-v0.1-dummy", "lstm-enso-v2-30years"}

// PredictionUser adalah data user MINIMAL yang ikut dikirim bersama
// prediksi (di-embed sebagai relasi `user`).
//
// Kenapa dibatasi hanya ID, Username, Name?
//   - Endpoint prediksi bisa diakses user biasa (bukan hanya admin).
//   - Jangan sampai response ikut membocorkan password hash atau email
//     user lain hanya karena datanya di-JOIN di query.
//   - Struct terpisah (bukan reuse model User penuh) membuat kontrak
//     response eksplisit dan aman secara default.
type PredictionUser struct {
	ID       models.UUIDBinary `json:"id"`
	Username string            `json:"username"`
	Name     string            `json:"name"`
}

// PredictionItem adalah representasi satu baris prediksi yang dikirim
// ke client, sudah termasuk relasi `user` dan `station` (jika ada).
//
// Field opsional memakai pointer (*float64, *string, *int) karena di
// database kolomnya bisa NULL — dan client perlu membedakan antara
// "tidak ada nilai" (null) dan "nilai 0".
//
// User & Station memakai pointer agar bisa `omitempty`: jika LEFT JOIN
// tidak menemukan baris (mis. user sudah dihapus), field ini hilang
// dari JSON, bukan menjadi objek kosong yang membingungkan.
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

// PredictionWrite adalah payload untuk operasi INSERT/UPDATE.
//
// Berbeda dari PredictionItem, tipe ini:
//   - Tidak memiliki ID (ID dibuat di sisi repository saat Create).
//   - Tidak memiliki relasi User/Station (itu hanya untuk response).
//   - Hanya berisi kolom yang benar-benar ditulis ke tabel
//     weather_predictions.
//
// Memisahkan struct "write" dari struct "read" mencegah kesalahan
// seperti mengirim balik data relasi ke query INSERT.
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

// PredictionFilter berisi kriteria opsional untuk query List.
//
// Semua field memakai pointer agar bisa membedakan:
//   - nil  -> filter tidak aktif (kolom tidak masuk WHERE).
//   - !=nil -> filter aktif (kolom masuk WHERE dengan placeholder ?).
//
// PublicOnly adalah flag khusus (bukan filter kolom), yang bila true
// akan mengecualikan model_version tertentu dari hasil.
type PredictionFilter struct {
	ID         *models.UUIDBinary
	UserID     *models.UUIDBinary
	StationID  *models.UUIDBinary
	PublicOnly bool
}

// where menyusun klausa WHERE dinamis berdasarkan field yang terisi
// di PredictionFilter.
//
// Prinsip keamanan penting:
//   - HANYA fragmen SQL tetap (nama kolom + operator) yang dirangkai
//     menjadi string.
//   - SEMUA nilai user input dikirim sebagai argumen (placeholder `?`),
//     tidak pernah disisipkan ke string SQL. Ini mencegah SQL injection
//     sejak awal, tanpa perlu escape manual.
//
// Return:
//   - string: fragmen WHERE (diawali " WHERE ") atau "" bila tidak ada
//     filter aktif. Pemanggil tinggal menempelkannya ke query utama.
//   - []any: daftar argumen yang urutannya HARUS sama dengan urutan
//     placeholder di fragmen WHERE.
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
		// Perhatikan tanda kurung: WAJIB ada agar NOT IN hanya berlaku
		// untuk model_version, tidak "menelan" kondisi lain saat
		// kondisi digabung dengan AND.
		//
		// `model_version IS NULL` sengaja disertakan karena prediksi
		// yang diinput manual bisa memiliki model_version NULL, dan
		// NULL NOT IN (...) menghasilkan NULL (bukan true) di SQL —
		// tanpa klausa IS NULL, baris tersebut akan hilang dari hasil.
		conds = append(conds, "(wp.model_version IS NULL OR wp.model_version NOT IN (?, ?))")
		args = append(args, publicExcludedVersions...)
	}

	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// PredictionRepository menyimpan dependensi ke database.
// Pemakaian DBTX (bukan *sql.DB) memungkinkan repository ini dipakai
// baik untuk query langsung maupun di dalam transaksi.
type PredictionRepository struct{ db DBTX }

func NewPredictionRepository(db DBTX) *PredictionRepository { return &PredictionRepository{db: db} }

// selectPrediction adalah fragmen SELECT utama, dipakai oleh List dan Get.
//
// Kenapa ditaruh sebagai konstanta?
//   - Menjamin kolom yang di-Scan di scanPrediction selalu sinkron dengan
//     kolom yang di-SELECT (urutan dan jumlahnya).
//   - Kalau nanti ada kolom baru, cukup ubah di satu tempat, lalu
//     sesuaikan scanPrediction — List/Get otomatis ikut.
//
// Detail JOIN:
//   - LEFT JOIN users dan LEFT JOIN stations dipakai (bukan INNER) agar
//     prediksi tetap muncul walau user/station-nya sudah dihapus.
//     Konsekuensinya: kolom dari tabel yang di-LEFT JOIN bisa NULL,
//     itulah kenapa scanPrediction memakai pointer untuk kolom-kolom tsb.
//
//   - Untuk station, kita ikut mengambil created_by dan updated_by karena
//     StationItem menyertakan field tersebut (lihat struct StationItem).
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

// scanPrediction membaca satu baris hasil query menjadi PredictionItem.
//
// Kenapa parameter memakai interface { Scan(...any) error }?
// Supaya fungsi ini bisa menerima *sql.Row (dari QueryRowContext) maupun
// *sql.Rows (dari QueryContext) — keduanya punya method Scan dengan
// signature yang sama. Dengan begini logika scan tidak perlu diduplikasi
// di Get dan List.
//
// Kenapa kolom hasil LEFT JOIN di-scan ke pointer?
// Karena saat JOIN tidak menemukan pasangan, MySQL mengembalikan NULL.
// Jika di-scan langsung ke string/float64, akan error. Dengan pointer:
//   - pointer nil  -> kolom NULL -> relasi tidak diisi.
//   - pointer isi  -> kolom berisi -> relasi diisi.
func scanPrediction(sc interface{ Scan(...any) error }) (*PredictionItem, error) {
	var p PredictionItem

	// Variabel sementara untuk kolom dari LEFT JOIN.
	// Semua bertipe pointer karena bisa NULL.
	var (
		uID                         *models.UUIDBinary
		uUsername, uName            *string
		sID, sCreatedBy, sUpdatedBy *models.UUIDBinary
		sCode, sName                *string
		sLat, sLon                  *float64
		sRegion, sSource            *string
		sActive                     *bool
	)

	// Urutan argumen Scan HARUS sama persis dengan urutan kolom
	// di selectPrediction. Kalau tidak, akan muncul error "number of
	// destination arguments in Scan does not match" atau tipe salah.
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

	// Isi relasi user hanya jika kolom u.id tidak NULL.
	if uID != nil {
		p.User = &PredictionUser{ID: *uID, Username: deref(uUsername), Name: deref(uName)}
	}

	// Isi relasi station hanya jika kolom s.id tidak NULL.
	// StatusActive di-*dereference* dengan default false jika NULL —
	// lebih aman daripada default true (mencegah stasiun nonaktif
	// tiba-tiba terlihat aktif hanya karena kolomnya NULL).
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

// deref mengembalikan nilai string dari pointer, atau string kosong
// jika pointer nil. Dipakai untuk kolom string hasil LEFT JOIN.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// derefF mengembalikan nilai float64 dari pointer, atau 0 jika pointer nil.
// Dipakai untuk kolom numerik hasil LEFT JOIN.
func derefF(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

// List mengembalikan daftar prediksi sesuai filter.
//
// Alur:
//  1. Susun fragmen WHERE + args dari filter.
//  2. Gabungkan dengan selectPrediction dan ORDER BY.
//  3. Iterasi semua baris, scan tiap baris ke PredictionItem.
//
// Catatan:
//   - ORDER BY prediction_date DESC, id — urutan tetap dan deterministik
//     (id sebagai tiebreaker kalau ada tanggal yang sama), supaya hasil
//     konsisten dan mudah di-paginate di frontend.
//   - `list := []PredictionItem{}` (bukan `var list []PredictionItem`)
//     agar JSON mengembalikan `[]`, bukan `null`, saat hasil kosong.
//   - `rows.Err()` WAJIB diperiksa di akhir; tanpa ini, error yang
//     terjadi saat iterasi (mis. koneksi putus di tengah stream) akan
//     hilang diam-diam.
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

// Get mengambil satu prediksi berdasarkan ID.
//
// `sql.ErrNoRows` (tidak ada baris) diterjemahkan menjadi ErrNotFound
// (error sentinel tingkat aplikasi) agar controller tidak perlu tahu
// detail driver database; cukup cek `errors.Is(err, ErrNotFound)`.
func (r *PredictionRepository) Get(ctx context.Context, id models.UUIDBinary) (*PredictionItem, error) {
	p, err := scanPrediction(r.db.QueryRowContext(ctx, selectPrediction+` WHERE wp.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// Create menyisipkan satu baris prediksi baru lalu mengembalikan data
// yang baru dibuat.
//
// Kenapa ID dibuat di sini (bukan AUTO_INCREMENT)?
//   - UUID v7 (via NewUUIDBinary) memungkinkan client tahu ID tanpa
//     round-trip tambahan, sekaligus ramah index B-tree.
//
// Kenapa setelah INSERT langsung `return r.Get(ctx, id)`?
//   - Kita ingin response berisi data lengkap TERMASUK relasi user dan
//     station yang di-JOIN. INSERT hanya tahu kolom di tabel utama,
//     jadi pembacaan ulang lebih sederhana dan konsisten.
//   - Trade-off: satu query tambahan per create. Untuk skala aplikasi
//     ini, itu masih wajar dan menghindari duplikasi logic scan.
func (r *PredictionRepository) Create(ctx context.Context, in PredictionWrite) (*PredictionItem, error) {
	id := models.NewUUIDBinary()
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

// Update menimpa SEMUA kolom yang bisa ditulis (full replace).
//
// Perilaku penting:
//   - Field opsional (WindSpeed, WindDirection, EnsoIndex, dst.) yang
//     bernilai nil pada input akan menjadi NULL di database.
//   - Artinya, jika client ingin mempertahankan nilai lama, ia HARUS
//     mengirimkan nilai lama tersebut di body. Ini konsisten dengan
//     semantik "PUT" (replace), bukan "PATCH" (partial update).
//   - Tanggung jawab pengecekan keberadaan data ada di layer controller
//     (yang memanggil Get dulu sebelum Update), karena MySQL tidak
//     mengembalikan error saat UPDATE mengenai 0 baris.
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

// Delete menghapus satu baris berdasarkan ID.
//
// Return:
//   - (true,  nil) : baris benar-benar terhapus.
//   - (false, nil) : tidak ada baris dengan ID tersebut (perlu dibalas 404
//     oleh controller, BUKAN 200 "berhasil").
//   - (false, err) : kegagalan query.
//
// `RowsAffected()` dipakai karena `ExecContext` pada DELETE tidak
// mengembalikan error saat 0 baris terhapus. Tanpa ini, controller tidak
// bisa membedakan "berhasil hapus" dan "tidak ada yang dihapus".
func (r *PredictionRepository) Delete(ctx context.Context, id models.UUIDBinary) (bool, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM weather_predictions WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}