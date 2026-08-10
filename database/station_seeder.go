package database

import (
	"fmt"
	"weather-backend/config"
	"weather-backend/models"
)

func SeedStations() {
	// Ambil user superadmin untuk dijadikan created_by/updated_by
	var adminUser models.User
	if err := config.DB.Where("username = ?", "superadmin").First(&adminUser).Error; err != nil {
		fmt.Println("Gagal menemukan user superadmin untuk seeding stations:", err)
		return
	}

	regionBatamKota := "Batam Kota"
	regionLubukBaja := "Lubuk Baja"
	regionSekupang := "Sekupang"
	regionBatuAmpar := "Batu Ampar"
	regionSagulung := "Sagulung"
	regionBatuAji := "Batu Aji"
	regionBengkong := "Bengkong"
	regionNongsa := "Nongsa"
	regionSeiBeduk := "Sei Beduk"
	regionBelakangPadang := "Belakang Padang"
	regionBulang := "Bulang"
	regionGalang := "Galang"
	source := "BMKG API"

	stations := []models.Station{
		{
			StationCode:  "BTM-001",
			StationName:  "Kecamatan Batam Kota",
			Latitude:     1.130100,
			Longitude:    104.052900,
			Region:       &regionBatamKota,
			DataSource:   &source,
			StatusActive: true,
			CreatedBy:    &adminUser.ID,
			UpdatedBy:    &adminUser.ID,
		},
		{
			StationCode:  "BTM-002",
			StationName:  "Kecamatan Lubuk Baja",
			Latitude:     1.119800,
			Longitude:    104.034700,
			Region:       &regionLubukBaja,
			DataSource:   &source,
			StatusActive: true,
			CreatedBy:    &adminUser.ID,
			UpdatedBy:    &adminUser.ID,
		},
		{
			StationCode:  "BTM-003",
			StationName:  "Kecamatan Sekupang",
			Latitude:     1.100000,
			Longitude:    103.983300,
			Region:       &regionSekupang,
			DataSource:   &source,
			StatusActive: true,
			CreatedBy:    &adminUser.ID,
			UpdatedBy:    &adminUser.ID,
		},
		{
			StationCode:  "BTM-004",
			StationName:  "Kecamatan Batu Ampar",
			Latitude:     1.161670,
			Longitude:    104.004720,
			Region:       &regionBatuAmpar,
			DataSource:   &source,
			StatusActive: true,
			CreatedBy:    &adminUser.ID,
			UpdatedBy:    &adminUser.ID,
		},
		{
			StationCode:  "BTM-005",
			StationName:  "Kecamatan Sagulung",
			Latitude:     1.036698,
			Longitude:    103.949203,
			Region:       &regionSagulung,
			DataSource:   &source,
			StatusActive: true,
			CreatedBy:    &adminUser.ID,
			UpdatedBy:    &adminUser.ID,
		},
		{
			StationCode:  "BTM-006",
			StationName:  "Kecamatan Batu Aji",
			Latitude:     1.039700,
			Longitude:    103.980000,
			Region:       &regionBatuAji,
			DataSource:   &source,
			StatusActive: true,
			CreatedBy:    &adminUser.ID,
			UpdatedBy:    &adminUser.ID,
		},
		{
			StationCode:  "BTM-007",
			StationName:  "Kecamatan Bengkong",
			Latitude:     1.108400,
			Longitude:    104.047200,
			Region:       &regionBengkong,
			DataSource:   &source,
			StatusActive: true,
			CreatedBy:    &adminUser.ID,
			UpdatedBy:    &adminUser.ID,
		},
		{
			StationCode:  "BTM-008",
			StationName:  "Kecamatan Nongsa",
			Latitude:     1.189200,
			Longitude:    104.133900,
			Region:       &regionNongsa,
			DataSource:   &source,
			StatusActive: true,
			CreatedBy:    &adminUser.ID,
			UpdatedBy:    &adminUser.ID,
		},
		{
			StationCode:  "BTM-009",
			StationName:  "Kecamatan Sei Beduk",
			Latitude:     1.092900,
			Longitude:    104.024700,
			Region:       &regionSeiBeduk,
			DataSource:   &source,
			StatusActive: true,
			CreatedBy:    &adminUser.ID,
			UpdatedBy:    &adminUser.ID,
		},
		{
			StationCode:  "BTM-010",
			StationName:  "Kecamatan Belakang Padang",
			Latitude:     1.166700,
			Longitude:    103.900000,
			Region:       &regionBelakangPadang,
			DataSource:   &source,
			StatusActive: true,
			CreatedBy:    &adminUser.ID,
			UpdatedBy:    &adminUser.ID,
		},
		{
			StationCode:  "BTM-011",
			StationName:  "Kecamatan Bulang",
			Latitude:     0.933300,
			Longitude:    104.133300,
			Region:       &regionBulang,
			DataSource:   &source,
			StatusActive: true,
			CreatedBy:    &adminUser.ID,
			UpdatedBy:    &adminUser.ID,
		},
		{
			StationCode:  "BTM-012",
			StationName:  "Kecamatan Galang",
			Latitude:     0.883300,
			Longitude:    104.133300,
			Region:       &regionGalang,
			DataSource:   &source,
			StatusActive: true,
			CreatedBy:    &adminUser.ID,
			UpdatedBy:    &adminUser.ID,
		},
	}

	for _, station := range stations {
		var existing models.Station
		err := config.DB.Where("station_code = ?", station.StationCode).First(&existing).Error
		if err != nil {
			if errCreate := config.DB.Create(&station).Error; errCreate != nil {
				fmt.Println("Gagal membuat station", station.StationCode, ":", errCreate)
			}
		}
	}

	fmt.Println("Seeder Stations berhasil dijalankan!")
}