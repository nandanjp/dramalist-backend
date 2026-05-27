package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
)

// ── Provider config ────────────────────────────────────────────────────────────

type oauthProvider struct {
	authURL     string
	tokenURL    string
	userInfoURL string
	scopes      string
}

var providers = map[string]oauthProvider{
	"google": {
		authURL:     "https://accounts.google.com/o/oauth2/v2/auth",
		tokenURL:    "https://oauth2.googleapis.com/token",
		userInfoURL: "https://www.googleapis.com/oauth2/v3/userinfo",
		scopes:      "openid email profile",
	},
	"github": {
		authURL:     "https://github.com/login/oauth/authorize",
		tokenURL:    "https://github.com/login/oauth/access_token",
		userInfoURL: "https://api.github.com/user",
		scopes:      "user:email read:user",
	},
}

// ── Initiate ───────────────────────────────────────────────────────────────────

// OAuthInitiate redirects the browser to the provider's authorization URL.
// GET /auth/oauth/:provider
func (h *Handler) OAuthInitiate(c *gin.Context) {
	providerName := c.Param("provider")
	p, ok := providers[providerName]
	if !ok {
		errJSON(c, http.StatusBadRequest, "unsupported provider")
		return
	}

	b := make([]byte, 16)
	rand.Read(b)
	state := hex.EncodeToString(b)

	// Store state in Redis for CSRF verification on callback (10-minute window)
	if err := h.rdb.Set(c.Request.Context(), "oauth_state:"+state, providerName, 10*time.Minute).Err(); err != nil {
		errJSON(c, http.StatusInternalServerError, "failed to initiate OAuth")
		return
	}

	clientID, redirectURI := h.providerCredentials(providerName)
	params := url.Values{
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {p.scopes},
		"state":         {state},
	}
	if providerName == "google" {
		params.Set("access_type", "offline")
	}

	redirect(c, p.authURL+"?"+params.Encode())
}

// ── Callback ───────────────────────────────────────────────────────────────────

// OAuthCallback handles the provider redirect, exchanges the code for a profile,
// upserts the user, and issues tokens (or gates on TOTP if enabled).
// GET /auth/oauth/:provider/callback
func (h *Handler) OAuthCallback(c *gin.Context) {
	ctx := c.Request.Context()
	providerName := c.Param("provider")

	if _, ok := providers[providerName]; !ok {
		errJSON(c, http.StatusBadRequest, "unsupported provider")
		return
	}

	if errParam := c.Query("error"); errParam != "" {
		redirect(c, h.cfg.AppBaseURL+"/auth/error?reason="+url.QueryEscape(errParam))
		return
	}

	code := c.Query("code")
	state := c.Query("state")

	// Validate CSRF state
	storedProvider, err := h.rdb.Get(ctx, "oauth_state:"+state).Result()
	if err != nil || storedProvider != providerName {
		errJSON(c, http.StatusBadRequest, "invalid or expired state")
		return
	}
	h.rdb.Del(ctx, "oauth_state:"+state)

	// Exchange code → access token → user profile
	profile, err := h.fetchProfile(ctx, providerName, code)
	if err != nil {
		errJSON(c, http.StatusBadGateway, "failed to fetch OAuth profile")
		return
	}
	if profile.Email == "" {
		redirect(c, h.cfg.AppBaseURL+"/auth/error?reason=no_email")
		return
	}

	// Find or create user (linking accounts with the same email across providers)
	user, err := h.upsertUser(ctx, providerName, profile)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "user upsert failed")
		return
	}

	// Gate on TOTP if the user has it enabled
	if user.TOTPEnabled {
		b := make([]byte, 16)
		rand.Read(b)
		pendingID := hex.EncodeToString(b)
		h.rdb.Set(ctx, "pending_totp:"+pendingID, user.ID, 5*time.Minute)
		redirect(c, h.cfg.AppBaseURL+"/auth/verify-totp?pending="+pendingID)
		return
	}

	pair, err := h.issueTokenPair(ctx, user)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "token issuance failed")
		return
	}

	h.setRefreshCookie(c, pair.RefreshToken)
	redirect(c, h.cfg.AppBaseURL+"/auth/callback?status=success")
}

// ── Helpers ────────────────────────────────────────────────────────────────────

type oauthProfile struct {
	ProviderUserID string
	Email          string
	DisplayName    string
	AvatarURL      string
}

func (h *Handler) fetchProfile(ctx context.Context, providerName, code string) (oauthProfile, error) {
	if providerName == "google" {
		return h.fetchGoogleProfile(ctx, code)
	}
	return h.fetchGithubProfile(ctx, code)
}

