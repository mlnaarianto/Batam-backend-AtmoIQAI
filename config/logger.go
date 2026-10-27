package config

import (
	"go.uber.org/zap"
)

// Log adalah logger global aplikasi. Dipakai di seluruh package
// agar format dan level log konsisten.
var Log *zap.Logger

// InitLogger menginisialisasi Zap Logger global.
// Saat ini memakai NewDevelopment() agar log mudah dibaca ketika develop.
// Ganti ke NewProduction() saat sudah production (log JSON, level lebih ketat).
func InitLogger() {
	var err error
	Log, err = zap.NewDevelopment()
	if err != nil {
		// Gagal init logger berarti aplikasi tidak bisa mencatat error sama sekali,
		// jadi lebih aman langsung panic daripada lanjut dengan logger nil.
		panic("Gagal menginisialisasi Zap Logger: " + err.Error())
	}
}