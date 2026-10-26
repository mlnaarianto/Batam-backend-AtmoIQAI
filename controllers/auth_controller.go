package controllers

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"weather-backend/config"
	"weather-backend/helpers"
	"weather-backend/models"
	"weather-backend/repositories"
	"weather-backend/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

const refreshCookieName = "refresh_token"

type LoginInput struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func setRefreshCookie(c *gin.Context, value string, maxAge int) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(refreshCookieName, value, maxAge, "/", "", os.Getenv("COOKIE_SECURE") == "true", true)
}

func clearRefreshCookie(c *gin.Context) { setRefreshCookie(c, "", -1) }

func newRefreshRecord(c *gin.Context, userID, familyID models.UUIDBinary, hash string) *repositories.RefreshToken {
	return &repositories.RefreshToken{
		ID:        models.NewUUIDBinary(),
		UserID:    userID,
		FamilyID:  familyID,
		TokenHash: hash,
		UserAgent: c.Request.UserAgent(),
		IPAddress: c.ClientIP(),
		ExpiredAt: time.Now().Add(services.RefreshTokenTTL).UTC(),
	}
}

func respondSession(c *gin.Context, u *repositories.AuthUser, accessToken string, exp time.Time, message string) {
	// TAMBAHAN: Ambil photo_url, jadikan pointer agar bisa mengirim `null` ke JSON jika kosong
	var photoURL *string
	if u.PhotoPath.Valid {
		photoURL = &u.PhotoPath.String
	}

	helpers.ResponseOK(c, gin.H{
		"id":         uuid.UUID(u.ID).String(),
		"message":    message,
		"token":      accessToken,
		"expires_at": exp.Unix(),
		"username":   u.Username,
		"name":       u.Name,
		"role":       services.RoleOf(u.Roles),
		"photo_url":  photoURL, // <-- TAMBAHAN: Dikirim ke frontend
	})
}

