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
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong! Aeroclimate API is running smoothly."})
	})

	// /refresh dan /logout sengaja TANPA AuthMiddleware karena dipanggil saat
	// access token sudah habis. Identitas dibuktikan oleh refresh token di cookie.
	r.POST("/login", controllers.Login)
	r.POST("/refresh", controllers.Refresh)
	r.POST("/logout", controllers.Logout)

	// File foto profil agar bisa ditampilkan lewat URL
	r.Static("/uploads/profile", "./uploads/profile")

	// Data publik (View All / View per ID)
	r.GET("/api/public/weather-predictions", controllers.GetPublicWeatherPredictions())
	r.GET("/api/public/stations", controllers.GetStations())

	// =========================================================================
	// RUTE TERPROTEKSI: semua di bawah /api wajib login.
	// Akses ditentukan oleh permission per rute, bukan oleh prefix URL.
	// =========================================================================
	api := r.Group("/api", middlewares.AuthMiddleware())

	perm := middlewares.RequirePermission

	// --- Hanya butuh login ---------------------------------------------------
	api.GET("/dashboard", func(c *gin.Context) {
		username, _ := c.Get("username")
		name, _ := username.(string)
		c.JSON(http.StatusOK, gin.H{"message": "Selamat datang, " + name + "!"})
	})
	api.GET("/me", controllers.Me)

	// Profil sendiri (ID selalu dari JWT, tanpa RequirePermission)
	api.GET("/profile", controllers.GetProfile)
	api.PUT("/profile/password", controllers.ChangePassword)
	api.POST("/profile/photo", controllers.UploadProfilePhoto)
	api.DELETE("/profile/photo", controllers.DeleteProfilePhoto)

	// --- Dashboard & RBAC (view-weather) --------------------------------------
	api.GET("/dashboard-stats", perm("view-weather"), controllers.GetDashboardStats())
	api.GET("/rbac", perm("view-weather"), controllers.GetRBACData)

	// --- Role user ------------------------------------------------------------
	api.PUT("/users/:id/roles", perm("manage-user-roles"), controllers.UpdateUserRoles)

	// --- Role & permission ----------------------------------------------------
	api.POST("/roles", perm("manage-roles"), controllers.CreateRole)
	api.PUT("/roles/:id", perm("manage-roles"), controllers.UpdateRole)
	api.DELETE("/roles/:id", perm("manage-roles"), controllers.DeleteRole)
	api.PUT("/roles/:id/permissions", perm("manage-role-permissions"), controllers.UpdateRolePermissions)

	// --- Station (manage-stations) --------------------------------------------
	api.GET("/stations", perm("manage-stations"), controllers.GetStations())
	api.GET("/stations/geocode", perm("manage-stations"), controllers.GeocodeSearch())
	api.GET("/stations/reverse-geocode", perm("manage-stations"), controllers.ReverseGeocode())
	api.GET("/stations/:id", perm("manage-stations"), controllers.GetStationByID())
	api.POST("/stations", perm("manage-stations"), controllers.CreateStation())
	api.PUT("/stations/:id", perm("manage-stations"), controllers.UpdateStation())
	api.DELETE("/stations/:id", perm("manage-stations"), controllers.DeleteStation())

	// --- Weather predictions ----------------------------------------------------
	// List: hanya login, filter per user diatur di controller
	api.GET("/weather-predictions", controllers.GetWeatherPredictions())
	// Detail: butuh izin view-id
	api.GET("/weather-predictions/:id", perm("weather-predictions:view-id"), controllers.GetWeatherPredictionByID())
	// Create, update, delete: butuh izin manage-weather-predictions
	api.POST("/weather-predictions", perm("manage-weather-predictions"), controllers.CreateWeatherPrediction())
	api.PUT("/weather-predictions/:id", perm("manage-weather-predictions"), controllers.UpdateWeatherPrediction())
	api.DELETE("/weather-predictions/:id", perm("manage-weather-predictions"), controllers.DeleteWeatherPrediction())
}