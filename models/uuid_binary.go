package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
)

// UUIDBinary adalah tipe kustom agar uuid.UUID bisa klop dengan kolom BINARY(16) MySQL
type UUIDBinary uuid.UUID

// --- TAMBAHAN: bikin UUIDBinary di-encode/decode sebagai string UUID biasa di JSON ---

func (u UUIDBinary) MarshalJSON() ([]byte, error) {
	return json.Marshal(uuid.UUID(u).String())
}

func (u *UUIDBinary) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
		*u = UUIDBinary(uuid.Nil)
		return nil
	}
	parsed, err := uuid.Parse(s)
	if err != nil {
		return err
	}
	*u = UUIDBinary(parsed)
	return nil
}

// helperScan membaca data binary 16-byte dari database MySQL dan mengubahnya kembali menjadi UUID Go
func helperScan(u *UUIDBinary, value interface{}) error {
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return errors.New("gagal scan UUIDBinary: tipe data tidak dikenali")
	}

	parsedUUID, err := uuid.FromBytes(bytes)
	if err != nil {
		return err
	}
	*u = UUIDBinary(parsedUUID)
	return nil
}

// Scan mengimplementasikan sql.Scanner
func (u *UUIDBinary) Scan(value interface{}) error {
	return helperScan(u, value)
}

// Value mengimplementasikan driver.Valuer untuk mengubah UUID Go menjadi 16-byte biner mentah
func (u UUIDBinary) Value() (driver.Value, error) {
	parsedUUID := uuid.UUID(u)
	if parsedUUID == uuid.Nil {
		return nil, nil
	}
	return parsedUUID.MarshalBinary()
}