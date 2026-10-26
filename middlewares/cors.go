package middlewares

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

func CORSMiddleware() gin.HandlerFunc {
	// Daftar origin frontend dari .env, pisahkan dengan koma.
	raw := os.Getenv("FRONTEND_ORIGINS")
	if raw == "" {
		raw = "http://localhost:5173"
	}
	allowed := map[string]bool{}
	for _, o := range strings.Split(raw, ",") {
		allowed[strings.TrimSpace(o)] = true
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if allowed[origin] {
			h := c.Writer.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
			h.Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")
			h.Add("Vary", "Origin")
		}

		// Tangani preflight request dari browser
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}