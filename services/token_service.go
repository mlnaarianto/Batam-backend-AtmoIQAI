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
	AccessTokenTTL  = 15 * time.Minute
	RefreshTokenTTL = 8 * time.Hour
)

// RoleOf: "admin" jika punya role admin, selain itu "user" (sama seperti logika lama).
func RoleOf(roles []string) string {
	for _, r := range roles {
		if r == "admin" {
			return "admin"
		}
	}
	return "user"
}

func NewAccessToken(u *repositories.AuthUser) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(AccessTokenTTL)
	claims := jwt.MapClaims{
		"user_id":  uuid.UUID(u.ID).String(),
		"username": u.Username,
		"role":     RoleOf(u.Roles),
		"jti":      uuid.NewString(),
		"iat":      now.Unix(),
		"exp":      exp.Unix(),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(middlewares.JwtKey)
	return signed, exp, err
}

// NewRefreshToken mengembalikan nilai mentah (untuk cookie) dan hash-nya (untuk database).
func NewRefreshToken() (raw string, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, HashToken(raw), nil
}

func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}