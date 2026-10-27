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