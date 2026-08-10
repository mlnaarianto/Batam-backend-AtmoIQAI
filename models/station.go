package models

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Station struct {
	ID           UUIDBinary  `json:"id" gorm:"type:binary(16);primaryKey;not null"`
	StationCode  string      `json:"station_code" gorm:"unique;not null"`
	StationName  string      `json:"station_name" gorm:"not null"`
	Latitude     float64     `json:"latitude" gorm:"type:decimal(9,6);not null"`
	Longitude    float64     `json:"longitude" gorm:"type:decimal(9,6);not null"`
	Region       *string     `json:"region"`
	DataSource   *string     `json:"data_source"`
	StatusActive bool        `json:"status_active" gorm:"not null;default:1"`
	CreatedBy    *UUIDBinary `json:"created_by" gorm:"type:binary(16)"`
	UpdatedBy    *UUIDBinary `json:"updated_by" gorm:"type:binary(16)"`

	// Relasi (opsional, untuk preload data user pembuat/pengubah)
	Creator *User `json:"creator,omitempty" gorm:"foreignKey:CreatedBy;references:ID"`
	Updater *User `json:"updater,omitempty" gorm:"foreignKey:UpdatedBy;references:ID"`
}

// Hook GORM: Otomatis generate UUID v4 baru jika ID masih kosong
func (s *Station) BeforeCreate(tx *gorm.DB) (err error) {
	if uuid.UUID(s.ID) == uuid.Nil {
		s.ID = UUIDBinary(uuid.New())
	}
	return
}