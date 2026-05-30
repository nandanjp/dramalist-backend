package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"

	"dramalist/auth-service/secret"
)

// TOTPSetup generates a new TOTP secret for the requesting user, stores it
// temporarily in Redis, and returns the otpauth:// URI for QR code rendering.
// The secret is not persisted until TOTPConfirm succeeds.
// GET /auth/totp/setup
func (h *Handler) TOTPSetup(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var totpEnabled bool
	var email string
	if err := h.pool.QueryRow(c.Request.Context(),
		"SELECT email, totp_enabled FROM users WHERE id = $1", userID,
	).Scan(&email, &totpEnabled); err != nil {
		errJSON(c, http.StatusNotFound, "user not found")
		return
	}
	if totpEnabled {
		errJSON(c, http.StatusConflict, "TOTP already enabled")
		return
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Dramalist",
		AccountName: email,
	})
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "failed to generate TOTP secret")
		return
	}

	// Store plaintext secret temporarily — saved encrypted only after user confirms
	h.rdb.Set(c.Request.Context(), "totp_setup:"+userID, key.Secret(), 10*time.Minute)

	c.JSON(http.StatusOK, gin.H{
		"otpauth_uri": key.URL(),
		"secret":      key.Secret(), // shown once for manual entry
	})
}

// TOTPConfirm verifies the first TOTP code to confirm setup, encrypts and
// persists the secret, then generates 8 single-use backup codes.
// POST /auth/totp/confirm
func (h *Handler) TOTPConfirm(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errJSON(c, http.StatusBadRequest, "code required")
		return
	}

	tempSecret, err := h.rdb.Get(ctx, "totp_setup:"+userID).Result()
	if err != nil {
		errJSON(c, http.StatusBadRequest, "no pending TOTP setup — call /auth/totp/setup first")
		return
	}

	if !totp.Validate(body.Code, tempSecret) {
		errJSON(c, http.StatusBadRequest, "invalid code")
		return
	}

	// Encrypt secret before persisting
	encrypted, err := secret.Encrypt(tempSecret, h.cfg.TOTPEncryptionKey)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "failed to encrypt secret")
		return
	}

	_, err = h.pool.Exec(ctx,
		"UPDATE users SET totp_secret = $1, totp_enabled = true, updated_at = NOW() WHERE id = $2",
		encrypted, userID,
	)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "failed to save TOTP secret")
		return
	}
	h.rdb.Del(ctx, "totp_setup:"+userID)

	// Generate 8 single-use backup codes
	backupCodes := make([]string, 8)
	for i := range backupCodes {
		b := make([]byte, 5)
		rand.Read(b)
		backupCodes[i] = hex.EncodeToString(b)

		hash, _ := bcrypt.GenerateFromPassword([]byte(backupCodes[i]), 12)
		h.pool.Exec(ctx,
			"INSERT INTO totp_backup_codes (user_id, code_hash) VALUES ($1, $2)",
			userID, string(hash),
		)
	}

	c.JSON(http.StatusOK, gin.H{
		"backup_codes": backupCodes,
		"message":      "TOTP enabled — save backup codes securely, they cannot be retrieved again",
	})
}

// TOTPVerify completes MFA login initiated during OAuthCallback. The frontend
// sends the pending_id (stored in Redis) and the TOTP code or a backup code.
// POST /auth/totp/verify
func (h *Handler) TOTPVerify(c *gin.Context) {
	ctx := c.Request.Context()

	var body struct {
		PendingID  string `json:"pending_id" binding:"required"`
		Code       string `json:"code"`
		BackupCode string `json:"backup_code"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		errJSON(c, http.StatusBadRequest, "pending_id required")
		return
	}
	if body.Code == "" && body.BackupCode == "" {
		errJSON(c, http.StatusBadRequest, "code or backup_code required")
		return
	}

	// Validate pending session
	userID, err := h.rdb.Get(ctx, "pending_totp:"+body.PendingID).Result()
	if err != nil {
		errJSON(c, http.StatusUnauthorized, "invalid or expired session — restart login")
		return
	}
	h.rdb.Del(ctx, "pending_totp:"+body.PendingID)

	var encryptedSecret string
	var user dbUser
	if err := h.pool.QueryRow(ctx,
		"SELECT id::text, email, display_name, is_admin, totp_secret FROM users WHERE id = $1", userID,
	).Scan(&user.ID, &user.Email, &user.DisplayName, &user.IsAdmin, &encryptedSecret); err != nil {
		errJSON(c, http.StatusUnauthorized, "user not found")
		return
	}
	user.TOTPEnabled = true

	totpSecret, err := secret.Decrypt(encryptedSecret, h.cfg.TOTPEncryptionKey)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "failed to load TOTP secret")
		return
	}

	verified := false

	if body.Code != "" {
		verified = totp.Validate(body.Code, totpSecret)
	} else {
		// Try each unused backup code
		rows, _ := h.pool.Query(ctx,
			"SELECT id::text, code_hash FROM totp_backup_codes WHERE user_id = $1 AND used = false", userID,
		)
		defer rows.Close()
		for rows.Next() {
			var id, hash string
			rows.Scan(&id, &hash)
			if bcrypt.CompareHashAndPassword([]byte(hash), []byte(body.BackupCode)) == nil {
				h.pool.Exec(ctx,
					"UPDATE totp_backup_codes SET used = true, used_at = NOW() WHERE id = $1", id,
				)
				verified = true
				break
			}
		}
	}

	if !verified {
		errJSON(c, http.StatusUnauthorized, "invalid code")
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
