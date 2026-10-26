package database

import (
	"context"
	"fmt"

	"weather-backend/repositories"

	"golang.org/x/crypto/bcrypt"
)

func SeedUsers(ctx context.Context, db repositories.DBTX) error {
	// Password default "123" (hanya untuk pengembangan)
	hash, err := bcrypt.GenerateFromPassword([]byte("123"), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password seeder: %w", err)
	}
	hashed := string(hash)

	adminUserID, err := ensureUser(ctx, db, "superadmin", "Super Admin BMKG", "superadmin@gmail.com", hashed)
	if err != nil {
		return err
	}
	regularUserID, err := ensureUser(ctx, db, "warga_batam", "Warga Kota Batam", "warga.batam@gmail.com", hashed)
	if err != nil {
		return err
	}

	adminRoleID, err := ensureRole(ctx, db, "admin")
	if err != nil {
		return err
	}
	userRoleID, err := ensureRole(ctx, db, "user")
	if err != nil {
		return err
	}

	if err := replaceUserRoles(ctx, db, adminUserID, adminRoleID); err != nil {
		return fmt.Errorf("hubungkan role admin ke superadmin: %w", err)
	}
	if err := replaceUserRoles(ctx, db, regularUserID, userRoleID); err != nil {
		return fmt.Errorf("hubungkan role user ke warga_batam: %w", err)
	}

	fmt.Println("Seeder Users & User_Roles berhasil dijalankan!")
	return nil
}