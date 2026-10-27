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

// Nama cookie yang menyimpan refresh token. Access token tidak disimpan di cookie,
// melainkan dikirim di response dan disimpan client (mis. memory/localStorage).
const refreshCookieName = "refresh_token"

// LoginInput adalah payload yang dibutuhkan untuk login.
// Field wajib diisi, validasi otomatis oleh Gin saat binding.
type LoginInput struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// setRefreshCookie menulis refresh token ke cookie HttpOnly.
// - SameSite=Lax: cookie tidak terkirim pada request cross-site (proteksi CSRF dasar).
// - HttpOnly=true: JavaScript di browser tidak bisa membaca cookie (proteksi XSS).
// - Secure: hanya dikirim lewat HTTPS, dikontrol lewat env COOKIE_SECURE.
// - maxAge dalam detik; nilai negatif berarti hapus cookie.
func setRefreshCookie(c *gin.Context, value string, maxAge int) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(refreshCookieName, value, maxAge, "/", "", os.Getenv("COOKIE_SECURE") == "true", true)
}

// clearRefreshCookie menghapus cookie refresh token dari browser.
func clearRefreshCookie(c *gin.Context) { setRefreshCookie(c, "", -1) }

// newRefreshRecord membuat record refresh token yang siap disimpan ke database.
// - ID: identifier unik record.
// - FamilyID: ID "keluarga" token; semua token hasil rotasi dari satu login
//   berbagi FamilyID yang sama, sehingga bisa dicabut bersamaan saat
//   terdeteksi penyalahgunaan.
// - TokenHash: hash SHA256 dari token mentah (token asli tidak pernah disimpan).
// - UserAgent & IPAddress: disimpan untuk audit/forensik.
// - ExpiredAt: kapan token ini kedaluwarsa (UTC).
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

// respondSession menyusun response standar untuk login/refresh.
// Berisi data user + access token + waktu kedaluwarsa access token.
// photo_url dibuat pointer agar bisa dikirim sebagai null jika user belum punya foto.
func respondSession(c *gin.Context, u *repositories.AuthUser, accessToken string, exp time.Time, message string) {
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
		"photo_url":  photoURL,
	})
}

