package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// JWTIdentity is a middleware that extracts X-User-Id and X-User-Role from a
// Bearer JWT when the gateway has not already injected those headers.
// This allows services to be called directly (e.g. in smoke tests) without a
// gateway, while remaining compatible with the production nginx gateway path.
func JWTIdentity(secret string) gin.HandlerFunc {
	if secret == "" {
		return func(c *gin.Context) { c.Next() }
	}
	key := []byte(secret)
	return func(c *gin.Context) {
		if c.GetHeader("X-User-Id") != "" {
			c.Next()
			return
		}
		auth := c.GetHeader("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			c.Next()
			return
		}
		raw := strings.TrimPrefix(auth, "Bearer ")
		token, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return key, nil
		})
		if err != nil || !token.Valid {
			c.Next()
			return
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.Next()
			return
		}
		if userID, _ := claims["userId"].(string); userID != "" {
			c.Request.Header.Set("X-User-Id", userID)
		}
		if role, _ := claims["role"].(string); role != "" {
			c.Request.Header.Set("X-User-Role", role)
		}
		c.Next()
	}
}
