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
//
// Struct di bawah ini adalah bentuk data yang dikirim ke layer controller
// (dan akhirnya ke JSON response). Sengaja dipisah dari struct database
// atau dari model User/Role utama, karena:
//   - Tidak semua kolom tabel perlu keluar (mis. password tidak boleh).
//   - Bentuk nested (role punya permissions, user punya roles) jauh lebih
//     nyaman untuk dikonsumsi frontend dibanding hasil JOIN mentah.

// PermissionItem adalah satu baris tabel `permissions`.
type PermissionItem struct {
	ID        models.UUIDBinary `json:"id"`
	Name      string            `json:"name"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// RoleItem adalah satu role beserta daftar permission yang dimilikinya.
// Permissions berupa slice agar mudah dirender sebagai daftar di frontend.
// Default diinisialisasi ke slice kosong (bukan nil) agar JSON-nya `[]`,
// bukan `null` — lihat implementasi ListRolesWithPermissions.
type RoleItem struct {
	ID          models.UUIDBinary `json:"id"`
	Name        string            `json:"name"`
	Permissions []PermissionItem  `json:"permissions"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// UserItem adalah satu user beserta role yang melekat padanya.
// PERHATIAN: password sengaja TIDAK ada di sini dan juga tidak pernah
// di-SELECT di query manapun pada file ini (lihat ListUsersWithRoles).
type UserItem struct {
	ID       models.UUIDBinary `json:"id"`
	Username string            `json:"username"`
	Name     string            `json:"name"`
	Email    string            `json:"email"`
	Roles    []RoleItem        `json:"roles"`
}

// RBACRepository menangani semua query terkait Role, Permission, dan
// relasinya dengan User (Role-Based Access Control).
type RBACRepository struct{ db DBTX }

func NewRBACRepository(db DBTX) *RBACRepository { return &RBACRepository{db: db} }

// IsDuplicateEntry mengembalikan true jika error adalah MySQL error 1062
// (ER_DUP_ENTRY) — pelanggaran UNIQUE constraint.
//
// Kenapa perlu helper ini?
//   - MySQL tidak memberi error bertipe khusus; yang ada hanya kode numerik.
//   - Controller perlu membedakan "duplikasi nama role" (balas 409 Conflict)
//     dari error server biasa (500). Tanpa cek kode, kita akan salah
//     mengklasifikasi dan membalas 500 padahal itu kesalahan input user.
//
// errors.As dipakai (bukan type assertion biasa) agar tetap bekerja
// kalau error sudah dibungkus (wrapped) oleh fmt.Errorf("%w", ...).
func IsDuplicateEntry(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

// ============================================================================
// BACA
// ============================================================================

// ListPermissions mengambil seluruh permission yang terdaftar di sistem.
// Diurutkan berdasarkan name agar hasil stabil dan mudah dibaca.
//
// Return selalu slice (bukan nil) — `list := []PermissionItem{}` — supaya
// saat tidak ada data, response JSON mengembalikan `[]`, bukan `null`.
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
	// rows.Err() WAJIB dicek: error yang muncul saat iterasi (mis. koneksi
	// putus di tengah stream) tidak terdeteksi oleh `rows.Next() == false`
	// saja, jadi harus diperiksa eksplisit di akhir.
	return list, rows.Err()
}

// ListRolesWithPermissions mengambil semua role beserta permission-nya.
//
// Kenapa dua query, bukan satu JOIN?
//   - JOIN role + role_permissions + permissions dalam satu query akan
//     menghasilkan DUPLIKASI baris role (satu role dengan N permission
//     akan muncul N kali), yang harus di-grouping manual di Go.
//   - Dua query terpisah + map index (roleID -> posisi di slice) jauh
//     lebih sederhana dan lebih efisien untuk ukuran data RBAC.
//
// Alur:
//  1. Query roles, simpan ke `roles` (slice) dan `index` (map ID -> posisi).
//  2. Query pivot role_permissions JOIN permissions.
//  3. Untuk tiap baris pivot, append permission ke role yang sesuai
//     menggunakan lookup map index (O(1) per baris, bukan O(n)).
//
// Kunci detail:
//   - `it.Permissions = []PermissionItem{}` saat inisialisasi: role tanpa
//     permission tetap punya slice kosong, sehingga JSON-nya `[]`, bukan
//     `null`. Ini konsistensi kontrak API untuk frontend.
//   - `rows.Close()` dipanggil MANUAL sebelum query kedua. Pada *sql.DB
//     biasa, lupa close akan membuat koneksi tetap terbuka; pada MySQL
//     dengan connection pool terbatas, ini bisa menyebabkan deadlock
//     saat query berikutnya butuh koneksi.
//   - `defer prows.Close()` untuk query kedua aman karena kita langsung
//     selesai setelah iterasi.
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
		it.Permissions = []PermissionItem{} // slice kosong, bukan nil
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
		// `ok` akan false jika role sudah dihapus di antara dua query.
		// Dalam kasus itu, baris pivot diabaikan supaya hasil tetap valid.
		if i, ok := index[roleID]; ok {
			roles[i].Permissions = append(roles[i].Permissions, p)
		}
	}
	return roles, prows.Err()
}

