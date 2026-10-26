package database

import (
	"context"
	"fmt"

	"weather-backend/repositories"
)

func SeedRoles(ctx context.Context, db repositories.DBTX) error {
	for _, name := range []string{"admin", "user"} {
		if _, err := ensureRole(ctx, db, name); err != nil {
			return err
		}
	}
	fmt.Println("Seeder Roles berhasil dijalankan!")
	return nil
}