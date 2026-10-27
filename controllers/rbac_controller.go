package controllers

import (
	"weather-backend/config"
	"weather-backend/helpers"
	"weather-backend/repositories"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GetRBACData mengembalikan seluruh data yang dibutuhkan halaman manajemen
// RBAC (Role-Based Access Control) di frontend, sekaligus dalam satu response:
//
//   - roles       : daftar role beserta permission yang dimilikinya.
//   - permissions : daftar seluruh permission yang tersedia di sistem.
//   - users       : daftar user beserta role yang saat ini melekat.
//
// Kenapa digabung dalam satu endpoint?
// Halaman RBAC di frontend biasanya butuh ketiga data ini sekaligus untuk
// merender matriks role-permission dan daftar user-role. Menggabungkannya
// menghindari 3 request terpisah + race condition kecil saat data belum
// lengkap. Trade-off: response lebih besar, tapi untuk ukuran data RBAC
// (biasanya puluhan baris) ini masih wajar.
//
// Endpoint ini diproteksi di routes.go dengan:
//
//	AuthMiddleware() + RequirePermission("view-weather")
//
// Jadi hanya user yang sudah login DAN punya izin tersebut yang bisa akses.
func GetRBACData(c *gin.Context) {
	// Pakai context dari request agar query otomatis batal jika client
	// memutus koneksi atau request di-timeout oleh middleware.
	ctx := c.Request.Context()
	repo := repositories.NewRBACRepository(config.SQLDB)

	// 1. Ambil semua role beserta permission-nya.
	//    Jika salah satu query di bawah gagal, seluruh request digagalkan
	//    dengan 500 — karena mengirim data RBAC parsial bisa menyesatkan
	//    frontend saat merender UI.
	roles, err := repo.ListRolesWithPermissions(ctx)
	if err != nil {
		config.Log.Error("Gagal mengambil data roles dari database", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal mengambil data roles"})
		return
	}

	// 2. Ambil master daftar permission.
	permissions, err := repo.ListPermissions(ctx)
	if err != nil {
		config.Log.Error("Gagal mengambil data permissions dari database", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal mengambil data permissions"})
		return
	}

	// 3. Ambil daftar user beserta role-nya (untuk keperluan assign/unassign role).
	users, err := repo.ListUsersWithRoles(ctx)
	if err != nil {
		config.Log.Error("Gagal mengambil data users dari database", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal mengambil data users"})
		return
	}

	// Log ringkasan jumlah data. Berguna untuk audit ringan / debug
	// ketika ada laporan "data RBAC kosong" dari user.
	config.Log.Info("Data RBAC berhasil diambil",
		zap.Int("total_roles", len(roles)),
		zap.Int("total_permissions", len(permissions)),
		zap.Int("total_users", len(users)),
	)

	// Response dibungkus dalam objek "data" agar konsisten dengan format
	// helper standar dan memudahkan frontend menambahkan metadata lain
	// di masa depan tanpa breaking change.
	helpers.ResponseOK(c, gin.H{
		"message": "Data RBAC berhasil diambil",
		"data": gin.H{
			"roles":       roles,
			"permissions": permissions,
			"users":       users,
		},
	})
}