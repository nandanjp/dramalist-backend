package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

// slugPattern: 3-30 chars, lowercase alphanum + hyphens, no leading/trailing hyphens.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,28}[a-z0-9]$`)

// ── Response types ────────────────────────────────────────────────────────────

type profileResponse struct {
	ID          string               `json:"id"`
	Email       string               `json:"email"`
	DisplayName string               `json:"display_name"`
	AvatarURL   *string              `json:"avatar_url"`
	Bio         *string              `json:"bio"`
	IsPublic    bool                 `json:"is_public"`
	ProfileSlug *string              `json:"profile_slug"`
	Preferences *preferencesResponse `json:"preferences,omitempty"`
}

type preferencesResponse struct {
	DefaultSort         string   `json:"default_sort"`
	DefaultStatusFilter []string `json:"default_status_filter"`
	DefaultGenreFilter  []string `json:"default_genre_filter"`
	UITheme             string   `json:"ui_theme"`
}

type statsResponse struct {
	TotalWatched   int            `json:"total_watched"`
	TotalEpisodes  int            `json:"total_episodes"`
	AvgRating      *float64       `json:"avg_rating"`
	GenreBreakdown map[string]int `json:"genre_breakdown"`
}

// ── Request types ─────────────────────────────────────────────────────────────

type patchMeRequest struct {
	DisplayName *string      `json:"display_name"`
	AvatarURL   *string      `json:"avatar_url"`
	Bio         *string      `json:"bio"`
	IsPublic    *bool        `json:"is_public"`
	ProfileSlug *string      `json:"profile_slug"`
	Preferences *patchPrefs  `json:"preferences"`
}

type patchPrefs struct {
	DefaultSort         *string   `json:"default_sort"`
	DefaultStatusFilter *[]string `json:"default_status_filter"`
	DefaultGenreFilter  *[]string `json:"default_genre_filter"`
	UITheme             *string   `json:"ui_theme"`
}

// ── GET /users/me ─────────────────────────────────────────────────────────────

func (h *Handler) GetMe(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "missing user identity")
		return
	}

	ctx := c.Request.Context()

	// Lazy-upsert profile on first call using gateway-injected identity headers.
	email := c.GetHeader("X-User-Email")
	displayName := c.GetHeader("X-User-Display-Name")
	if displayName == "" {
		displayName = email // fallback: use email as display name
	}

	_, err := h.pool.Exec(ctx,
		`INSERT INTO profiles (id, email, display_name)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (id) DO NOTHING`,
		userID, email, displayName,
	)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "profile init failed")
		return
	}

	_, err = h.pool.Exec(ctx,
		"INSERT INTO preferences (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING",
		userID,
	)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "preferences init failed")
		return
	}

	h.respondWithProfile(c, userID)
}

// ── PATCH /users/me ───────────────────────────────────────────────────────────

func (h *Handler) PatchMe(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "missing user identity")
		return
	}

	var req patchMeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx := c.Request.Context()

	// Build profile UPDATE
	profileArgs := []any{userID}
	profileCols := []string{}
	n := 2

	if req.DisplayName != nil {
		profileCols = append(profileCols, fmt.Sprintf("display_name = $%d", n))
		profileArgs = append(profileArgs, *req.DisplayName)
		n++
	}
	if req.AvatarURL != nil {
		profileCols = append(profileCols, fmt.Sprintf("avatar_url = $%d", n))
		profileArgs = append(profileArgs, *req.AvatarURL)
		n++
	}
	if req.Bio != nil {
		profileCols = append(profileCols, fmt.Sprintf("bio = $%d", n))
		profileArgs = append(profileArgs, *req.Bio)
		n++
	}
	if req.IsPublic != nil {
		profileCols = append(profileCols, fmt.Sprintf("is_public = $%d", n))
		profileArgs = append(profileArgs, *req.IsPublic)
		n++
	}
	if req.ProfileSlug != nil {
		if !slugPattern.MatchString(*req.ProfileSlug) {
			errJSON(c, http.StatusBadRequest, "profile_slug must be 3-30 lowercase alphanumeric chars or hyphens, no leading/trailing hyphens")
			return
		}
		profileCols = append(profileCols, fmt.Sprintf("profile_slug = $%d", n))
		profileArgs = append(profileArgs, *req.ProfileSlug)
		n++
	}

	if len(profileCols) > 0 {
		profileCols = append(profileCols, "updated_at = NOW()")
		query := "UPDATE profiles SET " + strings.Join(profileCols, ", ") + " WHERE id = $1"
		if _, err := h.pool.Exec(ctx, query, profileArgs...); err != nil {
			if strings.Contains(err.Error(), "unique") {
				errJSON(c, http.StatusConflict, "profile_slug already taken")
				return
			}
			errJSON(c, http.StatusInternalServerError, "profile update failed")
			return
		}
	}

	// Build preferences UPDATE
	if req.Preferences != nil {
		p := req.Preferences
		prefArgs := []any{userID}
		prefCols := []string{}
		n = 2

		if p.DefaultSort != nil {
			prefCols = append(prefCols, fmt.Sprintf("default_sort = $%d", n))
			prefArgs = append(prefArgs, *p.DefaultSort)
			n++
		}
		if p.DefaultStatusFilter != nil {
			prefCols = append(prefCols, fmt.Sprintf("default_status_filter = $%d", n))
			prefArgs = append(prefArgs, *p.DefaultStatusFilter)
			n++
		}
		if p.DefaultGenreFilter != nil {
			prefCols = append(prefCols, fmt.Sprintf("default_genre_filter = $%d", n))
			prefArgs = append(prefArgs, *p.DefaultGenreFilter)
			n++
		}
		if p.UITheme != nil {
			prefCols = append(prefCols, fmt.Sprintf("ui_theme = $%d", n))
			prefArgs = append(prefArgs, *p.UITheme)
			n++
		}

		if len(prefCols) > 0 {
			prefCols = append(prefCols, "updated_at = NOW()")
			query := "UPDATE preferences SET " + strings.Join(prefCols, ", ") + " WHERE user_id = $1"
			if _, err := h.pool.Exec(ctx, query, prefArgs...); err != nil {
				errJSON(c, http.StatusInternalServerError, "preferences update failed")
				return
			}
		}
	}

	h.respondWithProfile(c, userID)
}

