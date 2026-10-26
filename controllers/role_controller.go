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

// UpdateRolePermissions mengganti/menimpa seluruh permission milik sebuah role
// Endpoint: PUT /api/admin/roles/:id/permissions
// Body: { "permission_ids": ["uuid-1", "uuid-2", ...] }
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

	// UUID permission yang tidak valid atau duplikat dilewati
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

	config.Log.Info("Permission role berhasil diperbarui",
		zap.String("role_id", roleIDStr),
		zap.String("role_name", roleName),
		zap.Any("new_permission_ids", input.PermissionIDs),
	)
	helpers.ResponseOK(c, gin.H{"message": "Permission role berhasil diperbarui"})
}

// CreateRole membuat role baru
// Endpoint: POST /api/admin/roles
// Body: { "name": "editor" }
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

// UpdateRole mengubah nama role
// Endpoint: PUT /api/admin/roles/:id
// Body: { "name": "editor-baru" }
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

	// Dibaca ulang: sekaligus memastikan role ada (UPDATE pada ID yang tidak ada tidak error)
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

// DeleteRole menghapus role
// Endpoint: DELETE /api/admin/roles/:id
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