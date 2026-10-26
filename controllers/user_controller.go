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

// UpdateUserRoles mengganti/menimpa seluruh role milik seorang user
// Endpoint: PUT /api/admin/users/:id/roles
// Body: { "role_ids": ["uuid-1", "uuid-2", ...] }
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

	// UUID role yang tidak valid atau duplikat dilewati
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

	config.Log.Info("Role pengguna berhasil diperbarui",
		zap.String("user_id", userIDStr),
		zap.String("username", username),
		zap.Any("new_role_ids", input.RoleIDs),
	)
	helpers.ResponseOK(c, gin.H{"message": "Role pengguna berhasil diperbarui"})
}