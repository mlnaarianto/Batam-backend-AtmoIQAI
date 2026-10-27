package repositories

import (
	"context"

	"weather-backend/models"
)

// PermissionRepository menangani seluruh query terkait pengecekan
// permission user. Repository ini sengaja dibuat tipis (hanya satu method
// publik saat ini) karena tujuannya spesifik: menjawab pertanyaan
// "apakah user X boleh melakukan aksi Y?".
type PermissionRepository struct {
	// db bertipe DBTX (interface), bukan *sql.DB, supaya repository
	// bisa dipakai baik untuk query langsung maupun di dalam transaksi
	// (lihat definisi DBTX di db.go).
	db DBTX
}

// NewPermissionRepository membuat instance baru PermissionRepository.
// Pemanggil biasanya mengirim config.SQLDB (untuk query biasa) atau tx
// (untuk query yang harus berada dalam transaksi).
func NewPermissionRepository(db DBTX) *PermissionRepository {
	return &PermissionRepository{db: db}
}

// HasPermission menentukan apakah `userID` memiliki `permission` yang diminta.
//
// Aturan (diimplementasikan langsung di SQL):
//
//  1. Admin BYPASS: jika user memiliki role dengan nama 'admin', maka
//     HasPermission selalu true — apa pun permission yang ditanyakan.
//     Ini memudahkan pengelolaan karena admin tidak perlu diberi semua
//     baris di role_permissions.
//
//  2. User biasa: harus memiliki minimal satu role yang memuat permission
//     yang diminta (lewat relasi user_roles -> roles -> role_permissions
//     -> permissions).
//
// Kenapa dipilih SQL murni, bukan beberapa query di Go?
//   - Satu round-trip ke database, bukan tiga (user_roles, role_permissions,
//     permissions). Untuk middleware yang dipanggil di hampir setiap
//     request terproteksi, ini penting untuk latensi.
//   - Atomik dan konsisten: hasilnya berasal dari satu snapshot data.
//
// Struktur query (dibaca dari dalam ke luar):
//
//	FROM user_roles ur
//	  JOIN roles r              ON r.id = ur.role_id
//	  LEFT JOIN role_permissions rp ON rp.role_id = r.id
//	  LEFT JOIN permissions p        ON p.id = rp.permission_id
//	WHERE ur.user_id = ?
//	  AND (r.name = 'admin' OR p.name = ?)
//
// Detail penting:
//
//   - `JOIN roles r` memakai INNER JOIN karena setiap baris user_roles
//     pasti merujuk ke role yang valid (foreign key). Tidak ada gunanya
//     mempertahankan baris tanpa role.
//
//   - `LEFT JOIN role_permissions rp` dan `LEFT JOIN permissions p`
//     sengaja LEFT (bukan INNER) karena:
//       * Sebuah role boleh memiliki nol permission (mis. role "viewer"
//         yang baru dibuat tapi belum diberi permission).
//       * Jika dipakai INNER JOIN, baris role "admin" tanpa permission
//         akan HILANG dari hasil, padahal admin harus tetap dianggap
//         punya akses lewat cabang `r.name = 'admin'`.
//     Dengan LEFT JOIN, baris role admin tetap muncul (kolom p.name NULL),
//     sehingga kondisi `r.name = 'admin'` tetap bisa cocok.
//
//   - Kondisi `(r.name = 'admin' OR p.name = ?)` diletakkan di WHERE,
//     bukan di ON. Ini disengaja: kita ingin satu baris dianggap cocok
//     jika SALAH SATU dari dua kondisi terpenuhi — bypass admin atau
//     nama permission persis sama dengan yang diminta.
//
//   - Query dibungkus `SELECT EXISTS(...)` sehingga database hanya perlu
//     tahu "ada minimal satu baris yang cocok atau tidak", tanpa menarik
//     seluruh baris ke aplikasi. Hasilnya selalu 1 baris berisi 0/1,
//     lalu di-Scan ke variabel `bool` (MySQL mengembalikan 0/1 yang
//     otomatis dikonversi driver menjadi bool).
//
// Nilai balik:
//   - (true,  nil) : user berhak.
//   - (false, nil) : user tidak berhak (bukan error — memang tidak punya).
//   - (false, err) : query gagal; pemanggil harus membalas 500, BUKAN 403,
//     karena kegagalan teknis bukan berarti user tidak berhak.
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

	// userID bertipe models.UUIDBinary yang mengimplementasikan
	// driver.Valuer, jadi bisa langsung dikirim sebagai parameter
	// dan akan dikonversi menjadi BINARY(16) oleh driver MySQL.
	// permission dikirim sebagai string biasa.
	var ok bool
	if err := r.db.QueryRowContext(ctx, q, userID, permission).Scan(&ok); err != nil {
		return false, err
	}
	return ok, nil
}