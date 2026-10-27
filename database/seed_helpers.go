package database

import (
	"context"
	"fmt"
	"strings"

	"weather-backend/models"
	"weather-backend/repositories"
)

// ensureByName memastikan baris (id, name) ada di tabel `table`,
// lalu mengembalikan ID-nya.
//
// Pola yang dipakai adalah "INSERT ... SELECT ... WHERE NOT EXISTS",
// yang berfungsi sebagai upsert tanpa duplikasi:
//
//	INSERT INTO roles (id, name)
//	SELECT ?, ? FROM DUAL
//	WHERE NOT EXISTS (SELECT 1 FROM roles WHERE name = ?)
//
// Kenapa memakai pola ini, bukan `INSERT IGNORE` atau
// `ON DUPLICATE KEY UPDATE`?
//
//   - `INSERT IGNORE` menyembunyikan SEMUA error (termasuk error serius
//     seperti tipe data salah), bukan hanya duplikasi. Ini berbahaya
//     di seeder.
//   - `ON DUPLICATE KEY UPDATE` membutuhkan unique index pada kolom
//     `name`. Skema saat ini memang punya unique index, tapi pola
//     `WHERE NOT EXISTS` tidak bergantung pada detail skema dan
//     semantiknya jelas: "insert hanya kalau belum ada".
//   - `SELECT ... FROM DUAL` dipakai agar bentuknya konsisten dengan
//     `INSERT ... SELECT` yang membutuhkan klausa FROM di MySQL.
//     `DUAL` adalah tabel dummy bawaan MySQL.
//
// Keamanan SQL injection:
//   - `table` di sini TIDAK berasal dari input user; hanya dari
//     konstanta internal ("roles" atau "permissions").
//   - Meskipun begitu, tetap ada whitelist eksplisit di awal fungsi
//     sebagai lapisan pertahanan tambahan: mencegah kesalahan
//     pemanggilan (mis. typo) yang bisa menghasilkan query dengan
//     nama tabel aneh.
//   - Semua nilai (id, name) dikirim sebagai placeholder `?`,
//     tidak pernah dirangkai ke string query.
//
// Nilai balik: ID baris (baru dibuat atau yang sudah ada).
// Pemanggil biasanya membutuhkan ID ini untuk operasi berikutnya
// (mis. replace role_permissions).
func ensureByName(ctx context.Context, db repositories.DBTX, table, name string) (models.UUIDBinary, error) {
	// Whitelist tabel: hanya "roles" dan "permissions" yang boleh
	// dipakai sebagai nilai `table`. Ini mencegah pemakaian tak
	// sengaja pada tabel lain (mis. `users` yang punya pola
	// berbeda — lihat ensureUser).
	if table != "roles" && table != "permissions" {
		return models.UUIDBinary{}, fmt.Errorf("tabel seed tidak dikenal: %s", table)
	}

	// Insert kondisional: hanya menyisipkan jika `name` belum ada.
	// `models.NewUUIDBinary()` dipakai sebagai ID baru (UUID v7),
	// dikirim sebagai argumen pertama.
	insert := fmt.Sprintf(
		`INSERT INTO %s (id, name)
		 SELECT ?, ? FROM DUAL
		 WHERE NOT EXISTS (SELECT 1 FROM %s WHERE name = ?)`, table, table)
	if _, err := db.ExecContext(ctx, insert, models.NewUUIDBinary(), name, name); err != nil {
		return models.UUIDBinary{}, fmt.Errorf("insert %s '%s': %w", table, name, err)
	}

	// Baca ulang ID-nya, baik untuk baris yang baru dibuat maupun
	// yang sudah ada sebelumnya. Satu query SELECT lebih sederhana
	// dan pasti benar dibanding mencoba membaca LastInsertId (yang
	// hanya berlaku untuk kolom AUTO_INCREMENT, bukan UUID).
	var id models.UUIDBinary
	sel := fmt.Sprintf(`SELECT id FROM %s WHERE name = ?`, table)
	if err := db.QueryRowContext(ctx, sel, name).Scan(&id); err != nil {
		return models.UUIDBinary{}, fmt.Errorf("baca id %s '%s': %w", table, name, err)
	}
	return id, nil
}

// ensureRole adalah shortcut ensureByName untuk tabel `roles`.
func ensureRole(ctx context.Context, db repositories.DBTX, name string) (models.UUIDBinary, error) {
	return ensureByName(ctx, db, "roles", name)
}

// ensurePermission adalah shortcut ensureByName untuk tabel `permissions`.
func ensurePermission(ctx context.Context, db repositories.DBTX, name string) (models.UUIDBinary, error) {
	return ensureByName(ctx, db, "permissions", name)
}

// ensureUser memastikan user dengan `username` tertentu ada di tabel
// users, lalu mengembalikan ID-nya.
//
// Beda dengan ensureByName karena tabel `users` memiliki kolom
// tambahan (name, email, password) sehingga butuh fungsi sendiri.
//
// Pola upsert-nya sama (INSERT ... SELECT ... WHERE NOT EXISTS),
// dengan kunci idempotensi di `username`.
//
// Kenapa idempotensi berbasis `username`, bukan `email`?
//   - Username adalah identitas login utama dan umumnya immutable.
//   - Email bisa diubah user di masa depan, jadi tidak cocok
//     dijadikan kunci seeder.
//   - Jika email harus unik, itu diatur oleh unique index di skema,
//     bukan oleh seeder.
//
// Catatan soal passwordHash:
//   - Parameter ini HANYA dipakai saat user benar-benar baru dibuat.
//   - Jika user sudah ada, INSERT dilewati oleh WHERE NOT EXISTS,
//     sehingga passwordHash TIDAK menimpa password user yang ada.
//   - Ini yang diinginkan: seeder tidak boleh mereset password
//     user yang mungkin sudah diganti admin.
func ensureUser(ctx context.Context, db repositories.DBTX, username, name, email, passwordHash string) (models.UUIDBinary, error) {
	const insert = `INSERT INTO users (id, username, name, email, password)
		SELECT ?, ?, ?, ?, ? FROM DUAL
		WHERE NOT EXISTS (SELECT 1 FROM users WHERE username = ?)`
	if _, err := db.ExecContext(ctx, insert,
		models.NewUUIDBinary(), username, name, email, passwordHash, username); err != nil {
		return models.UUIDBinary{}, fmt.Errorf("insert user '%s': %w", username, err)
	}

	// Sama seperti ensureByName: baca ulang ID dari username.
	var id models.UUIDBinary
	if err := db.QueryRowContext(ctx, `SELECT id FROM users WHERE username = ?`, username).Scan(&id); err != nil {
		return models.UUIDBinary{}, fmt.Errorf("baca id user '%s': %w", username, err)
	}
	return id, nil
}

