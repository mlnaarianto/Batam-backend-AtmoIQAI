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

// actorID membaca user_id dari JWT (disimpan middleware sebagai string).
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

// GET /api/admin/stations  dan  /api/public/stations
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

// GET /api/admin/stations/:id
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

type StationInput struct {
	StationCode  string  `json:"station_code" binding:"required,max=100"`
	StationName  string  `json:"station_name" binding:"required,max=255"`
	Latitude     float64 `json:"latitude" binding:"required"`
	Longitude    float64 `json:"longitude" binding:"required"`
	Region       *string `json:"region"`
	DataSource   *string `json:"data_source"`
	StatusActive *bool   `json:"status_active"`
}

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

// POST /api/admin/stations
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

// PUT /api/admin/stations/:id
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

		station, err := repo.Get(ctx, id)
		if err != nil {
			config.Log.Error("Gagal membaca station setelah update", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal update station"})
			return
		}
		c.JSON(http.StatusOK, station)
	}
}

// DELETE /api/admin/stations/:id
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

// ================== GEOCODING (gratis, pakai Nominatim/OpenStreetMap) ==================
var geocodeHTTPClient = &http.Client{
	Timeout: 8 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			Renegotiation: tls.RenegotiateFreelyAsClient,
		},
	},
}

type NominatimAddress struct {
	Suburb       string `json:"suburb"`
	CityDistrict string `json:"city_district"`
	Village      string `json:"village"`
	Town         string `json:"town"`
	Municipality string `json:"municipality"`
}

type NominatimResult struct {
	DisplayName string           `json:"display_name"`
	Lat         string           `json:"lat"`
	Lon         string           `json:"lon"`
	Address     NominatimAddress `json:"address"`
}

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

type GeocodeSuggestion struct {
	Label     string  `json:"label"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// GET /api/admin/stations/geocode?q=Sekupang, Batam
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
			"countrycodes":   {"id"},
			"addressdetails": {"0"},
		}.Encode()

		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal membuat request geocoding"})
			return
		}

		// Menggunakan User-Agent khusus yang valid agar tidak kena blokir 403 Forbidden
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

		suggestions := make([]GeocodeSuggestion, 0, len(results))
		for _, r := range results {
			lat, errLat := strconv.ParseFloat(r.Lat, 64)
			lon, errLon := strconv.ParseFloat(r.Lon, 64)
			if errLat != nil || errLon != nil {
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

// GET /api/admin/stations/reverse-geocode?lat=1.13&lon=104.05
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

		// Menggunakan User-Agent khusus yang valid agar tidak kena blokir 403 Forbidden
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

		c.JSON(http.StatusOK, gin.H{
			"display_name":   result.DisplayName,
			"suggested_name": pickSuggestedStationName(result.Address),
		})
	}
}