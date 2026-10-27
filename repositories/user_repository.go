package repositories

import (
	"context"
	"database/sql"
	"errors"

	"weather-backend/models"
)

// AuthUser adalah representasi user yang dipakai khusus dalam alur
// autentikasi & sesi (login, refresh, /me).
//
// Kenapa ada struct AuthUser, padahal sudah ada Profile dan UserItem
// di tempat lain? Karena kebutuhan datanya berbeda:
//
//   - Profile / UserItem : untuk tampilan profil / manajemen user,
//     TIDAK butuh password.
//   - AuthUser          : butuh password hash (untuk verifikasi login)
//     dan daftar roles (untuk membuat klaim JWT + menentukan role
//     utama).
//
// Dengan memisahkan struct, kita mencegah password hash "ikut
// terbawa" ke response yang tidak seharusnya.
//
// Detail field:
//
//   - Password  : hash bcrypt — dibandingkan dengan input user di
//     controller Login. Sengaja ada di sini karena hanya AuthUser
//     yang dipakai untuk verifikasi login.
//
//   - PhotoPath : memakai sql.NullString karena kolom di database
//     bisa NULL (user belum unggah foto). Dengan tipe ini kita bisa
//     membedakan "tidak ada foto" (Valid=false) dari string kosong.
//
//   - Roles     : slice nama role (string), bukan struct. Ini cukup
//     untuk kebutuhan:
//       * Menyisipkan claim "role" di JWT (satu role utama).
//       * Cek "apakah user admin?" lewat helper services.RoleOf.
//     Tidak perlu detail RoleItem lengkap di sini.
type AuthUser struct {
	ID        models.UUIDBinary
	Username  string
	Name      string
	Email     string
	Password  string
	PhotoPath sql.NullString
	Roles     []string
}

// UserRepository menangani query user untuk kebutuhan autentikasi.
// Operasi lain (update password, foto, dsb.) ada di ProfileRepository.
type UserRepository struct{ db DBTX }

func NewUserRepository(db DBTX) *UserRepository { return &UserRepository{db: db} }

// FindByUsernameOrEmail mencari user berdasarkan username ATAU email.
//
// Dipakai di:
//   - Controller Login, agar user bisa masuk dengan username maupun email.
//
// Kenapa `username = ? OR email = ?` dan bukan dua method terpisah?
//   - Login hanya butuh satu jalur; user tidak perlu memilih tipe login.
//   - Satu query berarti satu round-trip DB, lebih efisien.
//
// Kenapa `LIMIT 1`?
//   - Skema seharusnya menjamin username & email unik, tapi `LIMIT 1`
//     memberi jaring pengaman: kalau ada data yang (entah bagaimana)
//     duplikat, query tetap mengembalikan satu baris dan tidak error
//     aneh (mis. "expected 1 row, got 2").
//
// Nilai `identifier` dikirim dua kali karena ada dua placeholder (satu
// untuk username, satu untuk email).
func (r *UserRepository) FindByUsernameOrEmail(ctx context.Context, identifier string) (*AuthUser, error) {
	const q = `SELECT id, username, name, email, password, photo_path
	           FROM users
	           WHERE username = ? OR email = ?
	           LIMIT 1`
	return r.findOne(ctx, q, identifier, identifier)
}

// FindByID mencari user berdasarkan ID.
//
// Dipakai di:
//   - Controller Refresh, setelah token valid, untuk mengambil data
//     user terbaru (role bisa saja berubah sejak access token terakhir
//     dibuat).
//   - Controller Me (/api/me) untuk memuat data sesi di frontend.
//   - Middleware RequirePermission (tidak langsung, tapi melalui
//     PermissionRepository yang membaca user_roles).
//
// Kenapa setiap pemanggilan "menarik ulang" roles?
//   - Karena roles adalah bagian dari klaim JWT; saat refresh, kita
//     ingin token baru mencerminkan role yang berlaku SEKARANG,
//     bukan role yang berlaku saat login dulu.
func (r *UserRepository) FindByID(ctx context.Context, id models.UUIDBinary) (*AuthUser, error) {
	const q = `SELECT id, username, name, email, password, photo_path FROM users WHERE id = ? LIMIT 1`
	return r.findOne(ctx, q, id)
}

// findOne adalah helper internal yang mengeksekusi query utama user,
// lalu melengkapinya dengan daftar roles.
//
// Alur:
//  1. Scan satu baris ke AuthUser (termasuk password hash & photo_path).
//  2. Jika tidak ada baris, terjemahkan sql.ErrNoRows menjadi ErrNotFound
//     (error sentinel aplikasi), supaya controller tidak perlu tahu
//     detail driver database.
//  3. Panggil rolesOf untuk mengisi field Roles.
//
// Kenapa mengambil roles di method terpisah, bukan JOIN sekaligus?
//   - JOIN user + user_roles + roles akan menggandakan baris user
//     untuk setiap role yang dimiliki, sehingga harus di-grouping
//     manual dan menyulitkan scan field user lain.
//   - Dua query terpisah jauh lebih sederhana dan tetap cepat
//     (rolesOf memakai index user_id).
//   - Trade-off: dua round-trip DB. Untuk endpoint login/refresh
//     yang tidak dipanggil sesering endpoint baca biasa, ini wajar.
//
// Catatan keamanan:
//   - Password hash IKUT di-scan ke field `Password`. Ini memang
//     disengaja karena AuthUser dipakai untuk verifikasi login.
//   - Pastikan AuthUser TIDAK PERNAH di-serialize langsung ke JSON
//     response; controller harus memilih field yang perlu dikirim
//     (biasanya lewat respondSession).
func (r *UserRepository) findOne(ctx context.Context, query string, args ...any) (*AuthUser, error) {
	var u AuthUser
	err := r.db.QueryRowContext(ctx, query, args...).
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

// rolesOf mengambil daftar nama role yang dimiliki user.
//
// Detail penting:
//
//   - Hanya kolom `r.name` yang diambil. Kita tidak butuh ID, timestamp,
//     atau permission di sini — pemanggil hanya butuh nama role untuk:
//       * Menyusun klaim JWT "role".
//       * Cek "apakah user admin?" lewat services.RoleOf.
//     Jika suatu saat butuh detail role, gunakan RBACRepository.
//
//   - Tidak ada `ORDER BY`. Urutan role tidak bermakna di sini; RoleOf
//     hanya memindai seluruh daftar untuk mencari "admin". Kalau nanti
//     ada kebutuhan tampilan terurut, tambahkan `ORDER BY r.name`.
//
//   - `rows.Err()` WAJIB diperiksa di akhir untuk menangkap error yang
//     muncul saat iterasi (mis. koneksi putus di tengah stream).
//     Tanpa ini, error tersebut hilang diam-diam dan user akan tampak
//     "tidak punya role" — yang berbahaya untuk alur otorisasi.
//
//   - Return `nil` (bukan slice kosong) jika user tidak punya role.
//     Ini berbeda dari pola di RBACRepository yang mengembalikan `[]`.
//     Di sini, `nil` aman karena RoleOf dan pembuatan klaim JWT
//     memperlakukan nil sama seperti slice kosong ("tidak ada role").
//     Jika nanti method ini dipakai untuk response JSON, inisialisasi
//     ke `[]string{}` agar JSON-nya `[]`, bukan `null`.
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