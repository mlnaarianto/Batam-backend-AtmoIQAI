package routes

import (
	"net/http"

	"weather-backend/controllers"
	"weather-backend/middlewares"

	"github.com/gin-gonic/gin"
)

func SetupRouter(r *gin.Engine) {
	// =========================================================================
	// RUTE PUBLIK (tanpa autentikasi)
	// =========================================================================

	// Health check sederhana untuk memastikan API berjalan.
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong! Aeroclimate API is running smoothly."})
	})

	// Login, refresh, dan logout tidak memakai AuthMiddleware.
	// Alasannya: access token bisa sudah habis, jadi identitas diverifikasi
	// lewat refresh token yang disimpan di cookie.
	// Karena berada di luar /api, ketiganya otomatis lolos dari
	// pemblokiran must_change_password.
	r.POST("/login", controllers.Login)
	r.POST("/refresh", controllers.Refresh)
	r.POST("/logout", controllers.Logout)

	// File foto profil agar bisa diakses lewat URL.
	r.Static("/uploads/profile", "./uploads/profile")

	// Data publik: daftar prediksi cuaca dan daftar stasiun.
	r.GET("/api/public/weather-predictions", controllers.GetPublicWeatherPredictions())
	r.GET("/api/public/stations", controllers.GetStations())

	// =========================================================================
	// RUTE TERPROTEKSI: semua di bawah /api wajib login.
	// Hak akses ditentukan oleh permission per rute, bukan oleh prefix URL.
	// =========================================================================
	api := r.Group("/api", middlewares.AuthMiddleware())

	// Alias agar penulisan permission lebih singkat.
	perm := middlewares.RequirePermission

	// --- Boleh diakses walau must_change_password = 1 ------------------------
	// Hanya dua route ini (ditambah /logout di atas) yang dikecualikan:
	//   - /me: frontend perlu membaca flag must_change_password dan data sesi.
	//   - /profile/password: satu-satunya cara user menyelesaikan kewajibannya.
	// Route baru TIDAK boleh ditaruh di sini kecuali memang sengaja dikecualikan.
	api.GET("/me", controllers.Me)
	api.PUT("/profile/password", controllers.ChangePassword)

	// --- Semua route lain: wajib sudah mengganti password --------------------
	// Default-deny: route yang ditaruh di grup ini otomatis terblokir (403,
	// code "must_change_password") selama user belum mengganti password.
	// Urutan middleware: AuthMiddleware -> RequirePasswordChanged -> perm(...).
	guarded := api.Group("", middlewares.RequirePasswordChanged())

	// --- Sesi & profil sendiri (hanya butuh login) ---------------------------
	// Profil sendiri: ID user selalu diambil dari JWT, jadi tidak perlu RequirePermission.
	guarded.GET("/dashboard", func(c *gin.Context) {
		username, _ := c.Get("username")
		name, _ := username.(string)
		c.JSON(http.StatusOK, gin.H{"message": "Selamat datang, " + name + "!"})
	})
	guarded.GET("/profile", controllers.GetProfile)
	guarded.POST("/profile/photo", controllers.UploadProfilePhoto)
	guarded.DELETE("/profile/photo", controllers.DeleteProfilePhoto)

	// --- Dashboard (butuh view-weather) --------------------------------------
	guarded.GET("/dashboard-stats", perm("view-weather"), controllers.GetDashboardStats())

	// =========================================================================
	// RBAC: siapa boleh apa
	// Controller: role_controller.go (CRUD role) dan rbac_controller.go
	// (matriks RBAC + assign relasi).
	// =========================================================================

	// Data matriks RBAC (roles, permissions, users) dalam satu response.
	guarded.GET("/rbac", perm("view-weather"), controllers.GetRBACData)

	// CRUD role.
	guarded.POST("/roles", perm("manage-roles"), controllers.CreateRole)
	guarded.PUT("/roles/:id", perm("manage-roles"), controllers.UpdateRole)
	guarded.DELETE("/roles/:id", perm("manage-roles"), controllers.DeleteRole)

	// Assign relasi: role <-> permission, user <-> role.
	guarded.PUT("/roles/:id/permissions", perm("manage-role-permissions"), controllers.UpdateRolePermissions)
	guarded.PUT("/users/:id/roles", perm("manage-user-roles"), controllers.UpdateUserRoles)

	// =========================================================================
	// MANAJEMEN USER (oleh admin)
	// Controller: user_controller.go
	// =========================================================================

	// Daftar user dan pembuatan user baru (password sementara,
	// must_change_password = 1).
	guarded.GET("/users", perm("manage-users"), controllers.ListUsers)
	guarded.POST("/users", perm("manage-users"), controllers.CreateUser)

	// Reset password user lain: password diganti, must_change_password = 1,
	// dan semua sesi (refresh token) user tersebut dicabut.
	guarded.POST("/users/:id/reset-password", perm("reset-user-password"), controllers.ResetUserPassword)

	// --- Station (butuh manage-stations) -------------------------------------
	guarded.GET("/stations", perm("manage-stations"), controllers.GetStations())
	guarded.GET("/stations/geocode", perm("manage-stations"), controllers.GeocodeSearch())
	guarded.GET("/stations/reverse-geocode", perm("manage-stations"), controllers.ReverseGeocode())
	guarded.GET("/stations/:id", perm("manage-stations"), controllers.GetStationByID())
	guarded.POST("/stations", perm("manage-stations"), controllers.CreateStation())
	guarded.PUT("/stations/:id", perm("manage-stations"), controllers.UpdateStation())
	guarded.DELETE("/stations/:id", perm("manage-stations"), controllers.DeleteStation())

	// --- Weather predictions -------------------------------------------------
	// List: cukup login; filter data per user diatur di controller.
	guarded.GET("/weather-predictions", controllers.GetWeatherPredictions())

	// Detail: butuh izin view-id.
	guarded.GET("/weather-predictions/:id", perm("weather-predictions:view-id"), controllers.GetWeatherPredictionByID())

	// Create, update, delete: butuh izin manage-weather-predictions.
	guarded.POST("/weather-predictions", perm("manage-weather-predictions"), controllers.CreateWeatherPrediction())
	guarded.PUT("/weather-predictions/:id", perm("manage-weather-predictions"), controllers.UpdateWeatherPrediction())
	guarded.DELETE("/weather-predictions/:id", perm("manage-weather-predictions"), controllers.DeleteWeatherPrediction())
}