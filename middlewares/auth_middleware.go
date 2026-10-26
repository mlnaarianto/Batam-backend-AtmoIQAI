package middlewares

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"weather-backend/config"
	"weather-backend/models"
	"weather-backend/repositories"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var JwtKey []byte

// InitJWT dipanggil di main() SETELAH godotenv.Load().
func InitJWT() error {
	secret := os.Getenv("JWT_SECRET")
	if len(secret) < 32 {
		return errors.New("JWT_SECRET kosong atau kurang dari 32 karakter")
	}
	JwtKey = []byte(secret)
	return nil
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token tidak ditemukan, silakan login dulu"})
			c.Abort()
			return
		}

		tokenString := strings.Split(authHeader, "Bearer ")
		if len(tokenString) != 2 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Format token salah"})
			c.Abort()
			return
		}

		claims := jwt.MapClaims{}
		token, err := jwt.ParseWithClaims(
			tokenString[1],
			claims,
			func(token *jwt.Token) (interface{}, error) {
				return JwtKey, nil
			},
			jwt.WithValidMethods([]string{"HS256"}), // tolak algoritma selain HS256
		)

		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token tidak valid atau kedaluwarsa"})
			c.Abort()
			return
		}

		// Set data dari JWT claims ke dalam Gin Context
		c.Set("user_id", claims["user_id"])
		c.Set("username", claims["username"])
		c.Set("role", claims["role"])

		c.Next()
	}
}

// RequirePermission memeriksa permission langsung ke database (SQL murni),
// memakai user_id dari JWT. Bypass admin ada di dalam query HasPermission.
func RequirePermission(requiredPermission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userIDVal, exists := c.Get("user_id")
		if !exists || userIDVal == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		parsed, err := uuid.Parse(fmt.Sprint(userIDVal))
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		allowed, err := repositories.NewPermissionRepository(config.SQLDB).
			HasPermission(c.Request.Context(), models.UUIDBinary(parsed), requiredPermission)
		if err != nil {
			// Ganti dengan config.Log (Zap) kalau mau.
			log.Printf("RequirePermission: gagal cek permission %q untuk user %s: %v", requiredPermission, parsed, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Terjadi kesalahan server"})
			c.Abort()
			return
		}

		if !allowed {
			c.JSON(http.StatusForbidden, gin.H{"error": "Akses ditolak! Anda tidak memiliki izin."})
			c.Abort()
			return
		}

		c.Next()
	}
}