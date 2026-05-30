package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type emailRegisterRequest struct {
	Email       string `json:"email"        binding:"required,email"`
	DisplayName string `json:"display_name" binding:"required,min=2,max=80"`
	Password    string `json:"password"     binding:"required,min=8"`
}

type emailLoginRequest struct {
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// EmailRegister creates a new account with email + password and issues tokens.
// POST /auth/register
func (h *Handler) EmailRegister(c *gin.Context) {
	var req emailRegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, err.Error())
		return
	}

	ctx := c.Request.Context()

	var existing string
	if err := h.pool.QueryRow(ctx, "SELECT id::text FROM users WHERE email = $1", req.Email).Scan(&existing); err == nil {
		errJSON(c, http.StatusConflict, "email already registered")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "failed to hash password")
		return
	}

	var userID string
	if err := h.pool.QueryRow(ctx,
		"INSERT INTO users (email, display_name, password_hash) VALUES ($1, $2, $3) RETURNING id::text",
		req.Email, req.DisplayName, string(hash),
	).Scan(&userID); err != nil {
		errJSON(c, http.StatusInternalServerError, "failed to create user")
		return
	}

	pair, err := h.issueTokenPair(ctx, dbUser{ID: userID, Email: req.Email, DisplayName: req.DisplayName})
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "token issuance failed")
		return
	}

	h.setRefreshCookie(c, pair.RefreshToken)
	c.JSON(http.StatusCreated, gin.H{
		"access_token": pair.AccessToken,
		"expires_in":   h.cfg.AccessTokenTTL,
	})
}

// EmailLogin authenticates with email + password and issues tokens.
// Returns require_totp + pending_id if MFA is enabled.
// POST /auth/login
func (h *Handler) EmailLogin(c *gin.Context) {
	var req emailLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, err.Error())
		return
	}

	ctx := c.Request.Context()

	var user dbUser
	var hash string
	if err := h.pool.QueryRow(ctx,
		`SELECT id::text, email, display_name, totp_enabled, is_admin, COALESCE(password_hash, '')
		 FROM users WHERE email = $1`,
		req.Email,
	).Scan(&user.ID, &user.Email, &user.DisplayName, &user.TOTPEnabled, &user.IsAdmin, &hash); err != nil || hash == "" {
		errJSON(c, http.StatusUnauthorized, "invalid email or password")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		errJSON(c, http.StatusUnauthorized, "invalid email or password")
		return
	}

	if user.TOTPEnabled {
		b := make([]byte, 16)
		rand.Read(b)
		pendingID := hex.EncodeToString(b)
		h.rdb.Set(ctx, "pending_totp:"+pendingID, user.ID, 5*time.Minute)
		c.JSON(http.StatusOK, gin.H{"require_totp": true, "pending_id": pendingID})
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
		"expires_in":   h.cfg.AccessTokenTTL,
	})
}
