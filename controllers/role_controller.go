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