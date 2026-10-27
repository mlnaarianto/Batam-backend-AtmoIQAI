package repositories

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"weather-backend/models"
)

// RefreshToken merepresentasikan satu baris di tabel personal_access_tokens
// dengan type = 'refresh'.
//
// Kolom-kolom penting:
//
//   - ID       : identifier unik token (bukan token itu sendiri).
//   - UserID   : pemilik token.
//   - FamilyID : ID "keluarga" token. Semua token hasil rotasi dari SATU
//     login berbagi FamilyID yang sama. Konsep ini dipakai untuk
//     mendeteksi & menangani penyalahgunaan (lihat RevokeFamily).
//   - TokenHash: hash SHA256 dari token mentah. Token mentah TIDAK
//     pernah disimpan di database — hanya hash-nya. Jika database
//     bocor, penyerang tidak bisa langsung memakai isi tabel untuk
//     login.
//   - UserAgent, IPAddress: metadata untuk audit/forensik
//     (mis. "apakah token ini dipakai dari perangkat berbeda?").
//   - ExpiredAt: kapan token kedaluwarsa.
//   - RevokedAt: kapan token dicabut (NULL = masih aktif).
//     Memakai sql.NullTime karena kolomnya bisa NULL.
type RefreshToken struct {
	ID        models.UUIDBinary
	UserID    models.UUIDBinary
	FamilyID  models.UUIDBinary
	TokenHash string
	UserAgent string
	IPAddress string
	ExpiredAt time.Time
	RevokedAt sql.NullTime
}

// TokenRepository menangani seluruh query terkait refresh token
// (create, lookup, revoke, cleanup).
type TokenRepository struct{ db DBTX }

func NewTokenRepository(db DBTX) *TokenRepository { return &TokenRepository{db: db} }

