package repositories

import (
	"context"
	"database/sql"
	"errors"

	"weather-backend/models"

	"github.com/go-sql-driver/mysql"
)

// StationItem adalah representasi satu station yang dikirim ke client.
//
// Field nullable (Region, DataSource) memakai *string karena di database
// kolomnya bisa NULL — frontend perlu membedakan "tidak ada" (null) dari
// string kosong ("").
//
// CreatedBy / UpdatedBy memakai *models.UUIDBinary karena:
//   - Bisa NULL di database (station lama yang dibuat sebelum ada audit).
//   - Saat create/update, nilai ini diisi dari JWT (bukan dari body),
//     jadi client tidak bisa memalsukannya.
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

// StationWrite adalah payload untuk operasi INSERT/UPDATE.
//
// Dipisah dari StationItem karena:
//   - Tidak ada ID (dibuat di repo saat Create).
//   - Tidak ada CreatedBy/UpdatedBy di input body — nilai actor diisi
//     terpisah via field ActorID (berasal dari JWT, bukan dari client).
//   - StatusActive memakai *bool (bukan bool), supaya bisa membedakan:
//       * nil  -> field tidak dikirim client.
//       * !=nil -> client mengirim nilai (true/false) secara eksplisit.
//     Perbedaan ini penting untuk update: nilai nil berarti "jangan
//     ubah", bukan "set ke false".
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

// stationColumns adalah daftar kolom yang selalu di-SELECT.
// Ditaruh sebagai konstanta agar urutannya SELALU sinkron dengan
// scanStation(); kalau nanti ada kolom baru, cukup ubah di satu tempat
// dan sesuaikan scanStation — List dan Get otomatis ikut.
const stationColumns = `id, station_code, station_name, latitude, longitude,
	region, data_source, status_active, created_by, updated_by`

