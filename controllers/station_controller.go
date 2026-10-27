package controllers

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"weather-backend/config"
	"weather-backend/models"
	"weather-backend/repositories"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// actorID mengambil user_id dari JWT yang sudah disisipkan AuthMiddleware
// ke Gin Context. Nilai dikembalikan sebagai pointer agar bisa dibedakan
// antara "tidak ada actor" (nil) dan "actor dengan UUID tertentu".
//
// Kenapa pointer? Karena repository menerima *models.UUIDBinary untuk
// field `created_by`/`updated_by` yang opsional. Kalau actor tidak ada,
// kita kirim nil sehingga kolom di database bisa tetap NULL.
//
// Catatan: AuthMiddleware menyimpan user_id sebagai string, makanya di
// sini kita lakukan type assertion ke string lalu parse ke UUID.
// Kalau assertion/parse gagal, kembalikan nil (pemanggil harus siap
// dengan actor kosong).
func actorID(c *gin.Context) *models.UUIDBinary {
	raw, _ := c.Get("user_id")
	s, ok := raw.(string)
	if !ok {
		return nil
	}
	p, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	id := models.UUIDBinary(p)
	return &id
}

// GetStations mengembalikan daftar seluruh station.
// Dipakai oleh dua rute:
//   - GET /api/public/stations  (publik, tanpa login)
//   - GET /api/stations         (butuh izin "manage-stations")
//
// Handler yang sama bisa dipakai di dua rute karena data yang dikembalikan
// identik; perbedaan hak akses ditangani di level routing (middleware
// RequirePermission), bukan di handler.
func GetStations() gin.HandlerFunc {
	return func(c *gin.Context) {
		stations, err := repositories.NewStationRepository(config.SQLDB).List(c.Request.Context())
		if err != nil {
			config.Log.Error("Gagal mengambil data stations", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal mengambil data stations"})
			return
		}
		c.JSON(http.StatusOK, stations)
	}
}

// GetStationByID mengembalikan detail satu station berdasarkan ID di URL.
// ID divalidasi formatnya sebagai UUID agar query tidak dijalankan dengan
// parameter yang jelas-jelas tidak valid (menghemat round-trip ke DB).
// Jika tidak ditemukan, balas 404; error lain dianggap 500.
func GetStationByID() gin.HandlerFunc {
	return func(c *gin.Context) {
		parsedID, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "ID tidak valid"})
			return
		}

		station, err := repositories.NewStationRepository(config.SQLDB).Get(c.Request.Context(), models.UUIDBinary(parsedID))
		if errors.Is(err, repositories.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": "Station tidak ditemukan"})
			return
		}
		if err != nil {
			config.Log.Error("Gagal mengambil station", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal mengambil station"})
			return
		}
		c.JSON(http.StatusOK, station)
	}
}

// StationInput adalah payload untuk create/update station.
//
// Field yang wajib (binding:"required"):
//   - station_code, station_name, latitude, longitude.
//
// Field opsional menggunakan pointer agar bisa membedakan:
//   - field tidak dikirim  -> nil, artinya "jangan ubah" (untuk update)
//   - field dikirim null   -> pointer ke nil? tidak mungkin di Go; jadi
//     pada praktiknya, field opsional di sini dianggap "tidak diisi"
//     ketika tidak dikirim, dan "diisi" ketika nilainya ada.
//
// Pemisahan ini penting supaya update parsial aman.
type StationInput struct {
	StationCode  string  `json:"station_code" binding:"required,max=100"`
	StationName  string  `json:"station_name" binding:"required,max=255"`
	Latitude     float64 `json:"latitude" binding:"required"`
	Longitude    float64 `json:"longitude" binding:"required"`
	Region       *string `json:"region"`
	DataSource   *string `json:"data_source"`
	StatusActive *bool   `json:"status_active"`
}

// toWrite mengonversi payload input menjadi struktur yang dipakai repository.
// Actor dipisah dari input (bukan berasal dari body) agar user tidak bisa
// memalsukan created_by/updated_by — nilainya selalu berasal dari JWT.
func (in StationInput) toWrite(actor *models.UUIDBinary) repositories.StationWrite {
	return repositories.StationWrite{
		StationCode:  in.StationCode,
		StationName:  in.StationName,
		Latitude:     in.Latitude,
		Longitude:    in.Longitude,
		Region:       in.Region,
		DataSource:   in.DataSource,
		StatusActive: in.StatusActive,
		ActorID:      actor,
	}
}

