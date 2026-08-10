package controllers

import (
	"crypto/tls"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"weather-backend/config"
	"weather-backend/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// GET /api/admin/stations
func GetStations() gin.HandlerFunc {
	return func(c *gin.Context) {
		var stations []models.Station
		if err := config.DB.Find(&stations).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal mengambil data stations"})
			return
		}
		c.JSON(http.StatusOK, stations)
	}
}

// GET /api/admin/stations/:id
func GetStationByID() gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		parsedID, err := uuid.Parse(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "ID tidak valid"})
			return
		}

		var station models.Station
		if err := config.DB.Where("id = ?", models.UUIDBinary(parsedID)).First(&station).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"message": "Station tidak ditemukan"})
			return
		}
		c.JSON(http.StatusOK, station)
	}
}

type StationInput struct {
	StationCode  string  `json:"station_code" binding:"required"`
	StationName  string  `json:"station_name" binding:"required"`
	Latitude     float64 `json:"latitude" binding:"required"`
	Longitude    float64 `json:"longitude" binding:"required"`
	Region       *string `json:"region"`
	DataSource   *string `json:"data_source"`
	StatusActive *bool   `json:"status_active"`
}

// POST /api/admin/stations
func CreateStation() gin.HandlerFunc {
	return func(c *gin.Context) {
		var input StationInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "Input tidak valid", "error": err.Error()})
			return
		}

		userIDRaw, _ := c.Get("user_id")
		userID, ok := userIDRaw.(models.UUIDBinary)

		statusActive := true
		if input.StatusActive != nil {
			statusActive = *input.StatusActive
		}

		station := models.Station{
			StationCode:  input.StationCode,
			StationName:  input.StationName,
			Latitude:     input.Latitude,
			Longitude:    input.Longitude,
			Region:       input.Region,
			DataSource:   input.DataSource,
			StatusActive: statusActive,
		}
		if ok {
			station.CreatedBy = &userID
			station.UpdatedBy = &userID
		}

		if err := config.DB.Create(&station).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal membuat station", "error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, station)
	}
}

// PUT /api/admin/stations/:id
func UpdateStation() gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		parsedID, err := uuid.Parse(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "ID tidak valid"})
			return
		}

		var station models.Station
		if err := config.DB.Where("id = ?", models.UUIDBinary(parsedID)).First(&station).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"message": "Station tidak ditemukan"})
			return
		}

		var input StationInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "Input tidak valid", "error": err.Error()})
			return
		}

		userIDRaw, _ := c.Get("user_id")
		userID, ok := userIDRaw.(models.UUIDBinary)

		station.StationCode = input.StationCode
		station.StationName = input.StationName
		station.Latitude = input.Latitude
		station.Longitude = input.Longitude
		station.Region = input.Region
		station.DataSource = input.DataSource
		if input.StatusActive != nil {
			station.StatusActive = *input.StatusActive
		}
		if ok {
			station.UpdatedBy = &userID
		}

		if err := config.DB.Save(&station).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal update station", "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, station)
	}
}

// DELETE /api/admin/stations/:id
func DeleteStation() gin.HandlerFunc {
	return func(c *gin.Context) {
		idStr := c.Param("id")
		parsedID, err := uuid.Parse(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "ID tidak valid"})
			return
		}

		result := config.DB.Where("id = ?", models.UUIDBinary(parsedID)).Delete(&models.Station{})
		if result.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": "Gagal hapus station (kemungkinan masih dipakai weather_predictions)", "error": result.Error.Error()})
			return
		}
		if result.RowsAffected == 0 {
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