// Create menyimpan satu refresh token baru.
//
// Detail penting:
//
//  1. `type` di-hardcode 'refresh' di query. Tabel personal_access_tokens
//     juga dipakai untuk tipe token lain (mis. access token jika nanti
//     dipakai skema blacklist). Memfilter berdasarkan type penting agar
//     lookup tidak salah ambil baris tipe lain.
//
//  2. UserAgent di-`truncate` ke 255 karakter dan IPAddress ke 45
//     karakter, sesuai panjang kolom di database. Tanpa truncate,
//     user agent yang sangat panjang akan menyebabkan error
//     "Data too long for column" — bukan error yang ingin kita
//     tampilkan ke user, dan bukan penyebab login gagal yang wajar.
//
//     Kenapa 45 untuk IP? 45 adalah panjang maksimum representasi
//     IPv6 dalam bentuk string (termasuk IPv4-mapped IPv6).
//
//  3. ExpiredAt disimpan dalam UTC. Ini penting karena:
//     - Waktu mungkin dibandingkan dengan time.Now().UTC() saat
//     refresh (lihat controller).
//     - Menghindari perbedaan zona waktu antar server (mis. server
//     di WIB vs DB di UTC).
//
//  4. Token yang dikirim (t.TokenHash) HARUS sudah berupa hash —
//     hashing dilakukan di layer service/controller sebelum masuk
//     sini. Repository hanya menyimpan.
func (r *TokenRepository) Create(ctx context.Context, t *RefreshToken) error {
	const q = `INSERT INTO personal_access_tokens
	  (id, user_id, family_id, token_hash, type, user_agent, ip_address, expired_at)
	  VALUES (?, ?, ?, ?, 'refresh', ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q,
		t.ID, t.UserID, t.FamilyID, t.TokenHash,
		truncate(t.UserAgent, 255), truncate(t.IPAddress, 45),
		t.ExpiredAt.UTC(),
	)
	return err
}

// FindByHash mencari refresh token berdasarkan hash-nya.
//
// Dipakai untuk:
//   - Logout: mencari family ID token yang sedang dipakai, lalu
//     mencabut seluruh family tersebut.
//   - Cek status token (mis. di luar alur refresh).
//
// Klien hanya mengirim token MENTAH (dari cookie). Hash dibuat di
// service, lalu diteruskan ke sini. Dengan begini, query WHERE selalu
// memakai hash — token mentah tidak pernah muncul di log query.
func (r *TokenRepository) FindByHash(ctx context.Context, hash string) (*RefreshToken, error) {
	return r.find(ctx, `SELECT id, user_id, family_id, token_hash, expired_at, revoked_at
	                    FROM personal_access_tokens WHERE token_hash = ? AND type = 'refresh'`, hash)
}

// FindByHashForUpdate sama seperti FindByHash, TAPI memakai `FOR UPDATE`.
//
// `FOR UPDATE` mengunci baris sampai transaksi selesai. Ini penting
// pada alur refresh token karena:
//
//   - Mencegah "double refresh": jika dua request refresh datang
//     bersamaan dengan token yang sama, tanpa lock keduanya bisa
//     sama-sama lolos validasi lalu masing-masing membuat token baru.
//     Hasilnya: dua token baru dengan FamilyID sama, salah satu
//     menjadi "yatim" dan berpotensi dianggap reuse.
//   - Dengan lock, request kedua akan menunggu request pertama
//     commit. Setelah commit, status token lama sudah revoked, jadi
//     request kedua akan masuk ke cabang "reuse detection" (lihat
//     controller Refresh).
//
// Syarat: repository HARUS dibuat dengan *sql.Tx (bukan *sql.DB),
// karena di autocommit mode lock langsung dilepas setelah query.
func (r *TokenRepository) FindByHashForUpdate(ctx context.Context, hash string) (*RefreshToken, error) {
	return r.find(ctx, `SELECT id, user_id, family_id, token_hash, expired_at, revoked_at
	                    FROM personal_access_tokens WHERE token_hash = ? AND type = 'refresh' FOR UPDATE`, hash)
}

// find adalah helper internal untuk FindByHash dan FindByHashForUpdate.
// Mengembalikan ErrNotFound (bukan sql.ErrNoRows) agar controller cukup
// memeriksa error sentinel aplikasi.
func (r *TokenRepository) find(ctx context.Context, query string, hash string) (*RefreshToken, error) {
	var t RefreshToken
	err := r.db.QueryRowContext(ctx, query, hash).
		Scan(&t.ID, &t.UserID, &t.FamilyID, &t.TokenHash, &t.ExpiredAt, &t.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Revoke mencabut SATU token dengan men-set revoked_at.
//
// Kondisi `revoked_at IS NULL` di WHERE memastikan:
//   - Jika token sudah dicabut sebelumnya, UPDATE tidak mengubah apa pun.
//     Ini penting agar timestamp revoked_at TIDAK tertimpa (jejak audit
//     tetap akurat: kapan pertama kali token ini dicabut).
//   - Idempoten: memanggil Revoke dua kali tidak menimbulkan error.
//
// Dipakai pada refresh token rotation: setelah token lama dipakai untuk
// minta token baru, token lama DICABUT (bukan dihapus) agar jika
// muncul lagi nanti, kita bisa mendeteksi "reuse" (indikasi pencurian).
func (r *TokenRepository) Revoke(ctx context.Context, id models.UUIDBinary) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE personal_access_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		time.Now().UTC(), id)
	return err
}

// RevokeFamily mencabut SEMUA token dalam satu family.
//
// Konsep "family":
//   - Satu login menghasilkan satu FamilyID baru.
//   - Setiap rotasi (refresh) menghasilkan token baru dengan FamilyID
//     yang SAMA. Jadi satu family = satu "rantai" sesi login dari
//     perangkat yang sama.
//
// Kapan dipakai:
//
//  1. REUSE DETECTION (paling penting):
//     Jika refresh token yang dikirim ternyata SUDAH dicabut, itu
//     indikasi kuat token tersebut dicuri (penyerang memakai token
//     yang sudah dirotasi). Dalam kasus ini kita cabut SELURUH family,
//     sehingga:
//     - Penyerang tidak bisa lanjut memakai token curian.
//     - Pemilik sah terpaksa login ulang.
//     Ini lebih aman daripada hanya menolak satu token.
//
//  2. LOGOUT:
//     Saat user logout dari satu perangkat, cabut family token
//     tersebut (bukan seluruh token user) agar perangkat lain tetap
//     login.
//
// `revoked_at IS NULL` di WHERE: sama seperti Revoke, melindungi
// nilai revoked_at yang sudah ada agar tidak tertimpa.
func (r *TokenRepository) RevokeFamily(ctx context.Context, familyID models.UUIDBinary) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE personal_access_tokens SET revoked_at = ? WHERE family_id = ? AND revoked_at IS NULL`,
		time.Now().UTC(), familyID)
	return err
}

