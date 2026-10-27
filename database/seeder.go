package database

import (
	"context"
	"log"
	"time"

	"weather-backend/config"
)

// RunAllSeeders menjalankan seluruh seeder database dalam SATU transaksi.
//
// Tujuan utama: menjamin sifat atomik. Jika salah satu seeder gagal
// (mis. typo nama permission, constraint violation, koneksi terputus),
// seluruh perubahan di-rollback. Artinya database TIDAK akan pernah
// berada di kondisi "setengah ter-seed":
//   - Tidak ada role yang sudah dibuat tapi permission-nya belum diset.
//   - Tidak ada user admin yang sudah ada tapi role-nya belum diberikan.
//
// Kondisi setengah jadi seperti itu berbahaya karena aplikasi bisa
// dijalankan dengan RBAC yang tidak konsisten — mis. user admin tidak
// bisa mengakses apa pun, atau role dengan permission parsial.
//
// Kenapa semua seeder harus menerima `*sql.Tx`, bukan `*sql.DB`?
//   - Itu sebabnya setiap fungsi seeder (SeedRoles, SeedPermissions,
//     SeedUsers) bertipe parameter `repositories.DBTX`, bukan
//     `*sql.DB`. `*sql.Tx` memenuhi interface DBTX, sehingga semua
//     query di dalam seeder otomatis ikut transaksi ini.
//   - Kalau salah satu seeder diubah untuk memakai config.SQLDB
//     langsung, sifat atomik ini akan hilang tanpa peringatan.
//
// Seeder ini dipanggil dari main() hanya jika environment variable
// RUN_SEEDER=true, agar seeding TIDAK berjalan otomatis di production
// setiap kali aplikasi restart. Seeding adalah operasi sekali-jalan /
// manual, bukan bagian dari startup normal.
func RunAllSeeders() error {
	// Context dengan timeout mencegah seeding menggantung selamanya
	// jika database lambat atau ada lock yang tidak terlepas.
	//
	// Kenapa 30 detik?
	//   - Cukup panjang untuk seeding normal (jumlah role/permission
	//     kecil, user bawaan sedikit).
	//   - Cukup pendek untuk tidak menahan startup aplikasi terlalu
	//     lama jika database bermasalah.
	//   - Jika seeder tumbuh besar dan waktu ini tidak memadai,
	//     timeout bisa dinaikkan — TAPI ingat, timeout juga membatasi
	//     durasi transaksi (transaksi panjang = lock lebih lama).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Mulai transaksi. Semua seeder di bawah akan berjalan di dalam
	// transaksi ini, bukan di koneksi terpisah.
	tx, err := config.SQLDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// `defer tx.Rollback()` adalah jaring pengaman:
	//   - Jika fungsi kembali lebih awal karena error (return err),
	//     Rollback dijalankan dan membatalkan semua perubahan.
	//   - Jika fungsi sampai di tx.Commit(), Rollback TIDAK berefek
	//     apa pun (no-op) — perilaku standar database/sql.
	// Pola "defer Rollback + Commit eksplisit" ini adalah idiom umum
	// Go untuk transaksi agar tidak ada jalan keluar tanpa rollback.
	defer tx.Rollback()

	// Urutan seeder penting:
	//
	//  1. SeedRoles:
	//     Membuat role "admin" dan "user". Harus paling awal, karena
	//     seeder berikutnya (SeedPermissions, SeedUsers) membutuhkan
	//     role-role ini untuk mengisi relasi (role_permissions,
	//     user_roles).
	//
	//  2. SeedPermissions:
	//     Membuat permission dan menghubungkannya ke role. Membutuhkan
	//     role dari langkah 1.
	//
	//  3. SeedUsers:
	//     Membuat user bawaan (mis. admin) dan menetapkan role-nya.
	//     Membutuhkan role dari langkah 1.
	//
	// Meskipun SeedPermissions juga bisa memanggil ensureRole sendiri
	// (dan memang begitu untuk alasan keamanan), urutan eksplisit ini
	// membuat niat kode lebih jelas: role dulu, baru permission, baru
	// user.
	if err := SeedRoles(ctx, tx); err != nil {
		return err
	}
	if err := SeedPermissions(ctx, tx); err != nil {
		return err
	}
	if err := SeedUsers(ctx, tx); err != nil {
		return err
	}

	// Commit hanya dipanggil jika ketiga seeder sukses. Jika salah
	// satu gagal, `defer tx.Rollback()` akan membatalkan semua
	// perubahan — database kembali ke kondisi sebelum seeder
	// dijalankan.
	//
	// Jika Commit sendiri gagal (mis. koneksi putus tepat saat commit),
	// kita juga mengembalikan error. Database/sql menjamin Rollback
	// setelah Commit gagal tidak akan merusak apa pun (no-op atau
	// error yang bisa diabaikan).
	if err := tx.Commit(); err != nil {
		return err
	}

	// Log sukses hanya muncul jika commit benar-benar berhasil.
	// Ini pesan yang aman diandalkan untuk memastikan seeding selesai.
	log.Println("Seluruh database seeder berhasil dijalankan!")
	return nil
}