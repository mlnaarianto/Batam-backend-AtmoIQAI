package database

import (
	"context"
	"fmt"
	"strings"

	"weather-backend/models"
	"weather-backend/repositories"

	"github.com/google/uuid"
)

// ensureByName: buat baris (id, name) jika belum ada, lalu kembalikan id-nya.
// Nama tabel hanya berasal dari konstanta internal ("roles" atau "permissions"), bukan input user.
func ensureByName(ctx context.Context, db repositories.DBTX, table, name string) (models.UUIDBinary, error) {
	if table != "roles" && table != "permissions" {
		return models.UUIDBinary{}, fmt.Errorf("tabel seed tidak dikenal: %s", table)
	}

	insert := fmt.Sprintf(
		`INSERT INTO %s (id, name)
		 SELECT ?, ? FROM DUAL
		 WHERE NOT EXISTS (SELECT 1 FROM %s WHERE name = ?)`, table, table)
	if _, err := db.ExecContext(ctx, insert, models.UUIDBinary(uuid.New()), name, name); err != nil {
		return models.UUIDBinary{}, fmt.Errorf("insert %s '%s': %w", table, name, err)
	}

	var id models.UUIDBinary
	sel := fmt.Sprintf(`SELECT id FROM %s WHERE name = ?`, table)
	if err := db.QueryRowContext(ctx, sel, name).Scan(&id); err != nil {
		return models.UUIDBinary{}, fmt.Errorf("baca id %s '%s': %w", table, name, err)
	}
	return id, nil
}

func ensureRole(ctx context.Context, db repositories.DBTX, name string) (models.UUIDBinary, error) {
	return ensureByName(ctx, db, "roles", name)
}

func ensurePermission(ctx context.Context, db repositories.DBTX, name string) (models.UUIDBinary, error) {
	return ensureByName(ctx, db, "permissions", name)
}

// ensureUser: buat user jika username belum ada (password hash hanya dipakai saat baru dibuat).
func ensureUser(ctx context.Context, db repositories.DBTX, username, name, email, passwordHash string) (models.UUIDBinary, error) {
	const insert = `INSERT INTO users (id, username, name, email, password)
		SELECT ?, ?, ?, ?, ? FROM DUAL
		WHERE NOT EXISTS (SELECT 1 FROM users WHERE username = ?)`
	if _, err := db.ExecContext(ctx, insert,
		models.UUIDBinary(uuid.New()), username, name, email, passwordHash, username); err != nil {
		return models.UUIDBinary{}, fmt.Errorf("insert user '%s': %w", username, err)
	}

	var id models.UUIDBinary
	if err := db.QueryRowContext(ctx, `SELECT id FROM users WHERE username = ?`, username).Scan(&id); err != nil {
		return models.UUIDBinary{}, fmt.Errorf("baca id user '%s': %w", username, err)
	}
	return id, nil
}

// replaceRolePermissions menimpa seluruh permission milik role (berdasarkan nama permission).
func replaceRolePermissions(ctx context.Context, db repositories.DBTX, roleID models.UUIDBinary, names []string) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id = ?`, roleID); err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(names)), ",")
	args := make([]any, 0, len(names)+1)
	args = append(args, roleID)
	for _, n := range names {
		args = append(args, n)
	}

	res, err := db.ExecContext(ctx,
		`INSERT INTO role_permissions (role_id, permission_id)
		 SELECT ?, id FROM permissions WHERE name IN (`+placeholders+`)`, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); int(n) != len(names) {
		return fmt.Errorf("permission terpasang %d dari %d, ada nama permission yang tidak ditemukan", n, len(names))
	}
	return nil
}

// replaceUserRoles menimpa seluruh role milik user.
func replaceUserRoles(ctx context.Context, db repositories.DBTX, userID, roleID models.UUIDBinary) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id = ?`, userID); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)`, userID, roleID)
	return err
}