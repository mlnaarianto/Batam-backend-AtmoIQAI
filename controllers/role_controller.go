package controllers

import (
	"errors"
	"net/http"

	"weather-backend/config"
	"weather-backend/helpers"
	"weather-backend/models"
	"weather-backend/repositories"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// UpdateRolePermissions MENGGANTI seluruh permission milik sebuah role
// (bukan menambah). Artinya, permission yang tidak disebutkan di body akan
// dihapus dari role tersebut. Pola "replace" dipilih agar frontend cukup
// mengirim kondisi final dari checkbox matriks role-permission, tanpa perlu
// mengirim daftar "tambahan" dan "hapus" secara terpisah.
//
// Endpoint: PUT /api/roles/:id/permissions
// Body: { "permission_ids": ["uuid-1", "uuid-2", ...] }
//
// Alur:
//  1. Bind body & validasi format UUID role.
//  2. Bersihkan input permission_ids: buang yang tidak valid atau duplikat.
//  3. Buka transaksi, lock role (SELECT ... FOR UPDATE) agar tidak bentrok
//     dengan request lain yang mengubah role yang sama secara bersamaan.
//  4. Ganti relasi role_permissions (delete + insert) di dalam transaksi.
//  5. Commit & log.
func UpdateRolePermissions(c *gin.Context) {
	roleIDStr := c.Param("id")

	var input struct {
		PermissionIDs []string `json:"permission_ids"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		config.Log.Warn("Gagal bind JSON saat update permission role",
			zap.String("role_id", roleIDStr), zap.Error(err))
		helpers.ResponseBadRequest(c, gin.H{"error": "Data input tidak valid: " + err.Error()})
		return
	}

	parsedRoleID, err := uuid.Parse(roleIDStr)
	if err != nil {
		helpers.ResponseBadRequest(c, gin.H{"error": "Format UUID role tidak valid"})
		return
	}
	roleID := models.UUIDBinary(parsedRoleID)

	// Normalisasi daftar permission:
	//   - UUID tidak valid -> dilewati (bukan error) agar frontend tetap
	//     bisa mengirim data mentah dari state UI tanpa harus membersihkan.
	//   - UUID nil -> dilewati.
	//   - Duplikat -> dilewati supaya tidak mencoba insert baris ganda yang
	//     akan gagal karena unique constraint.
	seen := map[uuid.UUID]bool{}
	permIDs := make([]models.UUIDBinary, 0, len(input.PermissionIDs))
	for _, s := range input.PermissionIDs {
		p, err := uuid.Parse(s)
		if err != nil || p == uuid.Nil || seen[p] {
			continue
		}
		seen[p] = true
		permIDs = append(permIDs, models.UUIDBinary(p))
	}

	ctx := c.Request.Context()
	tx, err := config.SQLDB.BeginTx(ctx, nil)
	if err != nil {
		config.Log.Error("Gagal memulai transaksi update permission role", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal memperbarui permission role"})
		return
	}
	defer tx.Rollback() // no-op jika sudah Commit

	repo := repositories.NewRBACRepository(tx)

	// LockRole melakukan SELECT ... FOR UPDATE. Tujuannya:
	//   - memastikan role benar-benar ada sebelum diubah (404 kalau tidak),
	//   - mencegah race condition kalau ada dua request mengubah permission
	//     role yang sama pada waktu bersamaan.
	// Nilai roleName dipakai hanya untuk keperluan logging.
	roleName, err := repo.LockRole(ctx, roleID)
	if errors.Is(err, repositories.ErrNotFound) {
		helpers.ResponseNotFound(c, gin.H{"error": "Role tidak ditemukan"})
		return
	}
	if err != nil {
		config.Log.Error("Gagal membaca role", zap.String("role_id", roleIDStr), zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal memperbarui permission role"})
		return
	}

	// ReplaceRolePermissions menghapus relasi lama lalu menyisipkan yang baru
	// dalam satu transaksi, sehingga tidak ada kondisi "role tanpa permission"
	// yang terlihat oleh request lain di tengah proses.
	if err := repo.ReplaceRolePermissions(ctx, roleID, permIDs); err != nil {
		config.Log.Error("Gagal mengganti permission role", zap.String("role_id", roleIDStr), zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal memperbarui permission role"})
		return
	}
	if err := tx.Commit(); err != nil {
		config.Log.Error("Gagal commit update permission role", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal memperbarui permission role"})
		return
	}

	// Log dalam bentuk "input asli" (bukan permIDs yang sudah dinormalisasi)
	// agar saat audit terlihat apa yang sebenarnya dikirim client.
	config.Log.Info("Permission role berhasil diperbarui",
		zap.String("role_id", roleIDStr),
		zap.String("role_name", roleName),
		zap.Any("new_permission_ids", input.PermissionIDs),
	)
	helpers.ResponseOK(c, gin.H{"message": "Permission role berhasil diperbarui"})
}

// CreateRole membuat role baru.
//
// Endpoint: POST /api/roles
// Body: { "name": "editor" }
//
// Validasi:
//   - name wajib diisi, maksimal 100 karakter.
//   - Duplikasi nama role dideteksi dari error MySQL (unique constraint)
//     dan dibalas 409 Conflict, bukan 500, karena ini kesalahan input user.
func CreateRole(c *gin.Context) {
	var input struct {
		Name string `json:"name" binding:"required,max=100"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		helpers.ResponseBadRequest(c, gin.H{"error": "Data input tidak valid: " + err.Error()})
		return
	}

	repo := repositories.NewRBACRepository(config.SQLDB)
	role, err := repo.CreateRole(c.Request.Context(), input.Name)
	if repositories.IsDuplicateEntry(err) {
		// 409 Conflict lebih semantik dibanding 400 untuk kasus duplikasi.
		helpers.WriteJSON(c, http.StatusConflict, "Conflict", gin.H{"error": "Nama role sudah dipakai"})
		return
	}
	if err != nil {
		config.Log.Error("Gagal membuat role baru", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal membuat role"})
		return
	}

	config.Log.Info("Role baru berhasil dibuat", zap.String("role_name", role.Name))
	helpers.ResponseOK(c, gin.H{
		"message": "Role berhasil dibuat",
		"data":    role,
	})
}

// UpdateRole mengubah nama role.
//
// Endpoint: PUT /api/roles/:id
// Body: { "name": "editor-baru" }
//
// Catatan penting: UPDATE pada ID yang tidak ada TIDAK menghasilkan error
// di MySQL — hanya 0 row yang terpengaruh. Karena itu, setelah update kita
// membaca ulang role lewat GetRole untuk memastikan role memang ada,
// sekaligus mengembalikan data terbaru ke frontend. Jika tidak ditemukan,
// balas 404.
func UpdateRole(c *gin.Context) {
	roleIDStr := c.Param("id")

	parsedRoleID, err := uuid.Parse(roleIDStr)
	if err != nil {
		helpers.ResponseBadRequest(c, gin.H{"error": "Format UUID role tidak valid"})
		return
	}
	roleID := models.UUIDBinary(parsedRoleID)

	var input struct {
		Name string `json:"name" binding:"required,max=100"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		helpers.ResponseBadRequest(c, gin.H{"error": "Data input tidak valid: " + err.Error()})
		return
	}

	ctx := c.Request.Context()
	repo := repositories.NewRBACRepository(config.SQLDB)

	if err := repo.UpdateRoleName(ctx, roleID, input.Name); err != nil {
		if repositories.IsDuplicateEntry(err) {
			helpers.WriteJSON(c, http.StatusConflict, "Conflict", gin.H{"error": "Nama role sudah dipakai"})
			return
		}
		config.Log.Error("Gagal memperbarui role", zap.String("role_id", roleIDStr), zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal memperbarui role"})
		return
	}

	// Baca ulang setelah update: sekaligus memverifikasi role ada.
	role, err := repo.GetRole(ctx, roleID)
	if errors.Is(err, repositories.ErrNotFound) {
		helpers.ResponseNotFound(c, gin.H{"error": "Role tidak ditemukan"})
		return
	}
	if err != nil {
		config.Log.Error("Gagal membaca role setelah update", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal memperbarui role"})
		return
	}

	helpers.ResponseOK(c, gin.H{
		"message": "Role berhasil diperbarui",
		"data":    role,
	})
}

// DeleteRole menghapus sebuah role.
//
// Endpoint: DELETE /api/roles/:id
//
// Operasi dibungkus transaksi karena di dalam repo kemungkinan menghapus
// relasi turunan (mis. role_permissions) terlebih dahulu sebelum baris
// role-nya. Jika salah satu langkah gagal, rollback memastikan tidak ada
// data setengah terhapus.
//
// `deleted` dari repo dipakai untuk membedakan:
//   - false -> role tidak ada (balas 404, bukan 200 "berhasil").
//   - true  -> role benar-benar terhapus.
func DeleteRole(c *gin.Context) {
	roleIDStr := c.Param("id")

	parsedRoleID, err := uuid.Parse(roleIDStr)
	if err != nil {
		helpers.ResponseBadRequest(c, gin.H{"error": "Format UUID role tidak valid"})
		return
	}

	ctx := c.Request.Context()
	tx, err := config.SQLDB.BeginTx(ctx, nil)
	if err != nil {
		config.Log.Error("Gagal memulai transaksi hapus role", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal menghapus role"})
		return
	}
	defer tx.Rollback()

	deleted, err := repositories.NewRBACRepository(tx).DeleteRole(ctx, models.UUIDBinary(parsedRoleID))
	if err != nil {
		config.Log.Error("Gagal menghapus role", zap.String("role_id", roleIDStr), zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal menghapus role"})
		return
	}
	if !deleted {
		helpers.ResponseNotFound(c, gin.H{"error": "Role tidak ditemukan"})
		return
	}
	if err := tx.Commit(); err != nil {
		config.Log.Error("Gagal commit hapus role", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal menghapus role"})
		return
	}

	config.Log.Info("Role berhasil dihapus", zap.String("role_id", roleIDStr))
	helpers.ResponseOK(c, gin.H{"message": "Role berhasil dihapus"})
}