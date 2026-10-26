package controllers

import (
	"errors"
	"database/sql"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"weather-backend/config"
	"weather-backend/helpers"
	"weather-backend/models"
	"weather-backend/repositories"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

const (
	profilePhotoDir = "uploads/profile"
	profilePhotoURL = "/uploads/profile/"
	maxPhotoSize    = 2 << 20 // 2 MB
)

var allowedPhotoTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

type ChangePasswordInput struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required,min=8"`
	ConfirmPassword string `json:"confirm_password" binding:"required,eqfield=NewPassword"`
}

// ASUMSI #1: sesuaikan key "user_id" dengan yang di-set AuthMiddleware.
// Ingat bug created_by lama: type assertion yang salah gagal diam-diam.
func currentUserID(c *gin.Context) (models.UUIDBinary, bool) {
	v, ok := c.Get("user_id")
	if !ok {
		return models.UUIDBinary{}, false
	}
	switch id := v.(type) {
	case models.UUIDBinary:
		return id, true
	case uuid.UUID:
		return models.UUIDBinary(id), true
	case string:
		parsed, err := uuid.Parse(id)
		if err != nil {
			return models.UUIDBinary{}, false
		}
		return models.UUIDBinary(parsed), true
	}
	return models.UUIDBinary{}, false
}

func photoURL(p sql.NullString) any {
	if !p.Valid {
		return nil
	}
	return profilePhotoURL + p.String
}

func removePhotoFile(name string) {
	_ = os.Remove(filepath.Join(profilePhotoDir, filepath.Base(name))) // Base() mencegah path traversal
}

func GetProfile(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		helpers.ResponseUnauthorized(c, gin.H{"error": "Sesi tidak valid"})
		return
	}
	p, err := repositories.NewProfileRepository(config.SQLDB).GetByID(c.Request.Context(), userID)
	if errors.Is(err, repositories.ErrNotFound) {
		helpers.ResponseUnauthorized(c, gin.H{"error": "User tidak ditemukan"})
		return
	}
	if err != nil {
		config.Log.Error("Gagal membaca profil", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Terjadi kesalahan server"})
		return
	}
	helpers.ResponseOK(c, gin.H{
		"id":        uuid.UUID(p.ID).String(),
		"username":  p.Username,
		"name":      p.Name,
		"email":     p.Email,
		"photo_url": photoURL(p.PhotoPath),
	})
}

