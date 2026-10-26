package controllers

import (
	"weather-backend/config"
	"weather-backend/helpers"
	"weather-backend/repositories"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GetRBACData mengambil seluruh data Role, Permission, dan relasinya dengan User
func GetRBACData(c *gin.Context) {
	ctx := c.Request.Context()
	repo := repositories.NewRBACRepository(config.SQLDB)

	roles, err := repo.ListRolesWithPermissions(ctx)
	if err != nil {
		config.Log.Error("Gagal mengambil data roles dari database", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal mengambil data roles"})
		return
	}

	permissions, err := repo.ListPermissions(ctx)
	if err != nil {
		config.Log.Error("Gagal mengambil data permissions dari database", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal mengambil data permissions"})
		return
	}

	users, err := repo.ListUsersWithRoles(ctx)
	if err != nil {
		config.Log.Error("Gagal mengambil data users dari database", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal mengambil data users"})
		return
	}

	config.Log.Info("Data RBAC berhasil diambil",
		zap.Int("total_roles", len(roles)),
		zap.Int("total_permissions", len(permissions)),
		zap.Int("total_users", len(users)),
	)

	helpers.ResponseOK(c, gin.H{
		"message": "Data RBAC berhasil diambil",
		"data": gin.H{
			"roles":       roles,
			"permissions": permissions,
			"users":       users,
		},
	})
}