// replaceRolePermissions MENGGANTI seluruh permission milik sebuah role.
//
// Input berupa NAMA permission (bukan ID), karena seeder bekerja pada
// level deklaratif: "role admin harus punya permission A, B, C".
// ID permission tidak diketahui sebelum seeder dijalankan (dan bisa
// berbeda antar environment), jadi nama adalah kunci yang stabil.
//
// Alur:
//  1. Hapus semua relasi lama role ini di role_permissions.
//  2. Insert relasi baru memakai "INSERT ... SELECT ... IN (...)",
//     sehingga ID permission diambil langsung dari tabel permissions
//     berdasarkan nama.
//
// Kenapa REPLACE, bukan append?
//   - Seeder bersifat deklaratif: kondisi akhirnya harus SAMA dengan
//     daftar di kode, berapa kali pun dijalankan.
//   - Kalau append, permission lama yang sudah dihapus dari daftar
//     akan tertinggal di database — "sampah" yang membingungkan
//     admin saat melihat halaman RBAC.
//
// Detail keamanan dan kebenaran:
//   - `placeholders` dibangun dinamis (`?,?,?`) sesuai jumlah nama,
//     karena `IN (?...)` butuh jumlah placeholder sama dengan jumlah
//     argumen.
//   - Semua nilai (roleID + nama-nama) dikirim sebagai argumen `?`,
//     tidak pernah dirangkai ke string query.
//   - `len(names) == 0` ditangani lebih awal: kalau daftar kosong,
//     cukup selesai setelah DELETE (tidak ada yang perlu di-insert).
//
// Pemeriksaan jumlah baris terpasang:
//   - Setelah INSERT, kita bandingkan `RowsAffected()` dengan
//     `len(names)`.
//   - Jika jumlahnya kurang, artinya ada nama permission di daftar
//     yang TIDAK ditemukan di tabel permissions. Ini kesalahan
//     konfigurasi seeder (typo di nama, atau lupa menambahkan
//     permission baru lewat ensurePermission).
//   - Lebih baik gagal keras di sini daripada diam-diam meninggalkan
//     role dengan permission yang tidak lengkap — yang bisa membuat
//     admin/user kehilangan akses tanpa disadari.
func replaceRolePermissions(ctx context.Context, db repositories.DBTX, roleID models.UUIDBinary, names []string) error {
	// Hapus relasi lama (dalam transaksi yang dikelola RunAllSeeders).
	if _, err := db.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id = ?`, roleID); err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}

	// Bangun "?,?,?" sesuai jumlah nama.
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(names)), ",")
	args := make([]any, 0, len(names)+1)
	args = append(args, roleID)
	for _, n := range names {
		args = append(args, n)
	}

	res, err := db.ExecContext(ctx,
		`INSERT INTO role_permissions (role_id, permission_id)
		 SELECT ?, id FROM permissions WHERE name IN (`+placeholders+`)`, args...)
	if err != nil {
		return err
	}

	// Verifikasi: semua nama permission benar-benar ada di tabel permissions.
	if n, _ := res.RowsAffected(); int(n) != len(names) {
		return fmt.Errorf("permission terpasang %d dari %d, ada nama permission yang tidak ditemukan", n, len(names))
	}
	return nil
}

// replaceUserRoles MENGGANTI seluruh role milik user (set role tunggal).
//
// Berbeda dengan replaceRolePermissions yang menerima daftar, fungsi
// ini menerima SATU roleID. Alasannya:
//   - Seeder ini hanya dipakai untuk menetapkan role default ke user
//     bawaan (mis. admin -> role admin, user biasa -> role user).
//   - Kasus "user punya banyak role" ditangani lewat endpoint API
//     (controller UpdateUserRoles), bukan di seeder.
//
// Alur:
//  1. Hapus semua relasi user_roles milik user tersebut.
//  2. Insert satu relasi baru.
//
// Kenapa DELETE dulu, bukan langsung INSERT?
//   - Jika user sudah pernah diberi role lain sebelumnya (mis. saat
//     seeder dijalankan ulang dengan user yang sudah ada), kita
//     ingin kondisi akhirnya hanya berisi role yang ditetapkan di
//     sini — sesuai semantik "replace".
//   - Insert langsung bisa menghasilkan multiple role jika unique
//     constraint tidak mencakup pasangan (user_id, role_id).
//
// Karena seeder berjalan di dalam transaksi (dikelola RunAllSeeders),
// urutan DELETE lalu INSERT ini atomik: tidak ada kondisi "user tanpa
// role" yang terlihat oleh request lain di tengah proses.
func replaceUserRoles(ctx context.Context, db repositories.DBTX, userID, roleID models.UUIDBinary) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id = ?`, userID); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)`, userID, roleID)
	return err
}