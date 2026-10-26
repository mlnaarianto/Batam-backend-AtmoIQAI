package repositories

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"weather-backend/models"
)

type RefreshToken struct {
	ID        models.UUIDBinary
	UserID    models.UUIDBinary
	FamilyID  models.UUIDBinary
	TokenHash string
	UserAgent string
	IPAddress string
	ExpiredAt time.Time
	RevokedAt sql.NullTime
}

type TokenRepository struct{ db DBTX }

func NewTokenRepository(db DBTX) *TokenRepository { return &TokenRepository{db: db} }

func (r *TokenRepository) Create(ctx context.Context, t *RefreshToken) error {
	const q = `INSERT INTO personal_access_tokens
	  (id, user_id, family_id, token_hash, type, user_agent, ip_address, expired_at)
	  VALUES (?, ?, ?, ?, 'refresh', ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q,
		t.ID, t.UserID, t.FamilyID, t.TokenHash,
		truncate(t.UserAgent, 255), truncate(t.IPAddress, 45),
		t.ExpiredAt.UTC(),
	)
	return err
}

func (r *TokenRepository) FindByHash(ctx context.Context, hash string) (*RefreshToken, error) {
	return r.find(ctx, `SELECT id, user_id, family_id, token_hash, expired_at, revoked_at
	                    FROM personal_access_tokens WHERE token_hash = ? AND type = 'refresh'`, hash)
}

// FindByHashForUpdate mengunci baris sampai transaksi selesai (cegah refresh paralel).
func (r *TokenRepository) FindByHashForUpdate(ctx context.Context, hash string) (*RefreshToken, error) {
	return r.find(ctx, `SELECT id, user_id, family_id, token_hash, expired_at, revoked_at
	                    FROM personal_access_tokens WHERE token_hash = ? AND type = 'refresh' FOR UPDATE`, hash)
}

func (r *TokenRepository) find(ctx context.Context, query string, hash string) (*RefreshToken, error) {
	var t RefreshToken
	err := r.db.QueryRowContext(ctx, query, hash).
		Scan(&t.ID, &t.UserID, &t.FamilyID, &t.TokenHash, &t.ExpiredAt, &t.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Revoke mencabut satu token.
func (r *TokenRepository) Revoke(ctx context.Context, id models.UUIDBinary) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE personal_access_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		time.Now().UTC(), id)
	return err
}

// RevokeFamily mencabut seluruh token dalam satu family (satu sesi login).
func (r *TokenRepository) RevokeFamily(ctx context.Context, familyID models.UUIDBinary) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE personal_access_tokens SET revoked_at = ? WHERE family_id = ? AND revoked_at IS NULL`,
		time.Now().UTC(), familyID)
	return err
}

// RevokeAllByUser mencabut semua refresh token aktif milik user (semua perangkat).
func (r *TokenRepository) RevokeAllByUser(ctx context.Context, userID models.UUIDBinary) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE personal_access_tokens SET revoked_at = ? WHERE user_id = ? AND type = 'refresh' AND revoked_at IS NULL`,
		time.Now().UTC(), userID)
	return err
}

func (r *TokenRepository) DeleteExpired(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM personal_access_tokens WHERE expired_at < ?`, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}