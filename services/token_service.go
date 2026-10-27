package services

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"time"

	"weather-backend/middlewares"
	"weather-backend/repositories"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	// AccessTokenTTL: access token berumur pendek agar lebih aman jika bocor.
	AccessTokenTTL = 15 * time.Minute
	// RefreshTokenTTL: refresh token lebih panjang, dipakai untuk minta access token baru.
	RefreshTokenTTL = 8 * time.Hour
)

// RoleOf menentukan role utama user dari daftar roles.
// Jika ada role "admin", kembalikan "admin"; selain itu "user".
func RoleOf(roles []string) string {
	for _, r := range roles {
		if r == "admin" {
			return "admin"
		}
	}
	return "user"
}

// NewAccessToken membuat access token JWT yang sudah ditandatangani (HS256).
// Claims berisi user_id, username, role, jti (ID unik token), iat, dan exp.
// Mengembalikan token string, waktu kedaluwarsa, dan error jika signing gagal.
func NewAccessToken(u *repositories.AuthUser) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(AccessTokenTTL)
	claims := jwt.MapClaims{
		"user_id":  uuid.UUID(u.ID).String(),
		"username": u.Username,
		"role":     RoleOf(u.Roles),
		"jti":      uuid.NewString(), // ID unik token, berguna untuk revoke/audit.
		"iat":      now.Unix(),
		"exp":      exp.Unix(),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(middlewares.JwtKey)
	return signed, exp, err
}

// NewRefreshToken membuat refresh token acak 32 byte.
// Mengembalikan dua nilai:
//   - raw: token mentah untuk disimpan di cookie (hanya dikirim ke client).
//   - hash: SHA256 dari raw untuk disimpan di database.
// Dengan begitu, jika database bocor, token mentah tidak bisa dipakai langsung.
func NewRefreshToken() (raw string, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, HashToken(raw), nil
}

// HashToken mengembalikan hash SHA256 dari token mentah dalam bentuk hex.
// Dipakai untuk mencocokkan refresh token yang dikirim client dengan hash di database.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}