// CreateStation membuat station baru.
//
// Endpoint: POST /api/stations (butuh izin "manage-stations").
//
// Jika station_code sudah dipakai (unique constraint di DB), balas 409
// Conflict — bukan 500 — karena ini kesalahan input user, bukan bug server.
// Respons sukses memakai 201 Created sesuai semantik HTTP.
func CreateStation() gin.HandlerFunc {
	return func(c *gin.Context) {
		var input StationInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "Input tidak valid", "error": err.Error()})
			return
		}

		station, err := repositories.NewStationRepository(config.SQLDB).
			Create(c.Request.Context(), input.toWrite(actorID(c)))
		if repositories.IsDuplicateEntry(err) {
			c.JSON(http.StatusConflict, gin.H{"message": "Kode station sudah dipakai"})
			return
		}
		if err != nil {
			config.Log.Error("Gagal membuat station", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal membuat station"})
			return
		}
		c.JSON(http.StatusCreated, station)
	}
}

// UpdateStation mengubah data station berdasarkan ID di URL.
//
// Alur:
//  1. Validasi format UUID di path.
//  2. Bind body ke StationInput.
//  3. Cek dulu apakah station ada (SELECT ringan). Ini perlu karena UPDATE
//     di MySQL tidak error saat ID tidak ditemukan — hanya 0 row affected.
//     Dengan cek awal, kita bisa membalas 404 dengan jelas, bukan 200 palsu.
//  4. Jalankan update. Duplikasi station_code dibalas 409.
//  5. Baca ulang station untuk mengembalikan data terbaru ke frontend
//     (termasuk kolom yang mungkin dihitung/diisi default oleh DB).
func UpdateStation() gin.HandlerFunc {
	return func(c *gin.Context) {
		parsedID, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "ID tidak valid"})
			return
		}
		id := models.UUIDBinary(parsedID)

		var input StationInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "Input tidak valid", "error": err.Error()})
			return
		}

		ctx := c.Request.Context()
		repo := repositories.NewStationRepository(config.SQLDB)

		if _, err := repo.Get(ctx, id); errors.Is(err, repositories.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"message": "Station tidak ditemukan"})
			return
		} else if err != nil {
			config.Log.Error("Gagal membaca station", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal update station"})
			return
		}

		if err := repo.Update(ctx, id, input.toWrite(actorID(c))); err != nil {
			if repositories.IsDuplicateEntry(err) {
				c.JSON(http.StatusConflict, gin.H{"message": "Kode station sudah dipakai"})
				return
			}
			config.Log.Error("Gagal update station", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal update station"})
			return
		}

		// Baca ulang untuk respons yang akurat.
		station, err := repo.Get(ctx, id)
		if err != nil {
			config.Log.Error("Gagal membaca station setelah update", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal update station"})
			return
		}
		c.JSON(http.StatusOK, station)
	}
}

// DeleteStation menghapus station berdasarkan ID di URL.
//
// Penanganan khusus:
//   - Jika station masih direferensikan oleh weather_predictions (foreign
//     key constraint), balas 409 Conflict dengan pesan yang menjelaskan
//     kenapa penghapusan ditolak. Ini jauh lebih informatif daripada 500.
//   - Jika tidak ada baris yang terhapus, balas 404 — bukan 200 "berhasil",
//     karena user seharusnya tahu bahwa station yang dimaksud tidak ada.
func DeleteStation() gin.HandlerFunc {
	return func(c *gin.Context) {
		parsedID, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "ID tidak valid"})
			return
		}

		deleted, err := repositories.NewStationRepository(config.SQLDB).
			Delete(c.Request.Context(), models.UUIDBinary(parsedID))
		if repositories.IsForeignKeyViolation(err) {
			c.JSON(http.StatusConflict, gin.H{"message": "Station tidak bisa dihapus karena masih dipakai weather_predictions"})
			return
		}
		if err != nil {
			config.Log.Error("Gagal hapus station", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal hapus station"})
			return
		}
		if !deleted {
			c.JSON(http.StatusNotFound, gin.H{"message": "Station tidak ditemukan"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Station berhasil dihapus"})
	}
}

// =============================================================================
// GEOCODING (layanan gratis: Nominatim / OpenStreetMap)
// =============================================================================

// geocodeHTTPClient adalah HTTP client bersama untuk memanggil Nominatim.
//
// Kenapa pakai client khusus?
//   - Timeout: Nominatim bisa lambat; tanpa timeout, request user bisa
//     menggantung lama dan menahan koneksi.
//   - TLS Renegotiation: beberapa environment/proxy memerlukan renegosiasi
//     TLS bebas; tanpa opsi ini kadang request gagal dengan error handshake.
//
// Catatan: client ini dipakai bersama oleh GeocodeSearch dan ReverseGeocode,
// jadi perubahan konfigurasi otomatis berlaku untuk keduanya.
var geocodeHTTPClient = &http.Client{
	Timeout: 8 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			Renegotiation: tls.RenegotiateFreelyAsClient,
		},
	},
}