// scanStation membaca satu baris hasil query menjadi StationItem.
//
// Parameter memakai `interface { Scan(...any) error }` agar bisa menerima
// baik *sql.Row (dari QueryRowContext) maupun *sql.Rows (dari
// QueryContext), sehingga logic scan tidak perlu diduplikasi di Get & List.
//
// Kolom nullable (Region, DataSource, CreatedBy, UpdatedBy) di-scan ke
// pointer agar NULL dari database tetap valid (tidak error "converting
// NULL to string").
func scanStation(sc interface{ Scan(...any) error }) (*StationItem, error) {
	var s StationItem
	err := sc.Scan(&s.ID, &s.StationCode, &s.StationName, &s.Latitude, &s.Longitude,
		&s.Region, &s.DataSource, &s.StatusActive, &s.CreatedBy, &s.UpdatedBy)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// IsForeignKeyViolation mengembalikan true jika error adalah pelanggaran
// foreign key MySQL:
//
//   - 1451 (ER_ROW_IS_REFERENCED_2): baris MASIH dirujuk tabel lain
//     (mis. station masih dipakai weather_predictions) — muncul saat DELETE.
//   - 1452 (ER_NO_REFERENCED_ROW_2): referensi ke baris yang TIDAK ADA
//     (mis. user_id/station_id di input tidak valid) — muncul saat INSERT/UPDATE.
//
// Kenapa perlu helper ini?
//   - MySQL tidak memberi error bertipe khusus; hanya kode numerik.
//   - Controller perlu membedakan kedua kasus ini (keduanya 400/409,
//     bukan 500) agar pesan error yang dikirim ke client informatif.
//   - errors.As dipakai (bukan type assertion biasa) agar tetap bekerja
//     kalau error sudah dibungkus (wrapped) oleh fmt.Errorf("%w", ...).
func IsForeignKeyViolation(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && (me.Number == 1451 || me.Number == 1452)
}

// List mengembalikan seluruh station diurutkan berdasarkan nama.
//
// Trik yang dipakai:
//   - `list := []StationItem{}` (bukan `var list []StationItem`) agar
//     JSON response berupa `[]` saat kosong, bukan `null`.
//   - `rows.Err()` diperiksa di akhir untuk menangkap error yang muncul
//     saat iterasi (mis. koneksi putus di tengah stream). Tanpa ini,
//     error tersebut hilang diam-diam.
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

// Get mengambil satu station berdasarkan ID.
// sql.ErrNoRows diterjemahkan menjadi ErrNotFound agar controller
// cukup memeriksa `errors.Is(err, ErrNotFound)` untuk membalas 404.
func (r *StationRepository) Get(ctx context.Context, id models.UUIDBinary) (*StationItem, error) {
	s, err := scanStation(r.db.QueryRowContext(ctx,
		`SELECT `+stationColumns+` FROM stations WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

// Create menyisipkan station baru, lalu mengembalikan data lengkap yang
// baru dibuat.
//
// Detail penting pada query INSERT:
//
//  1. `COALESCE(?, 1)` untuk status_active:
//     - Jika client mengirim status_active (nil != true/false), nilai
//       itu yang dipakai.
//     - Jika client tidak mengirim (StationWrite.StatusActive = nil),
//       parameter menjadi NULL, dan COALESCE(NULL, 1) menghasilkan 1
//       (artinya: station baru default AKTIF).
//     - Kenapa default aktif? Karena station yang baru dibuat biasanya
//       langsung dianggap tersedia; user tetap bisa set false jika perlu.
//
//  2. created_by dan updated_by diisi `in.ActorID` yang berasal dari
//     JWT — BUKAN dari body request. Ini mencegah client memalsukan
//     "siapa yang membuat station".
//
//  3. Setelah INSERT, kita panggil `r.Get(ctx, id)` karena:
//     - INSERT tidak mengembalikan nilai kolom DEFAULT dari database.
//     - Response ingin menyertakan data lengkap termasuk timestamp/
//       kolom lain yang mungkin diisi database.
func (r *StationRepository) Create(ctx context.Context, in StationWrite) (*StationItem, error) {
	id := models.NewUUIDBinary() // UUID v7
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

// Update menimpa sebagian field station.
//
// Perilaku kolom per kolom (penting untuk dipahami):
//
//   - station_code, station_name, latitude, longitude, region, data_source:
//     SELALU ditimpa, termasuk dengan NULL jika input nil. Artinya
//     field-field ini mengikuti semantik "full replace".
//
//   - status_active dan updated_by: memakai COALESCE(?, kolom_sekarang).
//     Artinya:
//       * Jika input nil  -> kolom LAMA dipertahankan (tidak berubah).
//       * Jika input !=nil -> kolom diisi nilai baru.
//     Ini sengaja berbeda dari field lain, karena:
//       * status_active memakai *bool: nil berarti "client tidak
//         menyentuh status aktif", bukan "set ke false".
//       * updated_by berasal dari JWT (ActorID), dan jika tidak ada
//         actor (mis. skenario internal), kita tidak ingin menghapus
//         jejak siapa yang terakhir mengubah.
//
// Catatan:
//   - Method ini TIDAK memeriksa keberadaan station; MySQL tidak error
//     saat UPDATE mengenai 0 baris. Pengecekan keberadaan dilakukan di
//     controller (dengan Get sebelum Update).
//   - Duplikasi station_code akan muncul sebagai MySQL 1062; controller
//     menerjemahkannya menjadi 409 via IsDuplicateEntry().
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

// Delete menghapus station berdasarkan ID.
//
// Return:
//   - (true,  nil) : station benar-benar terhapus.
//   - (false, nil) : tidak ada baris dengan ID tersebut — controller
//     harus membalas 404, bukan 200 "berhasil".
//   - (false, err) : kegagalan query, bisa karena foreign key violation
//     (lihat IsForeignKeyViolation di controller).
//
// `RowsAffected()` dipakai karena `ExecContext` pada DELETE tidak
// mengembalikan error saat 0 baris terhapus; tanpa ini, controller
// tidak bisa membedakan "berhasil hapus" dari "tidak ditemukan".
func (r *StationRepository) Delete(ctx context.Context, id models.UUIDBinary) (bool, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM stations WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}