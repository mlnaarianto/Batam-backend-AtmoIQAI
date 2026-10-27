package repositories

import (
	"context"
	"database/sql"
	"errors"

	"weather-backend/models"
)

// ProfileRepository menangani query terkait data profil user, termasuk
// password hash dan path foto profil. Semua field disimpan di tabel `users`.
type ProfileRepository struct{ db DBTX }

// NewProfileRepository membuat instance baru ProfileRepository.
// Parameter `db` bertipe DBTX sehingga repository ini bisa dipakai baik
// untuk query langsung (`config.SQLDB`) maupun di dalam transaksi (`tx`).
func NewProfileRepository(db DBTX) *ProfileRepository { return &ProfileRepository{db: db} }

// Profile adalah representasi data profil user yang dikirim ke layer
// controller. Sengaja tidak memuat password — password hanya diambil
// lewat method khusus GetPasswordHash agar tidak tidak sengaja
// ter-embed di response JSON.
//
// PhotoPath memakai sql.NullString karena kolom photo_path di database
// bisa NULL (user belum mengunggah foto). Dengan tipe ini, kita bisa
// membedakan "tidak ada foto" (Valid=false) dari "ada foto dengan nama
// kosong" (yang seharusnya tidak terjadi).
type Profile struct {
	ID        models.UUIDBinary
	Username  string
	Name      string
	Email     string
	PhotoPath sql.NullString
}

// GetByID mengambil data profil user berdasarkan ID.
//
// Pemakaian utama:
//   - Endpoint GET /api/profile (profil user yang sedang login).
//   - Endpoint GET /api/me (data sesi lengkap).
//
// Jika user tidak ditemukan (mis. sudah dihapus), sql.ErrNoRows
// diterjemahkan menjadi ErrNotFound agar controller bisa membalas 404
// (atau 401, tergantung konteks) tanpa bergantung pada driver MySQL.
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

// GetPasswordHash mengambil HANYA kolom password (hash bcrypt) milik user.
//
// Kenapa dipisah dari GetByID?
//   - Password hash adalah data sensitif; tidak boleh ikut ter-scan ke
//     struct Profile yang mungkin suatu saat di-serialize ke JSON.
//   - Hanya endpoint yang benar-benar butuh verifikasi password (mis.
//     ChangePassword) yang memanggil method ini.
//
// Return berupa string hash siap dibandingkan dengan bcrypt.CompareHashAndPassword.
func (r *ProfileRepository) GetPasswordHash(ctx context.Context, id models.UUIDBinary) (string, error) {
	var hash string
	err := r.db.QueryRowContext(ctx, `SELECT password FROM users WHERE id = ?`, id).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return hash, err
}

// UpdatePassword menyimpan hash password baru untuk user dan
// mematikan penanda must_change_password (set ke 0).
//
// Dipakai saat user mengganti password SENDIRI. Karena user baru saja
// membuktikan dirinya dengan memasukkan password lama dan menetapkan
// yang baru, kewajiban "wajib ganti password" (akibat akun baru dibuat
// atau di-reset admin) otomatis selesai. Tanpa ini, middleware akan
// terus memblokir user walau password sudah diganti.
//
// Catatan:
//   - Parameter `hash` HARUS sudah berupa hash bcrypt (bukan password
//     plaintext). Hashing dilakukan di controller, bukan di sini, supaya
//     repository tetap murni sebagai layer akses data.
//   - Method ini tidak memeriksa apakah user ada. Jika ID tidak ditemukan,
//     MySQL hanya mengembalikan 0 row affected tanpa error. Pengecekan
//     keberadaan user adalah tanggung jawab pemanggil (biasanya lewat
//     GetPasswordHash terlebih dahulu di dalam transaksi yang sama).
func (r *ProfileRepository) UpdatePassword(ctx context.Context, id models.UUIDBinary, hash string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET password = ?, must_change_password = 0 WHERE id = ?`, hash, id)
	return err
}

// GetPhotoPath mengambil nilai kolom photo_path user.
//
// Return berupa sql.NullString agar pemanggil bisa membedakan:
//   - Valid=false -> user belum punya foto (NULL di database).
//   - Valid=true  -> nama file foto tersedia di .String.
//
// Method ini biasanya dipanggil SEBELUM SetPhotoPath untuk menyimpan
// nama file lama, supaya file lama bisa dihapus dari disk setelah
// database berhasil diupdate (lihat controller UploadProfilePhoto).
func (r *ProfileRepository) GetPhotoPath(ctx context.Context, id models.UUIDBinary) (sql.NullString, error) {
	var p sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT photo_path FROM users WHERE id = ?`, id).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// SetPhotoPath menulis nilai kolom photo_path.
//
// Parameter `p` bertipe sql.NullString, sehingga bisa mengirim:
//   - sql.NullString{String: name, Valid: true} -> set foto ke file `name`.
//   - sql.NullString{}                          -> set foto ke NULL
//     (dipakai oleh DeleteProfilePhoto untuk menghapus referensi foto).
//
// Perilaku NULL vs string kosong:
//   - NULL   -> "tidak punya foto" (semantik jelas di response JSON: null).
//   - ""     -> string kosong (menyebabkan URL foto menjadi "/uploads/profile/"
//     tanpa nama file — bukan kondisi yang diinginkan). Karena itu
//     penghapusan foto selalu memakai sql.NullString{} (NULL), bukan "".
//
// Seperti UpdatePassword, method ini tidak memverifikasi keberadaan
// user. Pemanggil yang bertanggung jawab atas hal itu bila perlu.
func (r *ProfileRepository) SetPhotoPath(ctx context.Context, id models.UUIDBinary, p sql.NullString) error {
	_, err := r.db.ExecContext(ctx, `UPDATE users SET photo_path = ? WHERE id = ?`, p, id)
	return err
}
