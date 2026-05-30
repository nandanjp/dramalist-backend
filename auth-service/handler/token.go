package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RefreshToken exchanges a valid refresh token (from cookie or body) for a new
// access token + rotated refresh token. Single-use: old token is revoked on use.
// POST /auth/token/refresh
func (h *Handler) RefreshToken(c *gin.Context) {
	ctx := c.Request.Context()

	// Accept refresh token from HttpOnly cookie (preferred) or request body
	refreshToken, err := c.Cookie("drl_refresh")
	if err != nil {
		var body struct {
			RefreshToken string `json:"refresh_token"`
		}
		if bindErr := c.ShouldBindJSON(&body); bindErr != nil || body.RefreshToken == "" {
			errJSON(c, http.StatusUnauthorized, "refresh token required")
			return
		}
		refreshToken = body.RefreshToken
	}

	// Validate against Redis (fast path)
	userID, err := h.rdb.Get(ctx, "refresh:"+refreshToken).Result()
	if err != nil {
		errJSON(c, http.StatusUnauthorized, "invalid or expired refresh token")
		return
	}

	// Revoke old token (rotation — single-use enforcement)
	h.rdb.Del(ctx, "refresh:"+refreshToken)
	h.pool.Exec(ctx, "UPDATE refresh_tokens SET revoked = true WHERE token = $1", refreshToken)

	// Load user
	var user dbUser
	if err := h.pool.QueryRow(ctx,
		"SELECT id::text, email, display_name, totp_enabled, is_admin FROM users WHERE id = $1", userID,
	).Scan(&user.ID, &user.Email, &user.DisplayName, &user.TOTPEnabled, &user.IsAdmin); err != nil {
		errJSON(c, http.StatusUnauthorized, "user not found")
		return
	}

	pair, err := h.issueTokenPair(ctx, user)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "token issuance failed")
		return
	}

	h.setRefreshCookie(c, pair.RefreshToken)
	c.JSON(http.StatusOK, gin.H{
		"access_token": pair.AccessToken,
		"token_type":   "Bearer",
		"expires_in":   h.cfg.AccessTokenTTL,
	})
}

// Logout revokes the refresh token and clears the cookie.
// POST /auth/logout
func (h *Handler) Logout(c *gin.Context) {
	refreshToken, err := c.Cookie("drl_refresh")
	if err == nil && refreshToken != "" {
		h.rdb.Del(c.Request.Context(), "refresh:"+refreshToken)
		h.pool.Exec(c.Request.Context(),
			"UPDATE refresh_tokens SET revoked = true WHERE token = $1", refreshToken)
	}
	c.SetCookie("drl_refresh", "", -1, "/", "", h.cfg.IsProduction(), true)
	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

// LogoutAll revokes every non-expired refresh token for the authenticated user,
// invalidating all sessions across all devices. The current access JWT stays
// valid until its 15-min TTL elapses — accepted tradeoff (no token blacklist).
// POST /auth/logout-all
func (h *Handler) LogoutAll(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	rows, err := h.pool.Query(ctx,
		"SELECT token FROM refresh_tokens WHERE user_id = $1 AND revoked = false AND expires_at > NOW()",
		userID,
	)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var tokens []string
	for rows.Next() {
		var tok string
		if err := rows.Scan(&tok); err == nil {
			tokens = append(tokens, tok)
		}
	}

	// Revoke in Redis and Postgres
	for _, tok := range tokens {
		h.rdb.Del(ctx, "refresh:"+tok)
	}
	h.pool.Exec(ctx,
		"UPDATE refresh_tokens SET revoked = true WHERE user_id = $1 AND revoked = false",
		userID,
	)

	c.SetCookie("drl_refresh", "", -1, "/", "", h.cfg.IsProduction(), true)
	c.JSON(http.StatusOK, gin.H{"message": "all sessions revoked", "sessions_revoked": len(tokens)})
}