// NominatimAddress adalah subset field `address` dari respons Nominatim
// yang kita pakai untuk menyarankan nama station.
// Field yang tersedia di Nominatim sangat banyak; kita hanya mengambil
// yang relevan agar struct tetap ringkas dan mudah dirawat.
type NominatimAddress struct {
	Suburb       string `json:"suburb"`
	CityDistrict string `json:"city_district"`
	Village      string `json:"village"`
	Town         string `json:"town"`
	Municipality string `json:"municipality"`
}

// NominatimResult adalah satu hasil dari Nominatim.
// Lat/Lon dikirim sebagai string oleh Nominatim, jadi kita simpan sebagai
// string lalu parse manual saat dibutuhkan.
type NominatimResult struct {
	DisplayName string           `json:"display_name"`
	Lat         string           `json:"lat"`
	Lon         string           `json:"lon"`
	Address     NominatimAddress `json:"address"`
}

// pickSuggestedStationName memilih nama yang paling masuk akal untuk
// dijadikan nama station, berdasarkan urutan prioritas:
// suburb -> city_district -> village -> town -> municipality.
//
// Urutan ini dipilih karena untuk wilayah Indonesia (khususnya Batam),
// "suburb" biasanya merepresentasikan kelurahan/perumahan yang lebih
// spesifik dibanding "city_district" atau "municipality" yang lebih luas.
// Jika semuanya kosong, kembalikan string kosong agar frontend tahu
// bahwa tidak ada saran nama (bukan mengira namanya benar-benar kosong).
func pickSuggestedStationName(addr NominatimAddress) string {
	switch {
	case addr.Suburb != "":
		return addr.Suburb
	case addr.CityDistrict != "":
		return addr.CityDistrict
	case addr.Village != "":
		return addr.Village
	case addr.Town != "":
		return addr.Town
	case addr.Municipality != "":
		return addr.Municipality
	default:
		return ""
	}
}