// Login memproses autentikasi user.
// Alur:
//  1. Bind & validasi input.
//  2. Cari user berdasarkan username atau email.
//  3. Bandingkan password dengan hash bcrypt di database.
//  4. Buat access token (JWT) dan refresh token (random).
//  5. Simpan hash refresh token ke database dalam "family" baru.
//  6. Kirim refresh token via cookie HttpOnly, access token via body.
//
// Pesan error untuk user tidak ditemukan dan password salah sengaja disamakan
// agar penyerang tidak bisa menebak apakah suatu username ada atau tidak.
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

	// bcrypt.CompareHashAndPassword membandingkan password input dengan hash tersimpan.
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(input.Password)); err != nil {
		config.Log.Warn("Login gagal: password salah", zap.String("username", user.Username))
		helpers.ResponseUnauthorized(c, gin.H{"error": "Username atau password salah!"})
		return
	}

	// Access token: JWT berumur pendek, dipakai untuk otorisasi tiap request.
	accessToken, exp, err := services.NewAccessToken(user)
	if err != nil {
		config.Log.Error("Gagal membuat access token", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal membuat token"})
		return
	}

	// Refresh token: random 32 byte; yang disimpan di DB hanya hash-nya.
	rawRefresh, hash, err := services.NewRefreshToken()
	if err != nil {
		config.Log.Error("Gagal membuat refresh token", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal membuat token"})
		return
	}

	// Satu login menghasilkan satu "family" baru. Rotasi berikutnya akan
	// mempertahankan FamilyID ini agar bisa dicabut bersama jika perlu.
	familyID := models.NewUUIDBinary()
	if err := tokens.Create(ctx, newRefreshRecord(c, user.ID, familyID, hash)); err != nil {
		config.Log.Error("Gagal menyimpan refresh token", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal menyimpan sesi login"})
		return
	}

	setRefreshCookie(c, rawRefresh, int(services.RefreshTokenTTL.Seconds()))
	config.Log.Info("Login berhasil", zap.String("username", user.Username), zap.String("role", services.RoleOf(user.Roles)))
	respondSession(c, user, accessToken, exp, "Login berhasil!")
}

// Refresh menukar refresh token lama dengan access token baru + refresh token baru
// (refresh token rotation). Proses ini berjalan dalam transaksi agar konsisten.
//
// Deteksi penyalahgunaan: jika refresh token yang dikirim ternyata SUDAH dicabut,
// itu tanda kuat token dicuri lalu dipakai ulang. Dalam kasus ini seluruh family
// token untuk user tersebut dicabut, memaksa login ulang di semua device.
func Refresh(c *gin.Context) {
	raw, err := c.Cookie(refreshCookieName)
	if err != nil || raw == "" {
		helpers.ResponseUnauthorized(c, gin.H{"error": "Refresh token tidak ditemukan"})
		return
	}
	hash := services.HashToken(raw)
	ctx := c.Request.Context()

	// Transaksi dipakai supaya revoke token lama + insert token baru terjadi atomik.
	tx, err := config.SQLDB.BeginTx(ctx, nil)
	if err != nil {
		config.Log.Error("Gagal memulai transaksi refresh", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Terjadi kesalahan server"})
		return
	}
	defer tx.Rollback() // tidak berpengaruh jika Commit sudah dipanggil.

	tokens := repositories.NewTokenRepository(tx)

	// FOR UPDATE: kunci baris agar dua request refresh bersamaan tidak
	// sama-sama berhasil (mencegah race condition pada rotasi token).
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

	// Token sudah dicabut tapi masih dipakai = indikasi pencurian.
	// Cabut seluruh family agar token yang sudah dirotasi juga tidak bisa dipakai.
	if old.RevokedAt.Valid {
		_ = tokens.RevokeFamily(ctx, old.FamilyID)
		_ = tx.Commit()
		config.Log.Warn("Reuse refresh token terdeteksi, family dicabut")
		clearRefreshCookie(c)
		helpers.ResponseUnauthorized(c, gin.H{"error": "Sesi tidak valid, silakan login ulang"})
		return
	}

	// Cek kedaluwarsa secara eksplisit (bisa juga di DB, tapi dicek di sini
	// agar pesan error dan cookie handling-nya jelas).
	if time.Now().UTC().After(old.ExpiredAt) {
		clearRefreshCookie(c)
		helpers.ResponseUnauthorized(c, gin.H{"error": "Sesi berakhir, silakan login ulang"})
		return
	}

	// Ambil data user terbaru (mis. role bisa saja berubah sejak token dibuat).
	user, err := repositories.NewUserRepository(config.SQLDB).FindByID(ctx, old.UserID)
	if err != nil {
		helpers.ResponseUnauthorized(c, gin.H{"error": "User tidak ditemukan"})
		return
	}

	// Siapkan refresh token baru sebelum menyentuh DB, agar jika gagal generate
	// tidak perlu rollback data yang sudah diubah.
	newRaw, newHash, err := services.NewRefreshToken()
	if err != nil {
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal membuat token"})
		return
	}

	// Rotasi: cabut token lama, buat token baru dengan FamilyID yang sama.
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

	// Buat access token baru setelah rotasi sukses.
	accessToken, exp, err := services.NewAccessToken(user)
	if err != nil {
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal membuat token"})
		return
	}

	setRefreshCookie(c, newRaw, int(services.RefreshTokenTTL.Seconds()))
	respondSession(c, user, accessToken, exp, "Token diperbarui")
}

// Logout mencabut seluruh family refresh token milik sesi saat ini,
// lalu menghapus cookie. Access token yang masih aktif tetap valid sampai
// expired (karena JWT stateless); ini trade-off umum yang bisa dimitigasi
// dengan blacklist jti jika dibutuhkan.
func Logout(c *gin.Context) {
	if raw, err := c.Cookie(refreshCookieName); err == nil && raw != "" {
		// Pakai context dengan timeout singkat agar logout tidak menggantung
		// jika DB lambat.
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

// Me mengembalikan informasi lengkap user yang sedang login.
// User diidentifikasi dari user_id yang sudah disisipkan AuthMiddleware
// ke context. Endpoint ini dipakai frontend untuk memuat data sesi
// (mis. setelah reload halaman) tanpa harus login ulang.
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

	// Profil diambil terpisah karena data profil (mis. email, foto) disimpan
	// di tabel/struktur berbeda dari tabel user utama.
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