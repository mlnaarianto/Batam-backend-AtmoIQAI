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

// Permission khusus yang menentukan apakah user boleh melihat prediksi
// milik SEMUA user. Tanpa permission ini, user hanya boleh melihat
// prediksi miliknya sendiri — lihat GetWeatherPredictions & GetWeatherPredictionByID.
const permViewAllPredictions = "weather-predictions:view-all"

// parseBinaryUUID adalah shortcut untuk mengubah string UUID menjadi
// models.UUIDBinary (tipe yang dipakai di seluruh repository).
// Jika string bukan UUID valid, error dari uuid.Parse diteruskan ke pemanggil
// agar handler bisa membalas 400 dengan pesan yang sesuai konteks.
func parseBinaryUUID(s string) (models.UUIDBinary, error) {
	p, err := uuid.Parse(s)
	return models.UUIDBinary(p), err
}

// =============================================================================
// LIST PREDIKSI
// =============================================================================

// GetPublicWeatherPredictions mengembalikan daftar prediksi cuaca untuk
// endpoint PUBLIK (tanpa login).
//
// Endpoint: GET /api/public/weather-predictions
//
// Kenapa harus dipisah dari GetWeatherPredictions?
// Karena endpoint ini tidak punya konteks user (tidak ada JWT), kita tidak
// bisa memeriksa permission. Untuk mencegah data internal bocor, repository
// diberi flag PublicOnly=true yang di dalamnya memfilter:
//
//   - Data AI yang dihasilkan saat user login (model_version "lstm-enso-v2-30years")
//   - Data dummy hasil seeder (model_version "lstm-v0.1-dummy")
//
// Dengan begitu, endpoint publik hanya menampilkan data yang memang layak
// untuk publik (mis. prediksi referensi/umum), bukan seluruh isi tabel.
//
// Filter opsional `?station_id=` memungkinkan frontend menampilkan
// prediksi untuk satu stasiun saja.
func GetPublicWeatherPredictions() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Flag PublicOnly WAJIB true di endpoint ini. Jangan pernah diset false
		// di sini karena endpoint ini terbuka tanpa autentikasi.
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

// GetWeatherPredictions mengembalikan daftar prediksi dengan aturan
// visibilitas berdasarkan permission "weather-predictions:view-all".
//
// Endpoint: GET /api/weather-predictions (butuh login, tanpa RequirePermission
// di routes.go karena aturan aksesnya lebih halus — ditangani di sini).
//
// Aturan visibilitas:
//
//  1. User PUNYA permission view-all:
//     - Boleh melihat prediksi SEMUA user.
//     - Query ?user_id=... boleh dipakai sebagai filter tambahan
//       (mis. admin ingin fokus ke satu user tertentu).
//
//  2. User TIDAK punya permission view-all:
//     - Hanya boleh melihat prediksi MILIKNYA sendiri.
//     - Query ?user_id=... SENGAJA DIABAIKAN. Jika tetap diproses,
//       user bisa memakai ?user_id=<uuid orang lain> untuk mengintip
//       data user lain (IDOR).
//
// Filter lain yang selalu dihormati (selama data memang milik user yang
// berhak): ?id=, ?station_id=.
//
// Catatan: filter `?id=` ada di sini (bukan di endpoint terpisah) agar
// frontend bisa melakukan "View per ID" lewat list endpoint yang sama.
// Namun endpoint GET /:id tetap disediakan untuk kebutuhan detail
// (lihat GetWeatherPredictionByID).
func GetWeatherPredictions() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		// actorID mengembalikan nil kalau user_id tidak ada / bukan string /
		// bukan UUID. Ini bisa terjadi jika middleware gagal mengisi context.
		// Anggap sebagai akses ditolak untuk berjaga-jaga.
		uid := actorID(c)
		if uid == nil {
			c.JSON(http.StatusForbidden, gin.H{"message": "Akses ditolak"})
			return
		}

		// Cek apakah user berhak melihat semua data. HasPermission melakukan
		// bypass admin di dalam query-nya, jadi admin otomatis canViewAll=true.
		canViewAll, err := repositories.NewPermissionRepository(config.SQLDB).
			HasPermission(ctx, *uid, permViewAllPredictions)
		if err != nil {
			config.Log.Error("Gagal cek permission view-all", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Terjadi kesalahan server"})
			return
		}

		f := repositories.PredictionFilter{}
		if canViewAll {
			// User istimewa: ?user_id= dipakai sebagai filter opsional.
			if s := c.Query("user_id"); s != "" {
				id, err := parseBinaryUUID(s)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"message": "user_id tidak valid"})
					return
				}
				f.UserID = &id
			}
		} else {
			// User biasa: PAKSA filter ke dirinya sendiri, abaikan ?user_id=.
			// Ini kunci anti-IDOR pada level list.
			f.UserID = uid
		}

		// Filter opsional ?id= untuk kebutuhan "View per ID" dari list.
		if s := c.Query("id"); s != "" {
			id, err := parseBinaryUUID(s)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"message": "id tidak valid"})
				return
			}
			f.ID = &id
		}

		// Filter opsional ?station_id= untuk melihat prediksi per stasiun.
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

