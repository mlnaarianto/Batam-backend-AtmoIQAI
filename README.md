# 🌤️ Weather Backend API

![Go](https://img.shields.io/badge/Go-1.20%2B-00ADD8?logo=go&logoColor=white)
![MySQL](https://img.shields.io/badge/MySQL-8.0%2B-4479A1?logo=mysql&logoColor=white)
![Gin](https://img.shields.io/badge/Gin-Web%20Framework-008ECF)
![License](https://img.shields.io/badge/License-MIT-green)

RESTful API untuk manajemen stasiun cuaca, prediksi cuaca (termasuk indikator **El Niño & La Niña**), serta manajemen pengguna berbasis **Role-Based Access Control (RBAC)**. Dibangun dengan **Go** dan **MySQL** menggunakan arsitektur berlapis (*layered*) dan *repository pattern* tanpa ORM.

> 📚 Proyek ini dibuat untuk **keperluan belajar** dan dirilis di bawah [Lisensi MIT](#-lisensi).

---

## 📑 Daftar Isi

- [Fitur Utama](#-fitur-utama)
- [Tech Stack](#️-tech-stack)
- [Arsitektur & Struktur Direktori](#-arsitektur--struktur-direktori)
- [Memulai](#-memulai)
- [Dokumentasi Endpoint](#-dokumentasi-endpoint)
- [Format Response](#-format-response)
- [Catatan Keamanan & Teknis](#️-catatan-keamanan--teknis)
- [Build untuk Production](#-build-untuk-production)
- [Lisensi](#-lisensi)

---

## 🚀 Fitur Utama

- **Autentikasi & sesi aman** — JWT (access token) + refresh token via *HttpOnly cookie*, dengan **refresh token rotation** dan deteksi pencurian token.
- **RBAC dinamis** — Role & permission dikelola lewat API. Permission dicek langsung ke database (*real-time*), dengan *admin bypass* otomatis.
- **Manajemen profil** — Ubah password (otomatis mencabut semua sesi) dan unggah foto profil dengan validasi *magic bytes*.
- **Manajemen stasiun cuaca** — CRUD stasiun yang terintegrasi dengan **Nominatim (OpenStreetMap)** untuk *geocoding* dan *reverse geocoding*.
- **Prediksi cuaca** — Menyimpan prediksi dengan parameter El Niño/La Niña, *confidence score*, dan *uncertainty metric*, lengkap dengan proteksi IDOR.
- **Optimasi database** — UUID v7 (*time-ordered*) disimpan sebagai `BINARY(16)` lewat tipe kustom `UUIDBinary`: hemat storage hingga 50% dan mengurangi *index fragmentation*.
- **Response terstandar** — Semua endpoint memakai format JSON seragam (`code`, `status`, `data`).
- **Database seeder** — *Idempotent* (aman dijalankan berulang) dan transaksional.

## 🛠️ Tech Stack

| Komponen | Teknologi |
|---|---|
| Bahasa | Go 1.20+ |
| Web Framework | [Gin](https://github.com/gin-gonic/gin) |
| Database | MySQL 8.0+ (raw SQL, tanpa ORM) |
| Autentikasi | `golang-jwt/jwt/v5`, `golang.org/x/crypto/bcrypt` |
| Logging | [Uber Zap](https://github.com/uber-go/zap) |
| Konfigurasi | `joho/godotenv` |
| UUID | `google/uuid` (v7) |
| Geocoding | OpenStreetMap Nominatim |

---

## 📁 Arsitektur & Struktur Direktori

Setiap lapisan punya tanggung jawab yang jelas, sehingga kode mudah dirawat dan diuji:

```text
weather-backend/
├── config/          # Koneksi database, logger, dan environment
├── controllers/     # HTTP handler (Auth, Profile, RBAC, Station, Weather, Dashboard)
├── database/        # Skrip migrasi (SQL) dan seeder
├── helpers/         # Helper response JSON terstandar
├── middlewares/     # JWT auth, pengecekan RBAC ke DB, CORS allowlist
├── models/          # Tipe data kustom (UUIDBinary) dan struct entitas
├── repositories/    # Query SQL murni (data access layer)
├── routes/          # Definisi routing (public & protected)
├── services/        # Logika bisnis (pembuatan JWT & refresh token)
├── uploads/         # Penyimpanan file (foto profil)
├── .env             # Variabel environment (JANGAN DI-COMMIT)
├── main.go          # Entry point aplikasi
└── README.md
```

**Alur request:** `routes` → `middlewares` → `controllers` → `services` → `repositories` → MySQL

---

## ⚙️ Memulai

### 1. Prasyarat

- [Go](https://go.dev/dl/) 1.20 atau lebih baru
- MySQL Server 8.0 atau lebih baru

### 2. Clone & Install Dependency

```bash
git clone https://github.com/username/weather-backend.git
cd weather-backend
go mod tidy
```

### 3. Konfigurasi Environment

Buat file `.env` di direktori root:

```env
# Server
PORT=8080
FRONTEND_ORIGINS=http://localhost:5173,http://localhost:3000
COOKIE_SECURE=false          # Ubah ke true jika sudah memakai HTTPS

# Database
DB_USER=root
DB_PASSWORD=password_mysql_anda
DB_HOST=127.0.0.1
DB_PORT=3306
DB_NAME=weather_db

# Security (WAJIB minimal 32 karakter)
JWT_SECRET=buat_secret_random_minimal_32_karakter_disini

# Seeder (true = isi data awal saat aplikasi dijalankan)
RUN_SEEDER=true
```

### 4. Migrasi Database

Buat database `weather_db`, lalu jalankan file `.up.sql` di `database/migrations/` secara berurutan. Anda bisa memakai [golang-migrate](https://github.com/golang-migrate/migrate) atau menjalankannya manual.

```bash
mysql -u root -p -e "CREATE DATABASE weather_db CHARACTER SET utf8mb4;"
```

### 5. Seeder (Opsional)

Jika `RUN_SEEDER=true`, aplikasi akan mengisi data awal secara otomatis saat pertama kali dijalankan:

| Data | Isi |
|---|---|
| Role | `admin`, `user` |
| Permission | 8 permission untuk admin, 2 untuk user |
| Super Admin | `superadmin` / `123` |
| User biasa | `warga_batam` / `123` |

> ⚠️ **Penting:** Ganti password default ini sebelum deploy ke production, dan set `RUN_SEEDER=false`.

### 6. Jalankan Server

```bash
go run main.go
```

Server berjalan di `http://localhost:8080`. Cek kesehatannya dengan `GET /ping`.

---

## 🔗 Dokumentasi Endpoint

### 🌐 Public (tanpa autentikasi)

| Method | Endpoint | Deskripsi |
|---|---|---|
| GET | `/ping` | Health check |
| POST | `/login` | Login; mengembalikan access token + refresh token (cookie) |
| POST | `/refresh` | Perbarui access token (dengan rotasi refresh token) |
| POST | `/logout` | Cabut sesi (hapus dari DB & cookie) |
| GET | `/uploads/profile/...` | Static file foto profil |
| GET | `/api/public/weather-predictions` | List prediksi publik |
| GET | `/api/public/stations` | List stasiun publik |

### 🔒 Protected (`/api`, header `Authorization: Bearer <access_token>`)

**Dasar & profil** — cukup login

| Method | Endpoint | Deskripsi |
|---|---|---|
| GET | `/api/dashboard` | Pesan selamat datang (tes login) |
| GET | `/api/me` | Info user yang sedang login |
| GET | `/api/profile` | Detail profil |
| PUT | `/api/profile/password` | Ubah password (otomatis mencabut semua sesi) |
| POST | `/api/profile/photo` | Unggah foto (maks 2 MB, JPG/PNG/WebP) |
| DELETE | `/api/profile/photo` | Hapus foto profil |

**Dashboard & RBAC** — permission `view-weather`

| Method | Endpoint | Deskripsi |
|---|---|---|
| GET | `/api/dashboard-stats` | Statistik total stasiun & prediksi |
| GET | `/api/rbac` | Seluruh data role, permission, dan user |

**Manajemen role**

| Method | Endpoint | Permission | Deskripsi |
|---|---|---|---|
| PUT | `/api/users/:id/roles` | `manage-user-roles` | Timpa role milik user |
| POST | `/api/roles` | `manage-roles` | Buat role baru |
| PUT | `/api/roles/:id` | `manage-roles` | Ubah nama role |
| DELETE | `/api/roles/:id` | `manage-roles` | Hapus role |
| PUT | `/api/roles/:id/permissions` | `manage-role-permissions` | Timpa permission milik role |

**Stasiun cuaca** — permission `manage-stations`

| Method | Endpoint | Deskripsi |
|---|---|---|
| GET | `/api/stations` | List stasiun |
| GET | `/api/stations/:id` | Detail stasiun |
| GET | `/api/stations/geocode?q=...` | Cari lokasi (OSM) |
| GET | `/api/stations/reverse-geocode?lat=...&lon=...` | Reverse geocoding (OSM) |
| POST | `/api/stations` | Tambah stasiun |
| PUT | `/api/stations/:id` | Perbarui stasiun |
| DELETE | `/api/stations/:id` | Hapus stasiun |

**Prediksi cuaca**

| Method | Endpoint | Permission | Deskripsi |
|---|---|---|---|
| GET | `/api/weather-predictions` | Login | List prediksi (otomatis difilter proteksi IDOR) |
| GET | `/api/weather-predictions/:id` | `weather-predictions:view-id` | Detail prediksi |
| POST | `/api/weather-predictions` | `manage-weather-predictions` | Tambah prediksi |
| PUT | `/api/weather-predictions/:id` | `manage-weather-predictions` | Perbarui prediksi |
| DELETE | `/api/weather-predictions/:id` | `manage-weather-predictions` | Hapus prediksi |

---

## 📦 Format Response

Seluruh endpoint mengembalikan struktur JSON yang konsisten:

```json
{
  "code": 200,
  "status": "OK",
  "data": {
    "message": "Login berhasil!",
    "token": "eyJhbGciOi...",
    "expires_at": 1735000000,
    "username": "superadmin",
    "name": "Super Admin BMKG",
    "role": "admin",
    "photo_url": null
  }
}
```

---

## 🛡️ Catatan Keamanan & Teknis

### Autentikasi & Token

- **JWT ketat** — Hanya menerima algoritma `HS256`; `JWT_SECRET` wajib minimal 32 karakter.
- **TTL token** — Access token berlaku 15 menit, refresh token 8 jam.
- **Token acak yang aman** — Refresh token dibuat dengan `crypto/rand` (256-bit) dan di-encode `base64.RawURLEncoding` agar aman untuk cookie.
- **Token hashing** — Refresh token tidak disimpan mentah; hanya hash SHA-256 yang masuk database, sehingga kebocoran DB tidak langsung bisa dipakai untuk login.
- **Deteksi pencurian token** — Jika refresh token yang sudah dicabut dipakai ulang, seluruh sesi dalam *token family* tersebut dicabut.
- **Pencabutan 3 level** — `Revoke` (satu token), `RevokeFamily` (satu sesi), `RevokeAllByUser` (semua perangkat).

### Keamanan Aplikasi

- **CORS allowlist** — Hanya origin di `FRONTEND_ORIGINS` yang diizinkan (mendukung credentials).
- **Keamanan upload** — Validasi lewat *magic bytes* (`http.DetectContentType`), bukan nama file atau header `Content-Type`; nama file diganti UUID.
- **Proteksi IDOR** — User tanpa permission *view-all* hanya dapat melihat data miliknya, walaupun menebak UUID.
- **Trusted proxies** — `SetTrustedProxies(nil)` untuk mencegah IP spoofing.
- **Transaction locking** — `FOR UPDATE` pada refresh token dan update role/user untuk mencegah *race condition*.
- **Ubah password** — Jika password lama salah, endpoint mengembalikan `400 Bad Request` (bukan `401`) agar interceptor frontend tidak memicu refresh token secara tidak sengaja.

### Database & Repository

- **`UUIDBinary`** — Tipe kustom (`sql.Scanner` + `driver.Valuer`), disimpan sebagai `BINARY(16)` dan diserialisasi sebagai string UUID di JSON.
- **UUID v7** — Berurutan berdasarkan waktu sehingga insert di InnoDB lebih efisien.
- **Timezone UTC** — Semua koneksi memakai `loc=UTC` agar `expired_at` dan `revoked_at` konsisten.
- **Interface `DBTX`** — Repository dapat berjalan di `*sql.DB` maupun `*sql.Tx` tanpa duplikasi kode.
- **Two-query pattern** — Menghindari N+1 query saat memuat relasi (role → permission, user → role).

---

## 📦 Build untuk Production

```bash
# Compile menjadi satu binary
go build -o app main.go

# Jalankan
./app
```

Checklist sebelum deploy:

- [ ] Ganti password akun seed, atau set `RUN_SEEDER=false`
- [ ] Gunakan `JWT_SECRET` acak yang panjang
- [ ] Set `COOKIE_SECURE=true` dan jalankan di belakang HTTPS
- [ ] Batasi `FRONTEND_ORIGINS` hanya ke domain frontend Anda
- [ ] Pastikan `.env` tidak masuk ke repository (`.gitignore`)

---

## 📝 Lisensi

Proyek ini dirilis di bawah **[MIT License](LICENSE)**. Anda bebas menggunakan, menyalin, memodifikasi, menggabungkan, menerbitkan, mendistribusikan, dan menjual salinan perangkat lunak ini, selama menyertakan pemberitahuan hak cipta dan lisensi pada salinan yang dibagikan.

Perangkat lunak disediakan **"apa adanya"**, tanpa jaminan apa pun. Proyek ini dibuat untuk pembelajaran.

Buat file `LICENSE` di root repository dengan isi berikut:

```text
MIT License

Copyright (c) 2026 Nama Anda

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## 👨‍💻 Author

Dibuat dengan untuk pembelajaran dan edukasi.