func ChangePassword(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		helpers.ResponseUnauthorized(c, gin.H{"error": "Sesi tidak valid"})
		return
	}

	var input ChangePasswordInput
	if err := c.ShouldBindJSON(&input); err != nil {
		helpers.ResponseBadRequest(c, gin.H{"error": "Input tidak valid: password baru minimal 8 karakter dan konfirmasi harus sama"})
		return
	}
	if len(input.NewPassword) > 72 { // batas bcrypt (dalam byte)
		helpers.ResponseBadRequest(c, gin.H{"error": "Password baru maksimal 72 karakter"})
		return
	}
	if input.NewPassword == input.CurrentPassword {
		helpers.ResponseBadRequest(c, gin.H{"error": "Password baru tidak boleh sama dengan password saat ini"})
		return
	}

	ctx := c.Request.Context()
	tx, err := config.SQLDB.BeginTx(ctx, nil)
	if err != nil {
		config.Log.Error("Gagal memulai transaksi ganti password", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Terjadi kesalahan server"})
		return
	}
	defer tx.Rollback()

	profile := repositories.NewProfileRepository(tx)
	oldHash, err := profile.GetPasswordHash(ctx, userID)
	if errors.Is(err, repositories.ErrNotFound) {
		helpers.ResponseUnauthorized(c, gin.H{"error": "User tidak ditemukan"})
		return
	}
	if err != nil {
		config.Log.Error("Gagal membaca password", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Terjadi kesalahan server"})
		return
	}

	// Sengaja 400, BUKAN 401: interceptor frontend memicu /refresh pada 401.
	if bcrypt.CompareHashAndPassword([]byte(oldHash), []byte(input.CurrentPassword)) != nil {
		helpers.ResponseBadRequest(c, gin.H{"error": "Password saat ini salah"})
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		config.Log.Error("Gagal hash password baru", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Terjadi kesalahan server"})
		return
	}

	if err := profile.UpdatePassword(ctx, userID, string(newHash)); err != nil {
		config.Log.Error("Gagal update password", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal mengubah password"})
		return
	}
	// Cabut semua sesi di semua perangkat.
	if err := repositories.NewTokenRepository(tx).RevokeAllByUser(ctx, userID); err != nil {
		config.Log.Error("Gagal mencabut sesi setelah ganti password", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal mengubah password"})
		return
	}
	if err := tx.Commit(); err != nil {
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal mengubah password"})
		return
	}

	clearRefreshCookie(c)
	helpers.ResponseOK(c, gin.H{"message": "Password berhasil diubah, silakan login ulang"})
}

func UploadProfilePhoto(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		helpers.ResponseUnauthorized(c, gin.H{"error": "Sesi tidak valid"})
		return
	}

	// Batasi body sebelum parsing multipart.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxPhotoSize+(64<<10))
	fh, err := c.FormFile("photo")
	if err != nil {
		helpers.ResponseBadRequest(c, gin.H{"error": "File wajib diisi (field 'photo', maksimal 2 MB)"})
		return
	}
	if fh.Size > maxPhotoSize {
		helpers.ResponseBadRequest(c, gin.H{"error": "Ukuran foto maksimal 2 MB"})
		return
	}

	src, err := fh.Open()
	if err != nil {
		helpers.ResponseBadRequest(c, gin.H{"error": "File tidak bisa dibaca"})
		return
	}
	defer src.Close()

	// Validasi tipe dari ISI file, bukan dari nama/Content-Type kiriman client.
	head := make([]byte, 512)
	n, _ := io.ReadFull(src, head)
	ext, allowed := allowedPhotoTypes[http.DetectContentType(head[:n])]
	if !allowed {
		helpers.ResponseBadRequest(c, gin.H{"error": "Format harus JPEG, PNG, atau WebP"})
		return
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal memproses file"})
		return
	}

	if err := os.MkdirAll(profilePhotoDir, 0o755); err != nil {
		config.Log.Error("Gagal membuat folder upload", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal menyimpan foto"})
		return
	}
	name := uuid.NewString() + ext // nama acak, nama asli dari client tidak dipakai
	dstPath := filepath.Join(profilePhotoDir, name)
	dst, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		config.Log.Error("Gagal membuat file foto", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal menyimpan foto"})
		return
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(dstPath)
		config.Log.Error("Gagal menulis file foto", zap.NamedError("copy", copyErr), zap.NamedError("close", closeErr))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal menyimpan foto"})
		return
	}

	ctx := c.Request.Context()
	repo := repositories.NewProfileRepository(config.SQLDB)
	old, _ := repo.GetPhotoPath(ctx, userID)
	if err := repo.SetPhotoPath(ctx, userID, sql.NullString{String: name, Valid: true}); err != nil {
		_ = os.Remove(dstPath) // jangan tinggalkan file yatim
		config.Log.Error("Gagal menyimpan path foto", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal menyimpan foto"})
		return
	}
	if old.Valid {
		removePhotoFile(old.String)
	}
	helpers.ResponseOK(c, gin.H{"message": "Foto profil diperbarui", "photo_url": profilePhotoURL + name})
}

func DeleteProfilePhoto(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		helpers.ResponseUnauthorized(c, gin.H{"error": "Sesi tidak valid"})
		return
	}
	ctx := c.Request.Context()
	repo := repositories.NewProfileRepository(config.SQLDB)
	old, err := repo.GetPhotoPath(ctx, userID)
	if err != nil {
		helpers.ResponseInternalError(c, gin.H{"error": "Terjadi kesalahan server"})
		return
	}
	if err := repo.SetPhotoPath(ctx, userID, sql.NullString{}); err != nil {
		config.Log.Error("Gagal menghapus path foto", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal menghapus foto"})
		return
	}
	if old.Valid {
		removePhotoFile(old.String)
	}
	helpers.ResponseOK(c, gin.H{"message": "Foto profil dihapus"})
}