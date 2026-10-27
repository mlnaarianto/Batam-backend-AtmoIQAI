package database

import (
	"context"
	"fmt"

	"weather-backend/repositories"
)

// SeedPermissions menyiapkan data dasar RBAC (Role-Based Access Control):
// daftar permission, dua role bawaan (admin & user), dan pemetaan
// permission ke masing-masing role.
//
// Seeder ini bersifat IDEMPOTEN lewat kombinasi:
//   - ensurePermission / ensureRole: insert hanya jika belum ada
//     (aman dijalankan berulang).
//   - replaceRolePermissions: menghapus relasi lama lalu menulis ulang
//     relasi sesuai daftar di sini.
//
// Efeknya: setiap kali seeder dijalankan, kondisi akhir RBAC akan
// SELALU sama dengan yang didefinisikan di file ini — tidak peduli
// berapa kali dipanggil. Ini penting karena seeder juga dipakai untuk
// "menyetel ulang" permission bawaan kalau ada perubahan daftar di
// versi kode berikutnya.
//
// Seeder ini berjalan di dalam SATU transaksi (lihat RunAllSeeders),
// jadi jika salah satu langkah gagal, tidak ada perubahan yang
// tersimpan — RBAC tidak akan berada di kondisi setengah jadi.
func SeedPermissions(ctx context.Context, db repositories.DBTX) error {
	// Daftar permission untuk role "admin".
	//
	// Kenapa admin tetap diberi daftar permission eksplisit, padahal
	// di query HasPermission admin sudah di-bypass (`r.name = 'admin'`
	// otomatis dianggap punya semua izin)?
	//
	//   - Bypass di query bersifat "efektif di runtime", sementara data
	//     di role_permissions dipakai untuk halaman RBAC di frontend
	//     (menampilkan centang/checklist "permission apa saja yang
	//     dimiliki admin").
	//   - Jika daftar ini kosong, UI RBAC akan menampilkan admin
	//     "tidak punya permission apa pun", yang membingungkan user.
	//   - Menyimpan daftar eksplisit juga memudahkan audit: "admin
	//     memang dirancang punya akses ke X, Y, Z".
	adminPermissions := []string{
		"manage-stations",
		"weather-predictions:view-all",
		"weather-predictions:view-id",
		"manage-weather-predictions",
		"view-weather",
		"manage-roles",
		"manage-role-permissions",
		"manage-user-roles",
	}

	// Daftar permission untuk role "user" (bawaan).
	//
	// Sengaja MINIMAL: hanya izin baca yang aman untuk semua user.
	// Permission lain (mengelola station, mengubah prediksi, mengatur
	// role) sengaja TIDAK diberikan. Admin bisa menambahkannya nanti
	// lewat halaman RBAC.
	//
	// Prinsip "least privilege": user biasa hanya mendapat apa yang
	// memang dibutuhkan untuk memakai fitur dasar aplikasi.
	userPermissions := []string{
		"view-weather",
		"weather-predictions:view-id",
	}

	// 1. Pastikan semua permission di adminPermissions ada di tabel
	//    permissions.
	//
	//    Kenapa hanya adminPermissions yang di-loop di sini?
	//    Karena userPermissions adalah subset dari adminPermissions
	//    (semua entry-nya sudah ada di daftar admin). Jadi cukup
	//    memastikan daftar yang lebih besar; yang lebih kecil otomatis
	//    ikut tersedia.
	//
	//    Kalau nanti userPermissions berisi permission yang TIDAK ada
	//    di adminPermissions, WAJIB ditambahkan juga ke loop ini —
	//    kalau tidak, replaceRolePermissions di bawah akan mengabaikan
	//    nama tersebut (permission tidak dikenal).
	for _, name := range adminPermissions {
		if _, err := ensurePermission(ctx, db, name); err != nil {
			return err
		}
	}

	// 2. Pastikan role "admin" dan "user" ada.
	//
	//    ensureRole mengembalikan ID role (baik yang sudah ada maupun
	//    yang baru dibuat). Kita perlu ID-nya untuk langkah
	//    replaceRolePermissions di bawah.
	//
	//    Role ini adalah role SISTEM yang diandalkan aplikasi:
	//      - "admin" dipakai oleh bypass di HasPermission.
	//      - "user"  dipakai sebagai role default untuk user baru.
	//    Jadi seeder WAJIB memastikan keduanya ada.
	adminRoleID, err := ensureRole(ctx, db, "admin")
	if err != nil {
		return err
	}
	userRoleID, err := ensureRole(ctx, db, "user")
	if err != nil {
		return err
	}

	// 3. Setel ulang permission untuk tiap role.
	//
	//    replaceRolePermissions melakukan pola "replace":
	//    hapus semua relasi lama, lalu insert relasi baru.
	//
	//    Kenapa REPLACE, bukan "tambah yang belum ada"?
	//    - Tujuannya agar kondisi akhir selalu sesuai definisi di
	//      file ini (idempoten & deklaratif).
	//    - Kalau hanya menambah, permission yang sudah tidak relevan
	//      (mis. dihapus dari daftar di versi kode baru) akan tetap
	//      tertinggal sebagai "sisa" di database.
	//    - Replace memastikan role benar-benar hanya punya permission
	//      yang tercantum di sini.
	//
	//    Error dibungkus dengan fmt.Errorf("%w", ...) agar:
	//      - Pesan asli tetap bisa diperiksa dengan errors.Is/As
	//        oleh pemanggil.
	//      - Saat error muncul di log, jelas langkah mana yang gagal
	//        ("set permission admin" vs "set permission user").
	if err := replaceRolePermissions(ctx, db, adminRoleID, adminPermissions); err != nil {
		return fmt.Errorf("set permission admin: %w", err)
	}
	if err := replaceRolePermissions(ctx, db, userRoleID, userPermissions); err != nil {
		return fmt.Errorf("set permission user: %w", err)
	}

	fmt.Println("Seeder Permissions & Role_Permissions berhasil dijalankan!")
	return nil
}