// RevokeAllByUser mencabut SEMUA refresh token aktif milik user,
// di semua perangkat.
//
// Kapan dipakai:
//   - Setelah ganti password: untuk "memaksa logout" semua sesi lain
//     yang mungkin dibuat dengan password lama. Ini praktik standar
//     keamanan.
//   - Setelah admin mereset password user: semua sesi lama user
//     tersebut harus mati, supaya hanya password sementara yang
//     berlaku dan user dipaksa menggantinya.
//   - Skenario "logout dari semua perangkat" jika nanti ditambahkan.
//
// Filter `type = 'refresh'` penting: jika nanti tabel
// personal_access_tokens juga menyimpan tipe token lain, kita tidak
// ingin ikut mencabutnya di sini.
func (r *TokenRepository) RevokeAllByUser(ctx context.Context, userID models.UUIDBinary) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE personal_access_tokens SET revoked_at = ? WHERE user_id = ? AND type = 'refresh' AND revoked_at IS NULL`,
		time.Now().UTC(), userID)
	return err
}

// DeleteExpired menghapus permanen token yang sudah kedaluwarsa.
//
// Kenapa perlu dihapus, bukan dibiarkan saja?
//   - Tabel token cenderung tumbuh cepat (setiap login + refresh
//     menambah baris).
//   - Token yang sudah expired tidak berguna untuk audit jangka
//     panjang — kalau perlu audit, biasanya disimpan terpisah.
//
// Kenapa hanya token EXPIRED yang dihapus, bukan revoked?
//   - Token yang di-REVOKE (mis. karena reuse detection) sengaja
//     DIPERTAHANKAN selama masih dalam masa expired-nya, karena
//     keberadaannya dibutuhkan untuk mendeteksi REUSE berikutnya.
//   - Jika langsung dihapus, permintaan refresh dengan token yang
//     sudah dicabut akan berakhir sebagai "tidak ditemukan" —
//     kehilangan sinyal penting untuk mendeteksi pencurian.
//
// Method ini biasanya dipanggil oleh job/cron terjadwal, bukan
// pada request user. Return `int64` (jumlah baris dihapus) berguna
// untuk logging job.
func (r *TokenRepository) DeleteExpired(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM personal_access_tokens WHERE expired_at < ?`, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// truncate memotong string agar panjangnya tidak melebihi n byte.
//
// Kenapa perlu? Kolom user_agent dan ip_address memiliki batas panjang
// di database (255 dan 45). User-Agent dari client bisa sangat panjang
// (atau sengaja dibuat panjang untuk menyerang), dan jika tidak
// dipotong akan:
//   - Menyebabkan error "Data too long for column" saat INSERT.
//   - Membuat proses login gagal padahal kredensial valid.
//
// Catatan: pemotongan per-byte, bukan per-rune. Untuk User-Agent yang
// isinya ASCII ini aman. Jika suatu saat dipakai untuk input unicode
// dan pemotongan di tengah multi-byte rune, hasilnya bisa rusak — tapi
// untuk keperluan kolom ini, cukup.
func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
