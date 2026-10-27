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

	// --- Sesi & profil sendiri (hanya butuh login) ---------------------------
	// Profil sendiri: ID user selalu diambil dari JWT, jadi tidak perlu RequirePermission.
	api.GET("/dashboard", func(c *gin.Context) {
		username, _ := c.Get("username")
		name, _ := username.(string)
		c.JSON(http.StatusOK, gin.H{"message": "Selamat datang, " + name + "!"})
	})
	api.GET("/me", controllers.Me)
	api.GET("/profile", controllers.GetProfile)
	api.PUT("/profile/password", controllers.ChangePassword)
	api.POST("/profile/photo", controllers.UploadProfilePhoto)
	api.DELETE("/profile/photo", controllers.DeleteProfilePhoto)

	// --- Dashboard (butuh view-weather) --------------------------------------
	api.GET("/dashboard-stats", perm("view-weather"), controllers.GetDashboardStats())

	// =========================================================================
	// RBAC: siapa boleh apa
	// Controller: role_controller.go (CRUD role) dan rbac_controller.go
	// (matriks RBAC + assign relasi).
	// =========================================================================

	// Data matriks RBAC (roles, permissions, users) dalam satu response.
	api.GET("/rbac", perm("view-weather"), controllers.GetRBACData)

	// CRUD role.
	api.POST("/roles", perm("manage-roles"), controllers.CreateRole)
	api.PUT("/roles/:id", perm("manage-roles"), controllers.UpdateRole)
	api.DELETE("/roles/:id", perm("manage-roles"), controllers.DeleteRole)

	// Assign relasi: role <-> permission, user <-> role.
	api.PUT("/roles/:id/permissions", perm("manage-role-permissions"), controllers.UpdateRolePermissions)
	api.PUT("/users/:id/roles", perm("manage-user-roles"), controllers.UpdateUserRoles)

	// =========================================================================
	// MANAJEMEN USER (oleh admin)
	// Controller: user_controller.go
	// Rute akan ditambahkan di sini: list, create, reset password.
	// =========================================================================

	// --- Station (butuh manage-stations) -------------------------------------
	api.GET("/stations", perm("manage-stations"), controllers.GetStations())
	api.GET("/stations/geocode", perm("manage-stations"), controllers.GeocodeSearch())
	api.GET("/stations/reverse-geocode", perm("manage-stations"), controllers.ReverseGeocode())
	api.GET("/stations/:id", perm("manage-stations"), controllers.GetStationByID())
	api.POST("/stations", perm("manage-stations"), controllers.CreateStation())
	api.PUT("/stations/:id", perm("manage-stations"), controllers.UpdateStation())
	api.DELETE("/stations/:id", perm("manage-stations"), controllers.DeleteStation())

	// --- Weather predictions -------------------------------------------------
	// List: cukup login; filter data per user diatur di controller.
	api.GET("/weather-predictions", controllers.GetWeatherPredictions())

	// Detail: butuh izin view-id.
	api.GET("/weather-predictions/:id", perm("weather-predictions:view-id"), controllers.GetWeatherPredictionByID())

	// Create, update, delete: butuh izin manage-weather-predictions.
	api.POST("/weather-predictions", perm("manage-weather-predictions"), controllers.CreateWeatherPrediction())
	api.PUT("/weather-predictions/:id", perm("manage-weather-predictions"), controllers.UpdateWeatherPrediction())
	api.DELETE("/weather-predictions/:id", perm("manage-weather-predictions"), controllers.DeleteWeatherPrediction())
}