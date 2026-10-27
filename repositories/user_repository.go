package repositories

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"weather-backend/models"
)

// ManagedUser adalah representasi user untuk tampilan manajemen user
// oleh admin (daftar user).
//
// Kenapa bukan UserItem? Nama itu sudah dipakai di rbac_repository.go
// untuk kebutuhan halaman RBAC (Roles berupa []RoleItem). Di sini Roles
// cukup berupa nama role (string) dan ada field tambahan
// must_change_password serta created_at.
//
// Sengaja TIDAK memuat password hash, supaya aman di-serialize langsung
// ke response JSON.
type ManagedUser struct {
	ID                 models.UUIDBinary `json:"id"`
	Username           string            `json:"username"`
	Name               string            `json:"name"`
	Email              string            `json:"email"`
	MustChangePassword bool              `json:"must_change_password"`
	Roles              []string          `json:"roles"`
	CreatedAt          time.Time         `json:"created_at"`
}

// NewUser adalah data masukan untuk membuat user baru oleh admin.
//
// PasswordHash HARUS sudah berupa hash bcrypt (hashing dilakukan di
// controller/service sebelum masuk repository).
type NewUser struct {
	ID           models.UUIDBinary
	Username     string
	Name         string
	Email        string
	PasswordHash string
}

// UserRepository menangani manajemen user oleh admin: daftar user,
// pembuatan user, dan reset password.
//
// Query login/sesi ada di AuthRepository; ganti password oleh user
// sendiri ada di ProfileRepository.
type UserRepository struct{ db DBTX }

func NewUserRepository(db DBTX) *UserRepository { return &UserRepository{db: db} }

// List mengambil semua user beserta role-nya, terurut berdasarkan nama.
//
// Dua query (user, lalu role semua user sekaligus) dipilih daripada JOIN
// agar tidak ada baris user ganda dan tidak perlu grouping manual.
// Role diambil sekali untuk semua user (bukan satu query per user)
// supaya tidak terjadi masalah N+1.
func (r *UserRepository) List(ctx context.Context) ([]ManagedUser, error) {
	const q = `SELECT id, username, name, email, must_change_password, created_at
	           FROM users
	           ORDER BY name`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []ManagedUser{}
	index := map[models.UUIDBinary]int{}
	for rows.Next() {
		var u ManagedUser
		if err := rows.Scan(&u.ID, &u.Username, &u.Name, &u.Email, &u.MustChangePassword, &u.CreatedAt); err != nil {
			return nil, err
		}
		u.Roles = []string{}
		index[u.ID] = len(items)
		items = append(items, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	const rq = `SELECT ur.user_id, r.name
	            FROM user_roles ur
	            JOIN roles r ON r.id = ur.role_id
	            ORDER BY r.name`
	roleRows, err := r.db.QueryContext(ctx, rq)
	if err != nil {
		return nil, err
	}
	defer roleRows.Close()

	for roleRows.Next() {
		var userID models.UUIDBinary
		var roleName string
		if err := roleRows.Scan(&userID, &roleName); err != nil {
			return nil, err
		}
		if i, ok := index[userID]; ok {
			items[i].Roles = append(items[i].Roles, roleName)
		}
	}
	return items, roleRows.Err()
}

// Create menyimpan user baru dengan must_change_password = 1.
//
// User yang dibuat admin memakai password sementara, jadi wajib
// menggantinya saat login pertama. Penetapan role default ("user")
// dilakukan terpisah (lihat AssignRole) agar Create tetap sederhana.
//
// Pelanggaran unik username/email dikembalikan apa adanya sebagai
// error driver; controller bertugas menerjemahkannya (mis. ke 409).
func (r *UserRepository) Create(ctx context.Context, u *NewUser) error {
	const q = `INSERT INTO users (id, username, name, email, password, must_change_password)
	           VALUES (?, ?, ?, ?, ?, 1)`
	_, err := r.db.ExecContext(ctx, q, u.ID, u.Username, u.Name, u.Email, u.PasswordHash)
	return err
}

// AssignRole memberi user sebuah role berdasarkan NAMA role.
//
// Memakai INSERT ... SELECT agar tidak perlu query terpisah untuk
// mencari ID role. Jika nama role tidak ada, tidak ada baris yang
// ter-insert dan ErrNotFound dikembalikan, supaya kegagalan tidak
// diam-diam (user tanpa role).
func (r *UserRepository) AssignRole(ctx context.Context, userID models.UUIDBinary, roleName string) error {
	const q = `INSERT INTO user_roles (user_id, role_id)
	           SELECT ?, id FROM roles WHERE name = ?`
	res, err := r.db.ExecContext(ctx, q, userID, roleName)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ResetPassword mengganti password user dengan hash baru dan
// menyalakan must_change_password = 1, sehingga user wajib
// menggantinya lagi saat login berikutnya.
//
// Mengembalikan ErrNotFound jika user tidak ada. Pemanggil harus
// memanggil TokenRepository.RevokeAllByUser di transaksi yang sama
// agar sesi lama ikut mati.
func (r *UserRepository) ResetPassword(ctx context.Context, userID models.UUIDBinary, passwordHash string) error {
	const q = `UPDATE users SET password = ?, must_change_password = 1 WHERE id = ?`
	res, err := r.db.ExecContext(ctx, q, passwordHash, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// RowsAffected bisa 0 jika nilai tidak berubah; cek keberadaan user.
		var exists int
		err := r.db.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id = ?`, userID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return nil
}