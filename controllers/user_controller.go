package controllers

import (
	"errors"
	"net/http"
	"strings"

	"weather-backend/config"
	"weather-backend/helpers"
	"weather-backend/models"
	"weather-backend/repositories"

	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// defaultUserRole adalah role yang diberikan ke user baru buatan admin.
// Role ini dibuat oleh seeder (SeedPermissions).
const defaultUserRole = "user"

// mysqlDuplicateEntry adalah nomor error MySQL untuk pelanggaran
// UNIQUE (username atau email sudah dipakai).
const mysqlDuplicateEntry = 1062

// CreateUserInput adalah payload untuk membuat user baru oleh admin.
// Password di sini bersifat SEMENTARA: user wajib menggantinya saat
// login pertama (must_change_password = 1).
type CreateUserInput struct {
	Username string `json:"username" binding:"required,min=3,max=50"`
	Name     string `json:"name" binding:"required,max=100"`
	Email    string `json:"email" binding:"required,email,max=255"`
	Password string `json:"password" binding:"required,min=8"`
}

// ResetUserPasswordInput adalah payload reset password oleh admin.
type ResetUserPasswordInput struct {
	NewPassword string `json:"new_password" binding:"required,min=8"`
}

// ListUsers mengembalikan daftar semua user beserta role-nya.
// Dilindungi permission "manage-users" di routes.
func ListUsers(c *gin.Context) {
	users, err := repositories.NewUserRepository(config.SQLDB).List(c.Request.Context())
	if err != nil {
		config.Log.Error("Gagal membaca daftar user", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Terjadi kesalahan server"})
		return
	}

	// Dipetakan manual agar format ID konsisten (string UUID) dan
	// tidak bergantung pada cara UUIDBinary di-serialize ke JSON.
	out := make([]gin.H, 0, len(users))
	for _, u := range users {
		out = append(out, gin.H{
			"id":                   uuid.UUID(u.ID).String(),
			"username":             u.Username,
			"name":                 u.Name,
			"email":                u.Email,
			"must_change_password": u.MustChangePassword,
			"roles":                u.Roles,
			"created_at":           u.CreatedAt,
		})
	}
	helpers.ResponseOK(c, gin.H{"users": out})
}

// CreateUser membuat user baru oleh admin.
//
// Alur:
//  1. Validasi input (termasuk batas 72 byte bcrypt).
//  2. Hash password sementara.
//  3. Dalam SATU transaksi: insert user (must_change_password = 1),
//     lalu beri role default "user". Jika salah satu gagal, tidak ada
//     yang tersimpan (tidak ada user tanpa role).
//  4. Username/email duplikat dibalas 409.
func CreateUser(c *gin.Context) {
	var input CreateUserInput
	if err := c.ShouldBindJSON(&input); err != nil {
		helpers.ResponseBadRequest(c, gin.H{"error": "Input tidak valid: username min 3 karakter, email harus valid, password min 8 karakter"})
		return
	}
	if len(input.Password) > 72 {
		helpers.ResponseBadRequest(c, gin.H{"error": "Password maksimal 72 karakter"})
		return
	}

	input.Username = strings.TrimSpace(input.Username)
	input.Name = strings.TrimSpace(input.Name)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if len(input.Username) < 3 || input.Name == "" {
		helpers.ResponseBadRequest(c, gin.H{"error": "Username dan nama tidak boleh kosong"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		config.Log.Error("Gagal hash password user baru", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Terjadi kesalahan server"})
		return
	}

	ctx := c.Request.Context()
	tx, err := config.SQLDB.BeginTx(ctx, nil)
	if err != nil {
		config.Log.Error("Gagal memulai transaksi buat user", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Terjadi kesalahan server"})
		return
	}
	defer tx.Rollback() // no-op jika sudah Commit.

	id := models.UUIDBinary(uuid.New())
	repo := repositories.NewUserRepository(tx)

	err = repo.Create(ctx, &repositories.NewUser{
		ID:           id,
		Username:     input.Username,
		Name:         input.Name,
		Email:        input.Email,
		PasswordHash: string(hash),
	})
	if err != nil {
		var me *mysql.MySQLError
		if errors.As(err, &me) && me.Number == mysqlDuplicateEntry {
			c.JSON(http.StatusConflict, gin.H{"error": "Username atau email sudah dipakai"})
			return
		}
		config.Log.Error("Gagal membuat user", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal membuat user"})
		return
	}

	if err := repo.AssignRole(ctx, id, defaultUserRole); err != nil {
		config.Log.Error("Gagal memberi role default", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal membuat user"})
		return
	}

	if err := tx.Commit(); err != nil {
		config.Log.Error("Gagal commit buat user", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal membuat user"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "User berhasil dibuat. User wajib mengganti password saat login pertama.",
		"user": gin.H{
			"id":                   uuid.UUID(id).String(),
			"username":             input.Username,
			"name":                 input.Name,
			"email":                input.Email,
			"must_change_password": true,
			"roles":                []string{defaultUserRole},
		},
	})
}

// ResetUserPassword mereset password user lain oleh admin.
//
// Alur:
//  1. Ambil ID target dari URL (:id), tolak jika bukan UUID valid.
//  2. Tolak reset akun sendiri: admin harus memakai ChangePassword,
//     supaya tidak mengunci/mem-logout dirinya sendiri secara tak sengaja.
//  3. Dalam SATU transaksi: set password baru + must_change_password = 1,
//     lalu cabut SEMUA refresh token target agar sesi lamanya mati.
func ResetUserPassword(c *gin.Context) {
	adminID, ok := currentUserID(c)
	if !ok {
		helpers.ResponseUnauthorized(c, gin.H{"error": "Sesi tidak valid"})
		return
	}

	parsed, err := uuid.Parse(c.Param("id"))
	if err != nil {
		helpers.ResponseBadRequest(c, gin.H{"error": "ID user tidak valid"})
		return
	}
	targetID := models.UUIDBinary(parsed)

	if targetID == adminID {
		helpers.ResponseBadRequest(c, gin.H{"error": "Gunakan fitur ganti password untuk akun sendiri"})
		return
	}

	var input ResetUserPasswordInput
	if err := c.ShouldBindJSON(&input); err != nil {
		helpers.ResponseBadRequest(c, gin.H{"error": "Input tidak valid: password baru minimal 8 karakter"})
		return
	}
	if len(input.NewPassword) > 72 {
		helpers.ResponseBadRequest(c, gin.H{"error": "Password maksimal 72 karakter"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		config.Log.Error("Gagal hash password reset", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Terjadi kesalahan server"})
		return
	}

	ctx := c.Request.Context()
	tx, err := config.SQLDB.BeginTx(ctx, nil)
	if err != nil {
		config.Log.Error("Gagal memulai transaksi reset password", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Terjadi kesalahan server"})
		return
	}
	defer tx.Rollback() // no-op jika sudah Commit.

	err = repositories.NewUserRepository(tx).ResetPassword(ctx, targetID, string(hash))
	if errors.Is(err, repositories.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "User tidak ditemukan"})
		return
	}
	if err != nil {
		config.Log.Error("Gagal reset password", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal mereset password"})
		return
	}

	if err := repositories.NewTokenRepository(tx).RevokeAllByUser(ctx, targetID); err != nil {
		config.Log.Error("Gagal mencabut sesi setelah reset password", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal mereset password"})
		return
	}

	if err := tx.Commit(); err != nil {
		config.Log.Error("Gagal commit reset password", zap.Error(err))
		helpers.ResponseInternalError(c, gin.H{"error": "Gagal mereset password"})
		return
	}

	helpers.ResponseOK(c, gin.H{"message": "Password direset. User wajib menggantinya saat login berikutnya."})
}