func Login(c *gin.Context) {
	var input LoginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		config.Log.Warn("Gagal bind JSON saat proses login", zap.Error(err))
		helpers.ResponseBadRequest(c, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	users := repositories.NewUserRepository(config.SQLDB)
	tokens := repositories.NewTokenRepository(config.SQLDB)

	user, err := users.FindByUsernameOrEmail(ctx, input.Username)
	if errors.Is(err, repositories.ErrNotFound) {
		config.Log.Warn("Login gagal: user tidak ditemukan", zap.String("input_username", input.Username))
		helpers.ResponseUnauthorized(c, gin.H{"error": "Username atau password salah!"})
		return
	}
	if err != nil {
		config.Log.Error("Gagal query user saat login", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Terjadi kesalahan server"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(input.Password)); err != nil {
		config.Log.Warn("Login gagal: password salah", zap.String("username", user.Username))
		helpers.ResponseUnauthorized(c, gin.H{"error": "Username atau password salah!"})
		return
	}

	accessToken, exp, err := services.NewAccessToken(user)
	if err != nil {
		config.Log.Error("Gagal membuat access token", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal membuat token"})
		return
	}

	rawRefresh, hash, err := services.NewRefreshToken()
	if err != nil {
		config.Log.Error("Gagal membuat refresh token", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal membuat token"})
		return
	}

	familyID := models.NewUUIDBinary() // satu login = satu family baru
	if err := tokens.Create(ctx, newRefreshRecord(c, user.ID, familyID, hash)); err != nil {
		config.Log.Error("Gagal menyimpan refresh token", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal menyimpan sesi login"})
		return
	}

	setRefreshCookie(c, rawRefresh, int(services.RefreshTokenTTL.Seconds()))
	config.Log.Info("Login berhasil", zap.String("username", user.Username), zap.String("role", services.RoleOf(user.Roles)))
	respondSession(c, user, accessToken, exp, "Login berhasil!")
}

func Refresh(c *gin.Context) {
	raw, err := c.Cookie(refreshCookieName)
	if err != nil || raw == "" {
		helpers.ResponseUnauthorized(c, gin.H{"error": "Refresh token tidak ditemukan"})
		return
	}
	hash := services.HashToken(raw)
	ctx := c.Request.Context()

	tx, err := config.SQLDB.BeginTx(ctx, nil)
	if err != nil {
		config.Log.Error("Gagal memulai transaksi refresh", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Terjadi kesalahan server"})
		return
	}
	defer tx.Rollback() // no-op jika sudah Commit

	tokens := repositories.NewTokenRepository(tx)

	old, err := tokens.FindByHashForUpdate(ctx, hash)
	if errors.Is(err, repositories.ErrNotFound) {
		clearRefreshCookie(c)
		helpers.ResponseUnauthorized(c, gin.H{"error": "Refresh token tidak valid"})
		return
	}
	if err != nil {
		config.Log.Error("Gagal membaca refresh token", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Terjadi kesalahan server"})
		return
	}

	// Token yang sudah dicabut dipakai lagi = indikasi pencurian: cabut seluruh family.
	if old.RevokedAt.Valid {
		_ = tokens.RevokeFamily(ctx, old.FamilyID)
		_ = tx.Commit()
		config.Log.Warn("Reuse refresh token terdeteksi, family dicabut")
		clearRefreshCookie(c)
		helpers.ResponseUnauthorized(c, gin.H{"error": "Sesi tidak valid, silakan login ulang"})
		return
	}

	if time.Now().UTC().After(old.ExpiredAt) {
		clearRefreshCookie(c)
		helpers.ResponseUnauthorized(c, gin.H{"error": "Sesi berakhir, silakan login ulang"})
		return
	}

	user, err := repositories.NewUserRepository(config.SQLDB).FindByID(ctx, old.UserID)
	if err != nil {
		helpers.ResponseUnauthorized(c, gin.H{"error": "User tidak ditemukan"})
		return
	}

	newRaw, newHash, err := services.NewRefreshToken()
	if err != nil {
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal membuat token"})
		return
	}

	if err := tokens.Revoke(ctx, old.ID); err != nil {
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal memperbarui sesi"})
		return
	}
	if err := tokens.Create(ctx, newRefreshRecord(c, user.ID, old.FamilyID, newHash)); err != nil {
		config.Log.Error("Gagal menyimpan refresh token baru", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal memperbarui sesi"})
		return
	}
	if err := tx.Commit(); err != nil {
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal memperbarui sesi"})
		return
	}

	accessToken, exp, err := services.NewAccessToken(user)
	if err != nil {
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal membuat token"})
		return
	}

	setRefreshCookie(c, newRaw, int(services.RefreshTokenTTL.Seconds()))
	respondSession(c, user, accessToken, exp, "Token diperbarui")
}

func Logout(c *gin.Context) {
	if raw, err := c.Cookie(refreshCookieName); err == nil && raw != "" {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		tokens := repositories.NewTokenRepository(config.SQLDB)
		if t, err := tokens.FindByHash(ctx, services.HashToken(raw)); err == nil {
			if err := tokens.RevokeFamily(ctx, t.FamilyID); err != nil {
				config.Log.Error("Gagal mencabut sesi saat logout", zap.Error(err))
			}
		}
	}
	clearRefreshCookie(c)
	helpers.ResponseOK(c, gin.H{"message": "Logout berhasil"})
}

// Me mengembalikan informasi lengkap user yang sedang login (berdasarkan JWT).
func Me(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		helpers.ResponseUnauthorized(c, gin.H{"error": "Sesi tidak valid"})
		return
	}

	ctx := c.Request.Context()

	user, err := repositories.NewUserRepository(config.SQLDB).FindByID(ctx, userID)
	if errors.Is(err, repositories.ErrNotFound) {
		helpers.ResponseUnauthorized(c, gin.H{"error": "User tidak ditemukan"})
		return
	}
	if err != nil {
		config.Log.Error("Gagal membaca user pada /me", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Terjadi kesalahan server"})
		return
	}

	profile, err := repositories.NewProfileRepository(config.SQLDB).GetByID(ctx, userID)
	if errors.Is(err, repositories.ErrNotFound) {
		helpers.ResponseUnauthorized(c, gin.H{"error": "User tidak ditemukan"})
		return
	}
	if err != nil {
		config.Log.Error("Gagal membaca profil pada /me", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Terjadi kesalahan server"})
		return
	}

	helpers.ResponseOK(c, gin.H{
		"id":        uuid.UUID(user.ID).String(),
		"username":  user.Username,
		"name":      user.Name,
		"email":     profile.Email,
		"role":      services.RoleOf(user.Roles),
		"photo_url": photoURL(profile.PhotoPath),
	})
}
