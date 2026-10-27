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
	// Folder fisik tempat file foto profil disimpan.
	profilePhotoDir = "uploads/profile"
	// Prefix URL publik untuk mengakses file foto (dilayani oleh r.Static di routes.go).
	profilePhotoURL = "/uploads/profile/"
	// Batas ukuran file foto: 2 MB.
	maxPhotoSize = 2 << 20
)

// allowedPhotoTypes memetakan MIME type yang diizinkan ke ekstensi file.
// Hanya tipe ini yang boleh diunggah, dan ekstensinya ditentukan oleh server
// (bukan dari nama file kiriman client) agar tidak bisa dimanipulasi.
var allowedPhotoTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// ChangePasswordInput adalah payload untuk endpoint ganti password.
//   - current_password: password lama untuk verifikasi.
//   - new_password: minimal 8 karakter, divalidasi otomatis oleh Gin.
//   - confirm_password: harus sama dengan new_password (eqfield).
type ChangePasswordInput struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required,min=8"`
	ConfirmPassword string `json:"confirm_password" binding:"required,eqfield=NewPassword"`
}

// currentUserID mengambil user_id dari Gin Context yang sebelumnya di-set
// oleh AuthMiddleware, lalu mengonversinya menjadi models.UUIDBinary.
//
// Konversi dibuat fleksibel karena tipe yang disimpan di context bisa
// bervariasi (UUIDBinary, uuid.UUID, atau string) tergantung cara
// pengambilan claim di AuthMiddleware. Jika tipe tidak dikenali atau
// parsing gagal, kembalikan false agar handler bisa membalas 401.
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

// photoURL mengubah nilai kolom photo_path dari database menjadi URL publik.
// Jika kolom NULL/tidak valid, kembalikan nil sehingga JSON menyimpan `null`
// (bukan string kosong) — frontend lebih mudah membedakan "tidak ada foto".
func photoURL(p sql.NullString) any {
	if !p.Valid {
		return nil
	}
	return profilePhotoURL + p.String
}

// removePhotoFile menghapus file foto lama dari disk.
// filepath.Base dipakai untuk mencegah path traversal jika `name`
// entah bagaimana berisi "../" (seharusnya sudah aman karena nama file
// dibuat oleh server, tapi tetap defensive).
func removePhotoFile(name string) {
	_ = os.Remove(filepath.Join(profilePhotoDir, filepath.Base(name)))
}

// GetProfile mengembalikan data profil user yang sedang login.
// User diidentifikasi dari JWT (bukan dari parameter URL),
// sehingga user tidak bisa mengintip profil user lain.
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

// ChangePassword mengganti password user yang sedang login.
//
// Alur dan alasan desainnya:
//  1. Validasi input (panjang minimal, konfirmasi sama, tidak sama dengan password lama).
//  2. Cek password lama dengan bcrypt. Jika salah → BALAS 400, bukan 401.
//     Alasannya: interceptor frontend biasanya memicu proses /refresh saat
//     menerima 401, padahal di sini masalahnya hanya password lama salah,
//     bukan sesi habis. Maka 400 lebih tepat.
//  3. Simpan password baru (hash bcrypt) ke database.
//  4. Cabut SEMUA refresh token user di semua perangkat agar sesi lain
//     ikut berakhir — praktik standar setelah perubahan kredensial.
//  5. Hapus cookie refresh token di browser saat ini.
//
// Semua perubahan DB dilakukan dalam satu transaksi agar konsisten:
// jika salah satu langkah gagal, tidak ada perubahan yang tersimpan.
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
	// bcrypt hanya memproses maksimal 72 byte; lebih dari itu error diam-diam
	// karena pemotongan, jadi kita tolak di sini agar jelas ke user.
	if len(input.NewPassword) > 72 {
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
	defer tx.Rollback() // no-op jika sudah Commit.

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

	// 400 (bukan 401) — lihat penjelasan di doc comment fungsi.
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

	// Paksa semua sesi lain logout: jika ada penyerang yang sudah login
	// memakai password lama, refresh token mereka ikut dicabut.
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

// UploadProfilePhoto menerima upload foto profil (multipart/form-data, field "photo").
//
// Langkah-langkah keamanannya:
//  1. Batasi ukuran body dengan MaxBytesReader agar tidak bisa di-bomb
//     dengan file raksasa (menghemat memori & disk).
//  2. Validasi tipe file dari ISI (http.DetectContentType), BUKAN dari
//     Content-Type header atau ekstensi nama file — keduanya bisa dipalsukan.
//  3. Simpan file dengan nama acak (UUID) sehingga nama asli dari client
//     tidak pernah dipakai (mencegah path traversal & tabrakan nama).
//  4. Tulis file ke disk, lalu update path di database. Jika update DB gagal,
//     hapus file yang baru dibuat agar tidak ada file yatim.
//  5. Setelah DB update berhasil, hapus file foto lama dari disk.
func UploadProfilePhoto(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		helpers.ResponseUnauthorized(c, gin.H{"error": "Sesi tidak valid"})
		return
	}

	// Batasi body sebelum parsing multipart. Ditambah sedikit (64 KiB)
	// untuk overhead multipart header.
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

	// Baca 512 byte pertama untuk mendeteksi tipe dari magic number file.
	head := make([]byte, 512)
	n, _ := io.ReadFull(src, head)
	ext, allowed := allowedPhotoTypes[http.DetectContentType(head[:n])]
	if !allowed {
		helpers.ResponseBadRequest(c, gin.H{"error": "Format harus JPEG, PNG, atau WebP"})
		return
	}
	// Kembalikan posisi baca ke awal supaya seluruh isi file bisa disalin nanti.
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal memproses file"})
		return
	}

	if err := os.MkdirAll(profilePhotoDir, 0o755); err != nil {
		config.Log.Error("Gagal membuat folder upload", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal menyimpan foto"})
		return
	}

	// Nama file dibuat dari UUID + ekstensi sesuai tipe terdeteksi.
	// O_EXCL memastikan kita tidak menimpa file yang sudah ada.
	name := uuid.NewString() + ext
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
		// Hapus file parsial agar tidak menumpuk sampah di folder upload.
		_ = os.Remove(dstPath)
		config.Log.Error("Gagal menulis file foto", zap.NamedError("copy", copyErr), zap.NamedError("close", closeErr))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal menyimpan foto"})
		return
	}

	ctx := c.Request.Context()
	repo := repositories.NewProfileRepository(config.SQLDB)

	// Simpan path lama dulu sebelum diupdate, supaya file lama bisa dihapus
	// SETELAH update DB berhasil (tidak menghapus file sebelum data baru valid).
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

// DeleteProfilePhoto menghapus foto profil user yang sedang login.
// Path di database di-NULL-kan terlebih dahulu, baru file fisiknya dihapus.
// Urutan ini dipilih agar data di DB selalu konsisten walaupun penghapusan
// file disk gagal (file yatim bisa dibersihkan belakangan, sedangkan path
// yang menunjuk ke file hilang akan menyebabkan error saat diakses frontend).
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