// ListUsersWithRoles mengambil semua user beserta role yang dimiliki.
//
// Pola sama dengan ListRolesWithPermissions: dua query + map index.
//
// Keamanan data:
//   - Query pertama TIDAK menyertakan kolom password. Password hanya
//     diambil lewat method khusus di ProfileRepository saat benar-benar
//     dibutuhkan untuk verifikasi.
//   - Ini mencegah "kebocoran tidak sengaja" jika suatu saat struct
//     UserItem di-serialize langsung ke JSON response.
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
		u.Roles = []RoleItem{} // slice kosong, bukan nil
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

// GetRole mengambil satu role berdasarkan ID (tanpa memuat permission-nya).
//
// Permission sengaja tidak di-load di sini karena method ini utamanya
// dipakai untuk:
//   - Respons setelah CreateRole/UpdateRole (cukup info role dasar).
//   - Pengecekan keberadaan role sebelum update (lihat controller).
//
// Permissions diinisialisasi ke slice kosong agar konsisten dengan bentuk
// RoleItem di ListRolesWithPermissions.
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

// LockRole mengunci baris role sampai transaksi selesai dan mengembalikan
// nama role (untuk logging).
//
// `FOR UPDATE` HANYA bermakna jika repository dibuat dengan `*sql.Tx`.
// Jika dipanggil dengan `*sql.DB` biasa, MySQL akan mengeksekusi statement
// di autocommit mode sehingga lock langsung dilepas setelah query selesai
// (praktis tidak berguna).
//
// Kegunaan lock ini:
//   - Mencegah dua request mengubah permission role yang sama secara
//     bersamaan (race condition pada pola replace: DELETE lalu INSERT).
//   - Sekaligus memverifikasi role ada: jika tidak ada, kembalikan
//     ErrNotFound, bukan error SQL.
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

// CreateRole membuat role baru dengan ID UUID v7, lalu mengembalikan
// data role lengkap (dibaca ulang lewat GetRole).
//
// Kenapa baca ulang setelah INSERT?
//   - INSERT hanya mengirim kolom yang kita isi (id, name). Kolom seperti
//     created_at/updated_at diisi DEFAULT oleh MySQL, dan kita ingin
//     nilai aslinya ikut dikirim ke client.
//   - Alternatifnya adalah mensimulasikan nilai timestamp di aplikasi,
//     tetapi itu berisiko tidak sinkron dengan DB. Membaca ulang lebih
//     akurat.
//
// Duplikasi nama role tidak diperiksa di sini: akan muncul sebagai
// MySQL error 1062, dan controller yang menerjemahkannya menjadi 409
// lewat IsDuplicateEntry().
func (r *RBACRepository) CreateRole(ctx context.Context, name string) (*RoleItem, error) {
	id := models.NewUUIDBinary() // UUID v7
	if _, err := r.db.ExecContext(ctx, `INSERT INTO roles (id, name) VALUES (?, ?)`, id, name); err != nil {
		return nil, err
	}
	return r.GetRole(ctx, id)
}