// GeocodeSuggestion adalah satu item hasil pencarian lokasi yang
// dikembalikan ke frontend untuk ditampilkan sebagai pilihan.
type GeocodeSuggestion struct {
	Label     string  `json:"label"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// GeocodeSearch mencari lokasi berdasarkan kata kunci (parameter ?q=).
//
// Endpoint: GET /api/stations/geocode?q=Sekupang, Batam
//
// Alur:
//  1. Validasi parameter `q`.
//  2. Bangun URL Nominatim dengan query yang di-encode (url.Values).
//  3. Kirim request dengan User-Agent khusus — Nominatim memblokir
//     request tanpa User-Agent yang jelas (403 Forbidden).
//  4. Periksa status code; jika bukan 200, balas 502 karena masalahnya
//     di layanan upstream, bukan di aplikasi kita.
//  5. Decode JSON, konversi lat/lon dari string ke float64.
//     Hasil yang gagal di-parse dilewati (bukan menggagalkan request),
//     supaya user tetap dapat saran lain yang valid.
//  6. Kembalikan daftar saran ke frontend.
func GeocodeSearch() gin.HandlerFunc {
	return func(c *gin.Context) {
		query := c.Query("q")
		if query == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "Parameter q wajib diisi"})
			return
		}

		endpoint := "https://nominatim.openstreetmap.org/search?" + url.Values{
			"q":              {query},
			"format":         {"json"},
			"limit":          {"5"},
			"countrycodes":   {"id"}, // Batasi ke Indonesia.
			"addressdetails": {"0"},
		}.Encode()

		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal membuat request geocoding"})
			return
		}

		// User-Agent khusus agar tidak diblokir Nominatim.
		req.Header.Set("User-Agent", "WeatherBackendBatamApp/1.0 (maulana.dev@proton.me)")

		resp, err := geocodeHTTPClient.Do(req)
		if err != nil {
			log.Println("[GeocodeSearch] Gagal request ke Nominatim:", err)
			c.JSON(http.StatusBadGateway, gin.H{"message": "Gagal menghubungi layanan geocoding", "error": err.Error()})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Println("[GeocodeSearch] Nominatim balas status non-200:", resp.StatusCode)
			c.JSON(http.StatusBadGateway, gin.H{"message": "Layanan geocoding membalas status tidak normal", "nominatim_status": resp.StatusCode})
			return
		}

		var results []NominatimResult
		if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"message": "Gagal membaca hasil geocoding"})
			return
		}

		// Konversi ke bentuk yang ringkas untuk frontend.
		suggestions := make([]GeocodeSuggestion, 0, len(results))
		for _, r := range results {
			lat, errLat := strconv.ParseFloat(r.Lat, 64)
			lon, errLon := strconv.ParseFloat(r.Lon, 64)
			if errLat != nil || errLon != nil {
				// Lewati hasil yang koordinatnya tidak valid; lebih baik
				// daripada menggagalkan seluruh request hanya karena 1 baris.
				continue
			}
			suggestions = append(suggestions, GeocodeSuggestion{
				Label:     r.DisplayName,
				Latitude:  lat,
				Longitude: lon,
			})
		}

		c.JSON(http.StatusOK, suggestions)
	}
}

// ReverseGeocode mencari alamat berdasarkan koordinat (?lat=..&lon=..).
//
// Endpoint: GET /api/stations/reverse-geocode?lat=1.13&lon=104.05
//
// Berbeda dengan GeocodeSearch, endpoint ini hanya mengembalikan SATU hasil
// (lokasi paling relevan) plus nama yang disarankan untuk station.
// Frontend biasanya memakainya saat user mengklik peta untuk mengisi form
// station secara otomatis.
//
// addressdetails=1 dipakai agar Nominatim menyertakan objek `address`
// yang dibutuhkan pickSuggestedStationName().
func ReverseGeocode() gin.HandlerFunc {
	return func(c *gin.Context) {
		lat := c.Query("lat")
		lon := c.Query("lon")
		if lat == "" || lon == "" {
			c.JSON(http.StatusBadRequest, gin.H{"message": "Parameter lat dan lon wajib diisi"})
			return
		}

		endpoint := "https://nominatim.openstreetmap.org/reverse?" + url.Values{
			"lat":            {lat},
			"lon":            {lon},
			"format":         {"json"},
			"addressdetails": {"1"},
		}.Encode()

		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal membuat request reverse geocoding"})
			return
		}

		// User-Agent khusus agar tidak diblokir Nominatim.
		req.Header.Set("User-Agent", "WeatherBackendBatamApp/1.0 (maulana.dev@proton.me)")

		resp, err := geocodeHTTPClient.Do(req)
		if err != nil {
			log.Println("[ReverseGeocode] Gagal request ke Nominatim:", err)
			c.JSON(http.StatusBadGateway, gin.H{"message": "Gagal menghubungi layanan geocoding", "error": err.Error()})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Println("[ReverseGeocode] Nominatim balas status non-200:", resp.StatusCode)
			c.JSON(http.StatusBadGateway, gin.H{"message": "Layanan geocoding membalas status tidak normal", "nominatim_status": resp.StatusCode})
			return
		}

		var result NominatimResult
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"message": "Gagal membaca hasil reverse geocoding"})
			return
		}

		// display_name: alamat lengkap dari Nominatim.
		// suggested_name: nama pendek yang disarankan untuk field "station_name".
		c.JSON(http.StatusOK, gin.H{
			"display_name":   result.DisplayName,
			"suggested_name": pickSuggestedStationName(result.Address),
		})
	}
}