// ── GET /users/me/stats ───────────────────────────────────────────────────────

func (h *Handler) GetMyStats(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "missing user identity")
		return
	}

	ctx := c.Request.Context()

	var stats statsResponse
	var genreBytes []byte

	err := h.pool.QueryRow(ctx,
		"SELECT total_watched, total_episodes, avg_rating, genre_breakdown FROM watch_stats WHERE user_id = $1",
		userID,
	).Scan(&stats.TotalWatched, &stats.TotalEpisodes, &stats.AvgRating, &genreBytes)

	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusOK, statsResponse{GenreBreakdown: map[string]int{}})
		return
	}
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "stats fetch failed")
		return
	}

	stats.GenreBreakdown = make(map[string]int)
	json.Unmarshal(genreBytes, &stats.GenreBreakdown) //nolint:errcheck — safe default

	c.JSON(http.StatusOK, stats)
}

// ── GET /users/:slug ──────────────────────────────────────────────────────────

func (h *Handler) GetBySlug(c *gin.Context) {
	slug := c.Param("slug")
	ctx := c.Request.Context()

	var p profileResponse
	err := h.pool.QueryRow(ctx,
		`SELECT id::text, email, display_name, avatar_url, bio, is_public, profile_slug
		 FROM profiles WHERE profile_slug = $1`,
		slug,
	).Scan(&p.ID, &p.Email, &p.DisplayName, &p.AvatarURL, &p.Bio, &p.IsPublic, &p.ProfileSlug)

	if errors.Is(err, pgx.ErrNoRows) {
		errJSON(c, http.StatusNotFound, "profile not found")
		return
	}
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "profile fetch failed")
		return
	}

	if !p.IsPublic {
		errJSON(c, http.StatusNotFound, "profile not found")
		return
	}

	c.JSON(http.StatusOK, p)
}

// ── Shared helper ─────────────────────────────────────────────────────────────

func (h *Handler) respondWithProfile(c *gin.Context, userID string) {
	ctx := c.Request.Context()

	var p profileResponse
	err := h.pool.QueryRow(ctx,
		`SELECT id::text, email, display_name, avatar_url, bio, is_public, profile_slug
		 FROM profiles WHERE id = $1`,
		userID,
	).Scan(&p.ID, &p.Email, &p.DisplayName, &p.AvatarURL, &p.Bio, &p.IsPublic, &p.ProfileSlug)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "profile fetch failed")
		return
	}

	var prefs preferencesResponse
	var statusFilter, genreFilter []string

	err = h.pool.QueryRow(ctx,
		`SELECT default_sort,
		        COALESCE(default_status_filter, '{}'),
		        COALESCE(default_genre_filter, '{}'),
		        ui_theme
		 FROM preferences WHERE user_id = $1`,
		userID,
	).Scan(&prefs.DefaultSort, &statusFilter, &genreFilter, &prefs.UITheme)
	if err == nil {
		if statusFilter == nil {
			statusFilter = []string{}
		}
		if genreFilter == nil {
			genreFilter = []string{}
		}
		prefs.DefaultStatusFilter = statusFilter
		prefs.DefaultGenreFilter = genreFilter
		p.Preferences = &prefs
	}

	c.JSON(http.StatusOK, p)
}
