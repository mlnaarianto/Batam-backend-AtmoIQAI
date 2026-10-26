package repositories

import (
	"context"

	"weather-backend/models"
)

type PermissionRepository struct {
	db DBTX
}

func NewPermissionRepository(db DBTX) *PermissionRepository {
	return &PermissionRepository{db: db}
}

// HasPermission true jika user punya role admin, atau punya permission
// yang diminta lewat salah satu rolenya (satu query EXISTS).
func (r *PermissionRepository) HasPermission(ctx context.Context, userID models.UUIDBinary, permission string) (bool, error) {
	const q = `
		SELECT EXISTS (
			SELECT 1
			FROM user_roles ur
			JOIN roles r ON r.id = ur.role_id
			LEFT JOIN role_permissions rp ON rp.role_id = r.id
			LEFT JOIN permissions p ON p.id = rp.permission_id
			WHERE ur.user_id = ?
			  AND (r.name = 'admin' OR p.name = ?)
		)`

	var ok bool
	if err := r.db.QueryRowContext(ctx, q, userID, permission).Scan(&ok); err != nil {
		return false, err
	}
	return ok, nil
}