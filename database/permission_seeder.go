package database

import (
	"context"
	"fmt"

	"weather-backend/repositories"
)

func SeedPermissions(ctx context.Context, db repositories.DBTX) error {
	adminPermissions := []string{
		"manage-stations",
		"weather-predictions:view-all",
		"weather-predictions:view-id",
		"manage-weather-predictions",
		"view-weather",
		"manage-roles",
		"manage-role-permissions",
		"manage-user-roles",
	}

	// User biasa secara default hanya view-weather dan view-id
	// (atau sesuai pengaturan admin nantinya di halaman RBAC).
	userPermissions := []string{
		"view-weather",
		"weather-predictions:view-id",
	}

	for _, name := range adminPermissions {
		if _, err := ensurePermission(ctx, db, name); err != nil {
			return err
		}
	}

	adminRoleID, err := ensureRole(ctx, db, "admin")
	if err != nil {
		return err
	}
	userRoleID, err := ensureRole(ctx, db, "user")
	if err != nil {
		return err
	}

	if err := replaceRolePermissions(ctx, db, adminRoleID, adminPermissions); err != nil {
		return fmt.Errorf("set permission admin: %w", err)
	}
	if err := replaceRolePermissions(ctx, db, userRoleID, userPermissions); err != nil {
		return fmt.Errorf("set permission user: %w", err)
	}

	fmt.Println("Seeder Permissions & Role_Permissions berhasil dijalankan!")
	return nil
}