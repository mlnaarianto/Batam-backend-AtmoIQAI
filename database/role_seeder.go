package database

import (
	"context"
	"fmt"

	"weather-backend/repositories"
)

// SeedRoles memastikan dua role sistem ("admin" dan "user") ada di
// tabel roles.
//
// Kenapa perlu seeder terpisah untuk roles, padahal SeedPermissions
// juga sudah memanggil ensureRole?
//
//   - Pemisahan tanggung jawab (separation of concerns):
//     SeedRoles fokus pada "role apa saja yang wajib ada", sedangkan
//     SeedPermissions fokus pada "permission apa yang dimiliki tiap
//     role". Dengan begitu:
//       * Kalau di masa depan hanya butuh menyiapkan role tanpa
//         menyentuh permission, cukup panggil SeedRoles.
//       * Perubahan pada daftar permission tidak memengaruhi logika
//         pembuatan role, dan sebaliknya.
//   - Urutan di RunAllSeeders biasanya: SeedRoles dulu, baru
//     SeedPermissions. Meski SeedPermissions juga idempoten dan
//     akan membuat role yang sama jika belum ada, punya seeder
//     terpisah membuat alur "siapkan role → siapkan permission"
//     eksplisit dan mudah dibaca.
//
// Sifat seeder ini: IDEMPOTEN.
// ensureRole hanya akan melakukan INSERT jika role dengan nama
// tersebut belum ada. Kalau sudah ada, fungsi mengembalikan ID-nya
// dan tidak melakukan perubahan apa pun. Jadi seeder ini aman
// dipanggil berulang kali (mis. setiap restart aplikasi dengan
// RUN_SEEDER=true).
//
// Kenapa dua role ini yang dipilih (dan bukan role lain)?
//   - "admin": dibutuhkan oleh query HasPermission untuk bypass
//     otorisasi (admin otomatis dianggap punya semua permission).
//     Tanpa role ini, tidak ada user yang bisa diberi akses penuh
//     lewat mekanisme standar.
//   - "user" : role default untuk user biasa (prinsip least
//     privilege). Dipakai saat assign role ke user baru atau saat
//     ResetUserRoles di seeder lain.
//
// Role tambahan (mis. "operator", "viewer") TIDAK dibuat di sini;
// role seperti itu sebaiknya dibuat lewat UI RBAC sesuai kebutuhan
// organisasi. Seeder hanya menyiapkan fondasi minimal yang
// diandalkan aplikasi.
func SeedRoles(ctx context.Context, db repositories.DBTX) error {
	// Loop hanya untuk dua role sistem. Jika di masa depan ada role
	// sistem lain yang wajib ada, tinggal tambahkan di slice ini.
	//
	// Catatan: ID hasil ensureRole tidak dipakai di sini. Untuk
	// SeedRoles, tujuannya murni "pastikan ada"; pemakaian ID
	// (mis. untuk replace role_permissions) dilakukan oleh seeder
	// lain seperti SeedPermissions. Mengabaikan ID di sini juga
	// membuat kode lebih ringkas — tidak ada variabel yang tidak
	// perlu.
	for _, name := range []string{"admin", "user"} {
		if _, err := ensureRole(ctx, db, name); err != nil {
			return err
		}
	}

	// Seeder berjalan dalam transaksi (dikelola RunAllSeeders).
	// Pesan di bawah hanya akan tercetak jika seluruh langkah sukses
	// dan transaksi di-commit. Jika ada error, seeder akan di-rollback
	// dan pemanggil menerima error tersebut, bukan pesan sukses ini.
	fmt.Println("Seeder Roles berhasil dijalankan!")
	return nil
}