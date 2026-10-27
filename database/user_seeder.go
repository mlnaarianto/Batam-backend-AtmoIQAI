package database

import (
	"context"
	"fmt"

	"weather-backend/repositories"

	"golang.org/x/crypto/bcrypt"
)

// SeedUsers menyiapkan dua user bawaan aplikasi:
//
//   - superadmin  -> role "admin"
//   - warga_batam -> role "user"
//
// Tujuan seeder ini adalah menyediakan akun awal supaya aplikasi bisa
// langsung dipakai setelah setup database, tanpa harus insert user
// manual lewat SQL. Ini juga memastikan user admin pertama selalu ada,
// sehingga halaman RBAC bisa diakses untuk mengelola user/permission
// selanjutnya.
//
// Sifat seeder: IDEMPOTEN.
// Karena ensureUser hanya meng-INSERT jika username belum ada, dan
// replaceUserRoles menimpa relasi role lama, seeder ini aman dipanggil
// berulang kali. Kondisi akhirnya selalu sama:
//
//   - superadmin  : role admin
//   - warga_batam : role user
//
// Seeder ini berjalan di dalam SATU transaksi (dikelola RunAllSeeders),
// jadi jika salah satu langkah gagal, tidak ada perubahan yang
// tersimpan — tidak akan ada user yang sudah dibuat tapi role-nya
// belum terhubung (atau sebaliknya).
func SeedUsers(ctx context.Context, db repositories.DBTX) error {
	// Password default "123" — HANYA untuk pengembangan/testing.
	//
	// PERINGATAN KEAMANAN (penting untuk dipahami):
	//   - Password ini sengaja sangat lemah agar mudah dipakai di
	//     lingkungan lokal.
	//   - Sebelum deploy ke production, password ini WAJIB diganti,
	//     atau seeder ini di-nonaktifkan (RUN_SEEDER=false) dan akun
	//     dibuat lewat proses lain yang lebih aman.
	//   - Satu-satunya mitigasi yang ada saat ini: seeder tidak
	//     berjalan otomatis; ia hanya aktif jika RUN_SEEDER=true.
	//
	// Kenapa tetap di-hash bcrypt meski lemah?
	//   - Aplikasi selalu membandingkan password dengan bcrypt.CompareHash.
	//     Kalau password "123" disimpan plaintext, login akan gagal.
	//   - Hashing di sini memastikan data seeder mengikuti kontrak data
	//     yang sama dengan user yang didaftarkan lewat aplikasi.
	//
	// Catatan: hash ini HANYA dipakai saat user benar-benar baru dibuat.
	// ensureUser tidak menimpa password user yang sudah ada, sehingga
	// kalau admin sudah mengganti password superadmin, seeder tidak akan
	// meresetnya kembali ke "123".
	hash, err := bcrypt.GenerateFromPassword([]byte("123"), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password seeder: %w", err)
	}
	hashed := string(hash)

	// Buat (atau ambil ID) user superadmin.
	// Dipanggil dengan role "admin" dan data profil wajar agar
	// tampilan UI langsung terisi.
	adminUserID, err := ensureUser(ctx, db, "superadmin", "Super Admin BMKG", "superadmin@gmail.com", hashed)
	if err != nil {
		return err
	}

	// Buat (atau ambil ID) user biasa sebagai contoh peran non-admin.
	// Nama "warga_batam" dipilih karena proyek ini terkait cuaca
	// wilayah Batam, tapi ini murni konvensi — bisa diganti sesuai
	// domain aplikasi.
	regularUserID, err := ensureUser(ctx, db, "warga_batam", "Warga Kota Batam", "warga.batam@gmail.com", hashed)
	if err != nil {
		return err
	}

	// Ambil ID role "admin" dan "user".
	//
	// Kenapa memanggil ensureRole di sini, padahal SeedRoles sudah
	// dipanggil sebelum SeedUsers di RunAllSeeders?
	//   - Ini adalah DEFENSIVE PROGRAMMING:
	//       * SeedUsers bisa dipanggil secara mandiri (mis. di test
	//         atau skenario manual) tanpa harus menjalankan SeedRoles
	//         terlebih dahulu.
	//       * ensureRole bersifat idempoten — kalau role sudah ada,
	//         hanya mengembalikan ID tanpa perubahan.
	//       * Biaya query-nya kecil (satu SELECT), manfaatnya besar
	//         dalam hal ketahanan kode terhadap urutan pemanggilan.
	adminRoleID, err := ensureRole(ctx, db, "admin")
	if err != nil {
		return err
	}
	userRoleID, err := ensureRole(ctx, db, "user")
	if err != nil {
		return err
	}

	// Hubungkan tiap user ke role-nya.
	//
	// replaceUserRoles menghapus semua relasi user_roles lama, lalu
	// memasang SATU relasi baru. Ini memastikan kondisi akhirnya
	// deklaratif: superadmin SELALU hanya punya role admin, dan
	// warga_batam SELALU hanya punya role user.
	//
	// Jika di masa depan user ini ingin diberi role TAMBAHAN (mis.
	// "operator"), jangan diubah di seeder — lakukan lewat endpoint
	// API UpdateUserRoles. Seeder sengaja mengunci kondisi default
	// agar perilakunya mudah diprediksi.
	//
	// Error dibungkus dengan fmt.Errorf("%w", ...) supaya:
	//   - Pesan asli tetap bisa diperiksa dengan errors.Is/As.
	//   - Log jelas menunjukkan langkah mana yang gagal ("hubungkan
	//     role admin ke superadmin" vs "hubungkan role user ke
	//     warga_batam").
	if err := replaceUserRoles(ctx, db, adminUserID, adminRoleID); err != nil {
		return fmt.Errorf("hubungkan role admin ke superadmin: %w", err)
	}
	if err := replaceUserRoles(ctx, db, regularUserID, userRoleID); err != nil {
		return fmt.Errorf("hubungkan role user ke warga_batam: %w", err)
	}

	// Pesan sukses hanya tercetak jika seluruh langkah di atas berhasil
	// dan transaksi di-commit oleh RunAllSeeders. Jika ada error, seeder
	// akan di-rollback dan pemanggil menerima error tersebut.
	fmt.Println("Seeder Users & User_Roles berhasil dijalankan!")
	return nil
}