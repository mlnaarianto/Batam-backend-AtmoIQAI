package controllers

import (
	"errors"

	"weather-backend/config"
	"weather-backend/helpers"
	"weather-backend/models"
	"weather-backend/repositories"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// UpdateUserRoles MENGGANTI seluruh role milik seorang user (bukan menambah).
// Artinya, role yang tidak disebutkan di body akan dihapus dari user tersebut.
// Pola "replace" dipilih agar frontend cukup mengirim kondisi final dari
// checkbox/selection, tanpa perlu mengirim daftar "tambahan" dan "hapus"
// secara terpisah.
//
// Endpoint: PUT /api/users/:id/roles (butuh izin "manage-user-roles")
// Body: { "role_ids": ["uuid-1", "uuid-2", ...] }
//
// Alur:
//  1. Bind body & validasi format UUID user di path.
//  2. Bersihkan input role_ids: buang yang tidak valid atau duplikat.
//  3. Buka transaksi, lock user (SELECT ... FOR UPDATE) agar tidak bentrok
//     dengan request lain yang mengubah role user yang sama secara bersamaan.
//  4. Ganti relasi user_roles (delete + insert) di dalam transaksi.
//  5. Commit & log.
func UpdateUserRoles(c *gin.Context) {
	userIDStr := c.Param("id")

	var input struct {
		RoleIDs []string `json:"role_ids"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		config.Log.Warn("Gagal bind JSON saat update role",
			zap.String("user_id", userIDStr), zap.Error(err))
		helpers.ResponseBadRequest(c, gin.H{"error": "Data input tidak valid: " + err.Error()})
		return
	}

	parsedUserID, err := uuid.Parse(userIDStr)
	if err != nil {
		helpers.ResponseBadRequest(c, gin.H{"error": "Format UUID user tidak valid"})
		return
	}
	userID := models.UUIDBinary(parsedUserID)

	// Normalisasi daftar role:
	//   - UUID tidak valid -> dilewati (bukan error) agar frontend tetap
	//     bisa mengirim data mentah dari state UI tanpa harus membersihkan.
	//   - UUID nil -> dilewati.
	//   - Duplikat -> dilewati supaya tidak mencoba insert baris ganda yang
	//     akan gagal karena unique constraint di tabel relasi.
	seen := map[uuid.UUID]bool{}
	roleIDs := make([]models.UUIDBinary, 0, len(input.RoleIDs))
	for _, s := range input.RoleIDs {
		p, err := uuid.Parse(s)
		if err != nil || p == uuid.Nil || seen[p] {
			continue
		}
		seen[p] = true
		roleIDs = append(roleIDs, models.UUIDBinary(p))
	}

	ctx := c.Request.Context()
	tx, err := config.SQLDB.BeginTx(ctx, nil)
	if err != nil {
		config.Log.Error("Gagal memulai transaksi update role user", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal memperbarui role pengguna"})
		return
	}
	defer tx.Rollback() // no-op jika sudah Commit

	repo := repositories.NewRBACRepository(tx)

	// LockUser melakukan SELECT ... FOR UPDATE. Tujuannya:
	//   - memastikan user benar-benar ada sebelum diubah (404 kalau tidak),
	//   - mencegah race condition kalau ada dua request mengubah role
	//     user yang sama pada waktu bersamaan.
	// Nilai username dipakai hanya untuk keperluan logging.
	username, err := repo.LockUser(ctx, userID)
	if errors.Is(err, repositories.ErrNotFound) {
		helpers.ResponseNotFound(c, gin.H{"error": "Pengguna tidak ditemukan"})
		return
	}
	if err != nil {
		config.Log.Error("Gagal membaca user", zap.String("user_id", userIDStr), zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal memperbarui role pengguna"})
		return
	}

	// ReplaceUserRoles menghapus relasi lama lalu menyisipkan yang baru
	// dalam satu transaksi, sehingga tidak ada kondisi "user tanpa role"
	// yang terlihat oleh request lain di tengah proses.
	if err := repo.ReplaceUserRoles(ctx, userID, roleIDs); err != nil {
		config.Log.Error("Gagal mengganti role user", zap.String("user_id", userIDStr), zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal memperbarui role pengguna"})
		return
	}
	if err := tx.Commit(); err != nil {
		config.Log.Error("Gagal commit update role user", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal memperbarui role pengguna"})
		return
	}

	// Log dalam bentuk "input asli" (bukan roleIDs yang sudah dinormalisasi)
	// agar saat audit terlihat apa yang sebenarnya dikirim client.
	config.Log.Info("Role pengguna berhasil diperbarui",
		zap.String("user_id", userIDStr),
		zap.String("username", username),
		zap.Any("new_role_ids", input.RoleIDs),
	)
	helpers.ResponseOK(c, gin.H{"message": "Role pengguna berhasil diperbarui"})
}