// UpdateRoleName mengubah nama role.
//
// Catatan penting: MySQL TIDAK mengembalikan error saat UPDATE mengenai
// 0 baris (mis. ID tidak ada). Artinya, keberhasilan method ini BUKAN
// bukti bahwa role benar-benar ada. Pengecekan keberadaan role dilakukan
// oleh controller (dengan GetRole setelah update).
func (r *RBACRepository) UpdateRoleName(ctx context.Context, id models.UUIDBinary, name string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE roles SET name = ? WHERE id = ?`, name, id)
	return err
}

// DeleteRole menghapus role beserta relasi role_permissions-nya.
//
// HARUS dijalankan di dalam transaksi oleh pemanggil. Urutan operasi:
//  1. Hapus dulu baris di role_permissions (relasi).
//  2. Baru hapus baris di roles.
//
// Kenapa manual, bukan mengandalkan ON DELETE CASCADE?
//   - Tidak semua skema database memakai CASCADE (tergantung migration).
//   - Menghapus eksplisit membuat niat kode jelas dan tidak bergantung
//     pada konfigurasi FK.
//
// Return:
//   - (true, nil)  : role benar-benar terhapus.
//   - (false, nil) : role tidak ada (controller harus membalas 404,
//     bukan 200 "berhasil").
//   - (false, err) : kegagalan query (rollback transaksi).
//
// `RowsAffected()` dipakai karena DELETE pada 0 baris tidak error; kita
// perlu cara untuk membedakan "terhapus" dari "tidak ditemukan".
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

// ReplaceRolePermissions MENGGANTI seluruh permission sebuah role.
// Pola: hapus semua relasi lama, lalu insert relasi baru.
//
// HARUS dijalankan di dalam transaksi (biasanya setelah LockRole), supaya
// tidak ada kondisi "role tanpa permission" yang terlihat oleh request lain
// di antara DELETE dan INSERT.
//
// Detail keamanan & kebenaran:
//
//  1. `placeholders` dibangun dari strings.Repeat("?,", n) untuk membuat
//     daftar placeholder `?,?,?` sesuai jumlah ID. Ini penting karena
//     `IN (?...)` tidak bisa memakai satu placeholder untuk banyak nilai;
//     jumlah placeholder harus sama dengan jumlah argumen.
//
//  2. `INSERT INTO role_permissions SELECT ?, id FROM permissions WHERE id IN (...)`
//     adalah pola yang elegan: alih-alih memvalidasi dulu bahwa setiap
//     permID benar-benar ada di tabel permissions (yang butuh query
//     tambahan), kita biarkan MySQL yang memfilter. Hasilnya:
//       * ID permission yang valid -> ikut ter-insert.
//       * ID permission yang TIDAK ada di tabel permissions -> otomatis
//         tidak ikut (karena tidak lolos WHERE id IN ...).
//     Ini juga mencegah error foreign key untuk ID yang tidak valid,
//     sesuai perilaku yang diinginkan (ID invalid diabaikan, bukan error).
//
//  3. `if len(permIDs) == 0 { return nil }` menangani kasus "hapus semua
//     permission dari role": setelah DELETE, tidak ada yang perlu di-insert.
//     Tanpa pengecekan ini, kita akan membangun query INSERT ... IN ()
//     yang tidak valid secara SQL.
//
//  4. Argumen pertama selalu roleID (untuk placeholder di posisi pertama
//     pada SELECT), diikuti semua permID untuk `IN (...)`. Urutan ini
//     harus cocok dengan urutan placeholder pada query.
func (r *RBACRepository) ReplaceRolePermissions(ctx context.Context, roleID models.UUIDBinary, permIDs []models.UUIDBinary) error {
	// Hapus relasi lama terlebih dahulu (dalam transaksi).
	if _, err := r.db.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id = ?`, roleID); err != nil {
		return err
	}
	if len(permIDs) == 0 {
		return nil
	}

	// Bangun "?,?,?" sesuai jumlah permID.
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

// LockUser mengunci baris user sampai transaksi selesai dan mengembalikan
// username (untuk logging).
//
// Sama seperti LockRole, `FOR UPDATE` hanya bermakna jika repository
// dibuat dengan `*sql.Tx`. Tujuannya:
//   - Mencegah race condition saat dua request mengubah role user yang
//     sama secara bersamaan (pola replace user_roles).
//   - Sekaligus memverifikasi user ada: jika tidak ada, kembalikan
//     ErrNotFound.
func (r *RBACRepository) LockUser(ctx context.Context, id models.UUIDBinary) (string, error) {
	var username string
	err := r.db.QueryRowContext(ctx, `SELECT username FROM users WHERE id = ? FOR UPDATE`, id).Scan(&username)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return username, err
}

// ReplaceUserRoles MENGGANTI seluruh role milik user.
// Pola dan prinsipnya persis sama dengan ReplaceRolePermissions:
//
//   - DELETE relasi lama, INSERT relasi baru, semua di dalam transaksi.
//   - `INSERT ... SELECT ?, id FROM roles WHERE id IN (?...)` membuat
//     ID role yang tidak ada di tabel roles otomatis terabaikan (bukan
//     error), sesuai perilaku yang diinginkan.
//   - `len(roleIDs) == 0` menangani kasus "hapus semua role dari user".
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