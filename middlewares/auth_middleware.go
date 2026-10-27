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

// InitJWT wajib dipanggil dari main() setelah godotenv.Load(),
// karena secret diambil dari environment variable JWT_SECRET.
func InitJWT() error {
	secret := os.Getenv("JWT_SECRET")
	if len(secret) < 32 {
		return errors.New("JWT_SECRET kosong atau kurang dari 32 karakter")
	}
	JwtKey = []byte(secret)
	return nil
}

// AuthMiddleware memvalidasi access token di header Authorization
// dan menyimpan data user ke Gin Context agar bisa dipakai handler berikutnya.
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token tidak ditemukan, silakan login dulu"})
			c.Abort()
			return
		}

		// Header harus berformat "Bearer <token>".
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
			// Batasi algoritma yang diterima agar tidak bisa di-bypass (mis. alg=none).
			jwt.WithValidMethods([]string{"HS256"}),
		)

		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token tidak valid atau kedaluwarsa"})
			c.Abort()
			return
		}

		// Simpan data penting dari claims ke context agar handler bisa memakainya.
		c.Set("user_id", claims["user_id"])
		c.Set("username", claims["username"])
		c.Set("role", claims["role"])

		c.Next()
	}
}

// RequirePermission memeriksa apakah user (diambil dari JWT) memiliki permission
// tertentu. Pengecekan dilakukan langsung ke database via query SQL murni.
// Bypass untuk admin sudah ditangani di dalam HasPermission.
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
			// Bisa diganti config.Log (Zap) jika ingin konsisten dengan logger utama.
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

// RequirePasswordChanged memblokir request (403) selama user masih
// harus mengganti password (must_change_password = 1).
//
// Harus dipasang SETELAH AuthMiddleware, karena butuh user_id di
// context. Route yang boleh diakses walau password belum diganti
// (/me, /profile/password, /logout) sengaja TIDAK dipasangi middleware
// ini (lihat routes/api.go).
//
// Flag dibaca dari database pada setiap request, bukan dari JWT, agar
// reset oleh admin langsung berlaku tanpa menunggu access token habis.
//
// Fail-closed: jika pengecekan gagal (error DB), request ditolak 500,
// bukan dibiarkan lewat.
//
// Response 403 membawa "code": "must_change_password" supaya frontend
// bisa membedakannya dari 403 biasa (tidak punya izin) dan mengarahkan
// user ke halaman ganti password.
func RequirePasswordChanged() gin.HandlerFunc {
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

		must, err := repositories.NewAuthRepository(config.SQLDB).
			MustChangePassword(c.Request.Context(), models.UUIDBinary(parsed))
		if errors.Is(err, repositories.ErrNotFound) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User tidak ditemukan"})
			c.Abort()
			return
		}
		if err != nil {
			log.Printf("RequirePasswordChanged: gagal cek must_change_password user %s: %v", parsed, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Terjadi kesalahan server"})
			c.Abort()
			return
		}

		if must {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "Anda wajib mengganti password terlebih dahulu",
				"code":  "must_change_password",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
