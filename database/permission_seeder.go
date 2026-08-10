package database

import (
	"fmt"
	"weather-backend/config"
	"weather-backend/models"
)

func SeedPermissions() {
	// Permission granular untuk weather predictions & station/role lainnya
	manageStationsPerm := models.Permission{Name: "manage-stations"}
	
	// PERUBAHAN: Mengubah permission weather menjadi lebih granular (view-all & view-id)
	viewAllWeatherPerm := models.Permission{Name: "weather-predictions:view-all"}
	viewIDWeatherPerm := models.Permission{Name: "weather-predictions:view-id"}
	manageWeatherPerm := models.Permission{Name: "manage-weather-predictions"}

	viewWeatherPerm := models.Permission{Name: "view-weather"}
	manageRolesPerm := models.Permission{Name: "manage-roles"}
	manageRolePermissionsPerm := models.Permission{Name: "manage-role-permissions"}
	manageUserRolesPerm := models.Permission{Name: "manage-user-roles"}

	// Eksekusi FirstOrCreate
	config.DB.FirstOrCreate(&manageStationsPerm, models.Permission{Name: "manage-stations"})
	config.DB.FirstOrCreate(&viewAllWeatherPerm, models.Permission{Name: "weather-predictions:view-all"})
	config.DB.FirstOrCreate(&viewIDWeatherPerm, models.Permission{Name: "weather-predictions:view-id"})
	config.DB.FirstOrCreate(&manageWeatherPerm, models.Permission{Name: "manage-weather-predictions"})
	config.DB.FirstOrCreate(&viewWeatherPerm, models.Permission{Name: "view-weather"})
	config.DB.FirstOrCreate(&manageRolesPerm, models.Permission{Name: "manage-roles"})
	config.DB.FirstOrCreate(&manageRolePermissionsPerm, models.Permission{Name: "manage-role-permissions"})
	config.DB.FirstOrCreate(&manageUserRolesPerm, models.Permission{Name: "manage-user-roles"})

	var adminRole models.Role
	if err := config.DB.Where("name = ?", "admin").First(&adminRole).Error; err != nil {
		fmt.Println("Gagal menemukan role admin:", err)
		return
	}

	var userRole models.Role
	if err := config.DB.Where("name = ?", "user").First(&userRole).Error; err != nil {
		fmt.Println("Gagal menemukan role user:", err)
		return
	}

	// Admin diberikan hak akses penuh termasuk view-all
	config.DB.Model(&adminRole).Association("Permissions").Replace([]models.Permission{
		manageStationsPerm,
		viewAllWeatherPerm,
		viewIDWeatherPerm,
		manageWeatherPerm,
		viewWeatherPerm,
		manageRolesPerm,
		manageRolePermissionsPerm,
		manageUserRolesPerm,
	})
	
	// User biasa secara default hanya diberikan hak akses view-id (atau sesuai pengaturan admin nantinya di RBAC)
	config.DB.Model(&userRole).Association("Permissions").Replace([]models.Permission{
		viewWeatherPerm,
		viewIDWeatherPerm,
	})

	fmt.Println("Seeder Permissions & Role_Permissions berhasil dijalankan!")
}