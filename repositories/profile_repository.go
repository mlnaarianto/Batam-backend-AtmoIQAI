package repositories

import (
	"context"
	"database/sql"
	"errors"

	"weather-backend/models"
)

type ProfileRepository struct{ db DBTX }

func NewProfileRepository(db DBTX) *ProfileRepository { return &ProfileRepository{db: db} }

type Profile struct {
	ID        models.UUIDBinary
	Username  string
	Name      string
	Email     string
	PhotoPath sql.NullString
}

func (r *ProfileRepository) GetByID(ctx context.Context, id models.UUIDBinary) (*Profile, error) {
	var p Profile
	err := r.db.QueryRowContext(ctx,
		`SELECT id, username, name, email, photo_path FROM users WHERE id = ?`, id,
	).Scan(&p.ID, &p.Username, &p.Name, &p.Email, &p.PhotoPath)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

func (r *ProfileRepository) GetPasswordHash(ctx context.Context, id models.UUIDBinary) (string, error) {
	var hash string
	err := r.db.QueryRowContext(ctx, `SELECT password FROM users WHERE id = ?`, id).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return hash, err
}

func (r *ProfileRepository) UpdatePassword(ctx context.Context, id models.UUIDBinary, hash string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE users SET password = ? WHERE id = ?`, hash, id)
	return err
}

func (r *ProfileRepository) GetPhotoPath(ctx context.Context, id models.UUIDBinary) (sql.NullString, error) {
	var p sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT photo_path FROM users WHERE id = ?`, id).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (r *ProfileRepository) SetPhotoPath(ctx context.Context, id models.UUIDBinary, p sql.NullString) error {
	_, err := r.db.ExecContext(ctx, `UPDATE users SET photo_path = ? WHERE id = ?`, p, id)
	return err
}