package repositories

import (
	"context"
	"database/sql"
	"errors"

	"weather-backend/models"
)

type AuthUser struct {
	ID        models.UUIDBinary
	Username  string
	Name      string
	Email     string
	Password  string
	PhotoPath sql.NullString // <-- TAMBAHAN: Untuk menampung NULL dari database
	Roles     []string
}

type UserRepository struct{ db DBTX }

func NewUserRepository(db DBTX) *UserRepository { return &UserRepository{db: db} }

func (r *UserRepository) FindByUsernameOrEmail(ctx context.Context, identifier string) (*AuthUser, error) {
	// TAMBAHAN: photo_path di query
	const q = `SELECT id, username, name, email, password, photo_path
	           FROM users
	           WHERE username = ? OR email = ?
	           LIMIT 1`
	return r.findOne(ctx, q, identifier, identifier)
}

func (r *UserRepository) FindByID(ctx context.Context, id models.UUIDBinary) (*AuthUser, error) {
	// TAMBAHAN: photo_path di query
	const q = `SELECT id, username, name, email, password, photo_path FROM users WHERE id = ? LIMIT 1`
	return r.findOne(ctx, q, id)
}

func (r *UserRepository) findOne(ctx context.Context, query string, args ...any) (*AuthUser, error) {
	var u AuthUser
	err := r.db.QueryRowContext(ctx, query, args...).
		// TAMBAHAN: &u.PhotoPath di Scan
		Scan(&u.ID, &u.Username, &u.Name, &u.Email, &u.Password, &u.PhotoPath)
		
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	roles, err := r.rolesOf(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	u.Roles = roles
	return &u, nil
}

func (r *UserRepository) rolesOf(ctx context.Context, userID models.UUIDBinary) ([]string, error) {
	const q = `SELECT r.name
	           FROM user_roles ur
	           JOIN roles r ON r.id = ur.role_id
	           WHERE ur.user_id = ?`
	rows, err := r.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		roles = append(roles, name)
	}
	return roles, rows.Err()
}