// GetWeatherPredictionByID mengembalikan detail satu prediksi berdasarkan ID.
//
// Endpoint: GET /api/weather-predictions/:id (butuh permission
// "weather-predictions:view-id" di routes.go).
//
// Kenapa masih ada pengecekan kepemilikan di sini?
// Karena permission "view-id" hanya menjawab "apakah user boleh memanggil
// endpoint ini", BUKAN "apakah user boleh melihat data ID ini". Tanpa
// pengecekan tambahan, siapa pun yang punya permission view-id bisa menebak
// UUID prediksi user lain dan melihat datanya — inilah celah IDOR klasik
// (Insecure Direct Object Reference).
//
// Aturan:
//   - canViewAll=true  -> boleh lihat semua ID.
//   - canViewAll=false -> hanya boleh jika prediction.UserID == user sendiri.
func GetWeatherPredictionByID() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		id, err := parseBinaryUUID(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "ID tidak valid"})
			return
		}

		// Ambil data dulu. Jika tidak ada, langsung 404 — tidak perlu lanjut
		// cek permission karena tidak ada yang bisa dibocorkan.
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

		// Inti anti-IDOR: kalau tidak punya view-all, hanya boleh akses
		// miliknya sendiri. Perhatikan kita membandingkan UserID hasil query,
		// bukan input dari user — jadi tidak bisa diakali lewat parameter.
		if !canViewAll && prediction.UserID != *uid {
			c.JSON(http.StatusForbidden, gin.H{"message": "Anda tidak memiliki akses ke data ini"})
			return
		}

		c.JSON(http.StatusOK, prediction)
	}
}

// =============================================================================
// INPUT & KONVERSI
// =============================================================================

// WeatherPredictionInput adalah payload untuk create/update prediksi.
//
// JEBAKAN PENTING soal binding:"required" pada float64:
// validator Go menganggap angka 0 sebagai "kosong". Jadi jika Rainfall
// diberi tag binding:"required", nilai 0.0 (artinya "tidak hujan" dan
// itu nilai yang SAH) akan ditolak sebagai input tidak valid.
//
// Karena itu:
//   - Field string yang wajib (UserID, StationID, PredictionDate,
//     StatusWeather) tetap memakai binding:"required".
//   - Field numerik TIDAK memakai binding:"required"; kesalahan format
//     angka tetap akan ditangkap oleh JSON decoder sebagai error bind.
//
// Field opsional memakai pointer (*float64, *string, *int) agar bisa
// membedakan "tidak dikirim" (nil) dari "dikirim dengan nilai 0" (pointer
// ke 0). Ini berguna untuk update parsial di masa depan dan untuk
// mempertahankan nilai NULL di database.
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

	// Info tentang model AI yang menghasilkan prediksi ini (opsional,
	// bisa dikosongkan jika prediksi diinput manual).
	ModelVersion      *string `json:"model_version"`
	PredictionHorizon *int    `json:"prediction_horizon"`
}

// parsePredictionDate mencoba dua format tanggal:
//
//  1. RFC3339 (mis. "2025-01-15T08:00:00Z") — format lengkap dengan zona waktu.
//  2. "2006-01-02" (mis. "2025-01-15") — format tanggal saja, umum dipakai
//     di form.
//
// RFC3339 dicoba lebih dulu karena lebih ketat/spesifik. Jika gagal,
// fallback ke format tanggal saja.
func parsePredictionDate(raw string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", raw)
}

// toWrite mengubah WeatherPredictionInput menjadi repositories.PredictionWrite.
//
// Return kedua (string) adalah pesan error yang siap dikirim ke client.
// Jika string kosong, artinya konversi berhasil. Pola ini dipakai agar
// pemanggil tidak perlu mengulang pengecekan yang sama di beberapa handler
// (Create & Update).
//
// Validasi yang dilakukan:
//   - UserID & StationID: harus UUID valid.
//   - PredictionDate: harus salah satu format yang didukung parsePredictionDate.
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

// =============================================================================
// CRUD
// =============================================================================

// CreateWeatherPrediction membuat prediksi cuaca baru.
//
// Endpoint: POST /api/weather-predictions (butuh permission
// "manage-weather-predictions" di routes.go).
//
// Perilaku khusus:
//   - Jika user_id atau station_id yang dirujuk tidak ada di database,
//     MySQL mengembalikan error foreign key. Kasus ini dibalas 400
//     (bukan 500) karena penyebabnya input user, dan pesan errornya
//     dibuat jelas agar frontend bisa menampilkan feedback yang tepat.
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

// UpdateWeatherPrediction mengubah prediksi yang sudah ada.
//
// Endpoint: PUT /api/weather-predictions/:id (butuh permission
// "manage-weather-predictions" di routes.go).
//
// Alur:
//  1. Validasi format UUID di path.
//  2. Cek keberadaan prediksi dulu (SELECT). Ini penting karena UPDATE
//     di MySQL tidak error saat ID tidak ditemukan — hanya 0 row affected.
//     Dengan cek awal, kita bisa membalas 404 dengan jelas.
//  3. Bind body + validasi lewat toWrite.
//  4. Jalankan update. FK violation -> 400.
//  5. Baca ulang untuk mengembalikan data terbaru ke frontend.
//
// Catatan: update ini bersifat FULL REPLACE (semua field di body dipakai
// untuk menimpa). Field opsional yang dikirim nil (tidak ada di body)
// akan menjadi NULL di database, sesuai perilaku pointer.
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

// DeleteWeatherPrediction menghapus prediksi berdasarkan ID.
//
// Endpoint: DELETE /api/weather-predictions/:id (butuh permission
// "manage-weather-predictions" di routes.go).
//
// Nilai `deleted` dari repo dipakai untuk membedakan:
//   - false -> prediksi tidak ada, balas 404 (bukan 200 "berhasil").
//   - true  -> prediksi benar-benar terhapus, balas 200.
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