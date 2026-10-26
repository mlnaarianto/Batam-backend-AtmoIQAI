package repositories

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"weather-backend/models"

	"github.com/go-sql-driver/mysql"
)

// ============================================================================
// DTO (struct polos, tanpa tag gorm)
// ============================================================================

type PermissionItem struct {
	ID        models.UUIDBinary `json:"id"`
	Name      string            `json:"name"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

type RoleItem struct {
	ID          models.UUIDBinary `json:"id"`
	Name        string            `json:"name"`
	Permissions []PermissionItem  `json:"permissions"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type UserItem struct {
	ID       models.UUIDBinary `json:"id"`
	Username string            `json:"username"`
	Name     string            `json:"name"`
	Email    string            `json:"email"`
	Roles    []RoleItem        `json:"roles"`
}

type RBACRepository struct{ db DBTX }

func NewRBACRepository(db DBTX) *RBACRepository { return &RBACRepository{db: db} }

// IsDuplicateEntry true jika error MySQL 1062 (nilai UNIQUE sudah dipakai).
func IsDuplicateEntry(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

// ============================================================================
// BACA
// ============================================================================

func (r *RBACRepository) ListPermissions(ctx context.Context) ([]PermissionItem, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, created_at, updated_at FROM permissions ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []PermissionItem{}
	for rows.Next() {
		var p PermissionItem
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// ListRolesWithPermissions: 2 query (roles, lalu pivot) lalu digabung di Go.
func (r *RBACRepository) ListRolesWithPermissions(ctx context.Context) ([]RoleItem, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, created_at, updated_at FROM roles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	roles := []RoleItem{}
	index := map[models.UUIDBinary]int{}
	for rows.Next() {
		var it RoleItem
		if err := rows.Scan(&it.ID, &it.Name, &it.CreatedAt, &it.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		it.Permissions = []PermissionItem{}
		index[it.ID] = len(roles)
		roles = append(roles, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	prows, err := r.db.QueryContext(ctx, `
		SELECT rp.role_id, p.id, p.name, p.created_at, p.updated_at
		FROM role_permissions rp
		JOIN permissions p ON p.id = rp.permission_id
		ORDER BY p.name`)
	if err != nil {
		return nil, err
	}
	defer prows.Close()

	for prows.Next() {
		var roleID models.UUIDBinary
		var p PermissionItem
		if err := prows.Scan(&roleID, &p.ID, &p.Name, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		if i, ok := index[roleID]; ok {
			roles[i].Permissions = append(roles[i].Permissions, p)
		}
	}
	return roles, prows.Err()
}

// ListUsersWithRoles: password tidak pernah ikut di-SELECT.
func (r *RBACRepository) ListUsersWithRoles(ctx context.Context) ([]UserItem, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, username, name, email FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	users := []UserItem{}
	index := map[models.UUIDBinary]int{}
	for rows.Next() {
		var u UserItem
		if err := rows.Scan(&u.ID, &u.Username, &u.Name, &u.Email); err != nil {
			rows.Close()
			return nil, err
		}
		u.Roles = []RoleItem{}
		index[u.ID] = len(users)
		users = append(users, u)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rrows, err := r.db.QueryContext(ctx, `
		SELECT ur.user_id, ro.id, ro.name, ro.created_at, ro.updated_at
		FROM user_roles ur
		JOIN roles ro ON ro.id = ur.role_id
		ORDER BY ro.name`)
	if err != nil {
		return nil, err
	}
	defer rrows.Close()

	for rrows.Next() {
		var userID models.UUIDBinary
		var ro RoleItem
		if err := rrows.Scan(&userID, &ro.ID, &ro.Name, &ro.CreatedAt, &ro.UpdatedAt); err != nil {
			return nil, err
		}
		if i, ok := index[userID]; ok {
			users[i].Roles = append(users[i].Roles, ro)
		}
	}
	return users, rrows.Err()
}

func (r *RBACRepository) GetRole(ctx context.Context, id models.UUIDBinary) (*RoleItem, error) {
	var it RoleItem
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, created_at, updated_at FROM roles WHERE id = ?`, id).
		Scan(&it.ID, &it.Name, &it.CreatedAt, &it.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	it.Permissions = []PermissionItem{}
	return &it, nil
}

// LockRole mengunci baris role sampai transaksi selesai dan mengembalikan namanya.
// Hanya bermakna jika repository dibuat dengan *sql.Tx.
func (r *RBACRepository) LockRole(ctx context.Context, id models.UUIDBinary) (string, error) {
	var name string
	err := r.db.QueryRowContext(ctx, `SELECT name FROM roles WHERE id = ? FOR UPDATE`, id).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return name, err
}

// ============================================================================
// TULIS
// ============================================================================

func (r *RBACRepository) CreateRole(ctx context.Context, name string) (*RoleItem, error) {
	id := models.NewUUIDBinary() // UUID v7
	if _, err := r.db.ExecContext(ctx, `INSERT INTO roles (id, name) VALUES (?, ?)`, id, name); err != nil {
		return nil, err
	}
	return r.GetRole(ctx, id)
}

func (r *RBACRepository) UpdateRoleName(ctx context.Context, id models.UUIDBinary, name string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE roles SET name = ? WHERE id = ?`, name, id)
	return err
}

// DeleteRole: jalankan di dalam transaksi. Mengembalikan false jika role tidak ada.
func (r *RBACRepository) DeleteRole(ctx context.Context, id models.UUIDBinary) (bool, error) {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id = ?`, id); err != nil {
		return false, err
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM roles WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ReplaceRolePermissions: jalankan di dalam transaksi. ID permission yang tidak ada
// di tabel permissions otomatis diabaikan (sama seperti perilaku GORM lama).
func (r *RBACRepository) ReplaceRolePermissions(ctx context.Context, roleID models.UUIDBinary, permIDs []models.UUIDBinary) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id = ?`, roleID); err != nil {
		return err
	}
	if len(permIDs) == 0 {
		return nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(permIDs)), ",")
	args := make([]any, 0, len(permIDs)+1)
	args = append(args, roleID)
	for _, id := range permIDs {
		args = append(args, id)
	}

	q := `INSERT INTO role_permissions (role_id, permission_id)
	      SELECT ?, id FROM permissions WHERE id IN (` + placeholders + `)`
	_, err := r.db.ExecContext(ctx, q, args...)
	return err
}

// LockUser mengunci baris user sampai transaksi selesai dan mengembalikan username-nya.
// Hanya bermakna jika repository dibuat dengan *sql.Tx.
func (r *RBACRepository) LockUser(ctx context.Context, id models.UUIDBinary) (string, error) {
	var username string
	err := r.db.QueryRowContext(ctx, `SELECT username FROM users WHERE id = ? FOR UPDATE`, id).Scan(&username)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return username, err
}

// ReplaceUserRoles: jalankan di dalam transaksi. ID role yang tidak ada di tabel roles
// otomatis diabaikan (sama seperti perilaku GORM lama).
func (r *RBACRepository) ReplaceUserRoles(ctx context.Context, userID models.UUIDBinary, roleIDs []models.UUIDBinary) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if len(roleIDs) == 0 {
		return nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(roleIDs)), ",")
	args := make([]any, 0, len(roleIDs)+1)
	args = append(args, userID)
	for _, id := range roleIDs {
		args = append(args, id)
	}

	q := `INSERT INTO user_roles (user_id, role_id)
	      SELECT ?, id FROM roles WHERE id IN (` + placeholders + `)`
	_, err := r.db.ExecContext(ctx, q, args...)
	return err
}