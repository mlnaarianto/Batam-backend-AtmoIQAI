package routes

import (
	"net/http"
	"weather-backend/controllers"
	"weather-backend/middlewares"

	"github.com/gin-gonic/gin"
)

func SetupRouter(r *gin.Engine) {
	// =========================================================================
	// RUTE PUBLIK (Tanpa Autentikasi)
	// =========================================================================
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong! Aeroclimate API is running smoothly."})
	})
	r.POST("/login", controllers.Login)

	// Endpoint publik untuk View All / View Per ID data publik
	r.GET("/api/public/weather-predictions", controllers.GetPublicWeatherPredictions())
	r.GET("/api/public/stations", controllers.GetStations())

	// =========================================================================
	// RUTE TERPROTEKSI - Hanya butuh login
	// =========================================================================
	protected := r.Group("/api")
	protected.Use(middlewares.AuthMiddleware())
	{
		protected.GET("/dashboard", func(c *gin.Context) {
			username, _ := c.Get("username")
			c.JSON(http.StatusOK, gin.H{
				"message": "Selamat datang, " + username.(string) + "!",
			})
		})
	}

	// =========================================================================
	// RUTE ADMIN UMUM (Dashboard Admin & RBAC)
	// =========================================================================
	adminRoutes := r.Group("/api/admin")
	adminRoutes.Use(middlewares.AuthMiddleware(), middlewares.RequirePermission("view-weather"))
	{
		adminRoutes.GET("/dashboard-stats", controllers.GetDashboardStats())
		adminRoutes.GET("/rbac", controllers.GetRBACData)
	}

	// =========================================================================
	// RUTE MANAJEMEN USER ROLES
	// =========================================================================
	userRoleRoutes := r.Group("/api/admin/users")
	userRoleRoutes.Use(middlewares.AuthMiddleware(), middlewares.RequirePermission("manage-user-roles"))
	{
		userRoleRoutes.PUT("/:id/roles", controllers.UpdateUserRoles)
	}

	// =========================================================================
	// RUTE MANAJEMEN ROLE & PERMISSION
	// =========================================================================
	roleRoutes := r.Group("/api/admin/roles")
	roleRoutes.Use(middlewares.AuthMiddleware(), middlewares.RequirePermission("manage-roles"))
	{
		roleRoutes.POST("", controllers.CreateRole)
		roleRoutes.PUT("/:id", controllers.UpdateRole)
		roleRoutes.DELETE("/:id", controllers.DeleteRole)
	}

	rolePermRoutes := r.Group("/api/admin/roles")
	rolePermRoutes.Use(middlewares.AuthMiddleware(), middlewares.RequirePermission("manage-role-permissions"))
	{
		rolePermRoutes.PUT("/:id/permissions", controllers.UpdateRolePermissions)
	}

	// =========================================================================
	// RUTE MANAJEMEN STATION
	// =========================================================================
	stationRoutes := r.Group("/api/admin")
	stationRoutes.Use(middlewares.AuthMiddleware(), middlewares.RequirePermission("manage-stations"))
	{
		stationRoutes.GET("/stations", controllers.GetStations())
		stationRoutes.GET("/stations/:id", controllers.GetStationByID())
		stationRoutes.POST("/stations", controllers.CreateStation())
		stationRoutes.PUT("/stations/:id", controllers.UpdateStation())
		stationRoutes.DELETE("/stations/:id", controllers.DeleteStation())
		stationRoutes.GET("/stations/geocode", controllers.GeocodeSearch())
		stationRoutes.GET("/stations/reverse-geocode", controllers.ReverseGeocode())
	}

	// =========================================================================
	// RUTE WEATHER PREDICTIONS (Akses Fleksibel Berdasarkan Auth & Controller Logic)
	// =========================================================================
	
	// 1. Rute List Utama (Hanya butuh login, logika filter diserahkan cerdas ke controller)
	r.GET(
		"/api/admin/weather-predictions", 
		middlewares.AuthMiddleware(), 
		controllers.GetWeatherPredictions(),
	)

	// 2. Rute Detail Berdasarkan ID Path Parameter (Memerlukan izin view-id)
	r.GET(
		"/api/admin/weather-predictions/:id", 
		middlewares.AuthMiddleware(), 
		middlewares.RequirePermission("weather-predictions:view-id"), 
		controllers.GetWeatherPredictionByID(),
	)

	// 3. Rute untuk Modifikasi Data (Create, Update, Delete - Memerlukan izin manage-weather-predictions)
	managePredictionGroup := r.Group("/api/admin")
	managePredictionGroup.Use(middlewares.AuthMiddleware(), middlewares.RequirePermission("manage-weather-predictions"))
	{
		managePredictionGroup.POST("/weather-predictions", controllers.CreateWeatherPrediction())
		managePredictionGroup.PUT("/weather-predictions/:id", controllers.UpdateWeatherPrediction())
		managePredictionGroup.DELETE("/weather-predictions/:id", controllers.DeleteWeatherPrediction())
	}
}