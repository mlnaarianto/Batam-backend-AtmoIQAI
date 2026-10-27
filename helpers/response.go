package helpers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// WriteJSON adalah helper utama untuk mengirim response JSON dengan format
// yang konsisten: { code, status, data }. Semua helper di bawah memakai ini.
func WriteJSON(c *gin.Context, code int, status string, data any) {
	c.JSON(code, gin.H{
		"code":   code,
		"status": status,
		"data":   data,
	})
}

// ResponseOK mengirim response sukses dengan HTTP 200.
func ResponseOK(c *gin.Context, data any) {
	WriteJSON(c, http.StatusOK, "OK", data)
}

// ResponseCreated mengirim response sukses pembuatan resource dengan HTTP 201.
func ResponseCreated(c *gin.Context, data any) {
	WriteJSON(c, http.StatusCreated, "Created", data)
}

// ResponseBadRequest mengirim response error validasi/input dengan HTTP 400.
func ResponseBadRequest(c *gin.Context, data any) {
	WriteJSON(c, http.StatusBadRequest, "Bad Request", data)
}

// ResponseUnauthorized mengirim response error autentikasi dengan HTTP 401.
func ResponseUnauthorized(c *gin.Context, data any) {
	WriteJSON(c, http.StatusUnauthorized, "Unauthorized", data)
}

// ResponseForbidden mengirim response error otorisasi dengan HTTP 403.
func ResponseForbidden(c *gin.Context, data any) {
	WriteJSON(c, http.StatusForbidden, "Forbidden", data)
}

// ResponseNotFound mengirim response error data tidak ditemukan dengan HTTP 404.
func ResponseNotFound(c *gin.Context, data any) {
	WriteJSON(c, http.StatusNotFound, "Not Found", data)
}

// ResponseInternalError mengirim response error server dengan HTTP 500.
func ResponseInternalError(c *gin.Context, data any) {
	WriteJSON(c, http.StatusInternalServerError, "Internal Server Error", data)
}