func (h *Handler) fetchGoogleProfile(ctx context.Context, code string) (oauthProfile, error) {
	// Exchange code for access token
	data := url.Values{
		"code":          {code},
		"client_id":     {h.cfg.GoogleClientID},
		"client_secret": {h.cfg.GoogleClientSecret},
		"redirect_uri":  {h.cfg.GoogleRedirectURI},
		"grant_type":    {"authorization_code"},
	}
	resp, err := http.PostForm("https://oauth2.googleapis.com/token", data)
	if err != nil {
		return oauthProfile{}, err
	}
	defer resp.Body.Close()

	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&tok)
	if tok.Error != "" {
		return oauthProfile{}, fmt.Errorf("google token exchange: %s", tok.Error)
	}

	// Fetch user info
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://www.googleapis.com/oauth2/v3/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return oauthProfile{}, err
	}
	defer res.Body.Close()

	var user struct {
		Sub     string `json:"sub"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	json.NewDecoder(res.Body).Decode(&user)
	return oauthProfile{
		ProviderUserID: user.Sub,
		Email:          user.Email,
		DisplayName:    user.Name,
		AvatarURL:      user.Picture,
	}, nil
}

func (h *Handler) fetchGithubProfile(ctx context.Context, code string) (oauthProfile, error) {
	// Exchange code for access token
	body, _ := json.Marshal(map[string]string{
		"client_id":     h.cfg.GithubClientID,
		"client_secret": h.cfg.GithubClientSecret,
		"code":          code,
	})
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://github.com/login/oauth/access_token", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return oauthProfile{}, err
	}
	defer res.Body.Close()

	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	json.NewDecoder(res.Body).Decode(&tok)
	if tok.Error != "" {
		return oauthProfile{}, fmt.Errorf("github token exchange: %s", tok.Error)
	}

	// Fetch user profile
	doGithub := func(apiURL string, out any) error {
		req, _ := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
		req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
		req.Header.Set("Accept", "application/vnd.github.v3+json")
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return json.Unmarshal(b, out)
	}

	var user struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
		Email     string `json:"email"`
	}
	if err := doGithub("https://api.github.com/user", &user); err != nil {
		return oauthProfile{}, err
	}

	email := user.Email
	if email == "" {
		var emails []struct {
			Email   string `json:"email"`
			Primary bool   `json:"primary"`
		}
		doGithub("https://api.github.com/user/emails", &emails)
		for _, e := range emails {
			if e.Primary {
				email = e.Email
				break
			}
		}
	}

	displayName := user.Name
	if displayName == "" {
		displayName = user.Login
	}

	return oauthProfile{
		ProviderUserID: fmt.Sprintf("%d", user.ID),
		Email:          email,
		DisplayName:    displayName,
		AvatarURL:      user.AvatarURL,
	}, nil
}

// upsertUser finds an existing user by OAuth account or email (for cross-provider
// account linking), or creates a new user row.
func (h *Handler) upsertUser(ctx context.Context, providerName string, profile oauthProfile) (dbUser, error) {
	// Check for existing OAuth account link
	var userID string
	err := h.pool.QueryRow(ctx,
		"SELECT user_id::text FROM oauth_accounts WHERE provider = $1 AND provider_user_id = $2",
		providerName, profile.ProviderUserID,
	).Scan(&userID)

	if err != nil {
		// No existing OAuth link — find or create user by email
		var existingID string
		scanErr := h.pool.QueryRow(ctx,
			"SELECT id::text FROM users WHERE email = $1", profile.Email,
		).Scan(&existingID)

		if scanErr != nil {
			// New user
			if err := h.pool.QueryRow(ctx,
				"INSERT INTO users (email, display_name, avatar_url) VALUES ($1, $2, $3) RETURNING id::text",
				profile.Email, profile.DisplayName, profile.AvatarURL,
			).Scan(&userID); err != nil {
				return dbUser{}, fmt.Errorf("insert user: %w", err)
			}
		} else {
			userID = existingID
		}

		// Link OAuth account
		_, err = h.pool.Exec(ctx,
			"INSERT INTO oauth_accounts (user_id, provider, provider_user_id, provider_email) VALUES ($1, $2, $3, $4)",
			userID, providerName, profile.ProviderUserID, profile.Email,
		)
		if err != nil {
			return dbUser{}, fmt.Errorf("link oauth account: %w", err)
		}
	}

	var user dbUser
	err = h.pool.QueryRow(ctx,
		"SELECT id::text, email, display_name, totp_enabled FROM users WHERE id = $1", userID,
	).Scan(&user.ID, &user.Email, &user.DisplayName, &user.TOTPEnabled)
	return user, err
}

func (h *Handler) providerCredentials(providerName string) (clientID, redirectURI string) {
	if providerName == "google" {
		return h.cfg.GoogleClientID, h.cfg.GoogleRedirectURI
	}
	return h.cfg.GithubClientID, h.cfg.GithubRedirectURI
}
