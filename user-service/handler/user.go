package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"dramalist/user-service/db"
)

// slugPattern: 3-30 chars, lowercase alphanum + hyphens, no leading/trailing hyphens.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,28}[a-z0-9]$`)

const (
	profileCacheTTL = 2 * time.Minute
	slugCacheTTL    = 5 * time.Minute
)

// ── Response types ────────────────────────────────────────────────────────────

type profileResponse struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	DisplayName string  `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
	Bio         *string `json:"bio"`
	IsPublic    bool    `json:"is_public"`
	ProfileSlug *string `json:"profile_slug"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type preferencesResponse struct {
	DefaultSort         string   `json:"default_sort"`
	DefaultStatusFilter []string `json:"default_status_filter"`
	DefaultGenreFilter  []string `json:"default_genre_filter"`
	UITheme             string   `json:"ui_theme"`
}

type meResponse struct {
	Profile     profileResponse      `json:"profile"`
	Preferences *preferencesResponse `json:"preferences"`
	WatchStats  any                  `json:"watch_stats"`
}

type statsResponse struct {
	TotalWatched   int            `json:"total_watched"`
	TotalEpisodes  int            `json:"total_episodes"`
	AvgRating      *float64       `json:"avg_rating"`
	GenreBreakdown map[string]int `json:"genre_breakdown"`
}

// ── Request types ─────────────────────────────────────────────────────────────

type patchMeRequest struct {
	DisplayName *string     `json:"display_name"`
	AvatarURL   *string     `json:"avatar_url"`
	Bio         *string     `json:"bio"`
	IsPublic    *bool       `json:"is_public"`
	ProfileSlug *string     `json:"profile_slug"`
	Preferences *patchPrefs `json:"preferences"`
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
		displayName = email
	}

	if err := h.store.UpsertProfile(ctx, userID, email, displayName); err != nil {
		errJSON(c, http.StatusInternalServerError, "profile init failed")
		return
	}
	if err := h.store.UpsertPreferences(ctx, userID); err != nil {
		errJSON(c, http.StatusInternalServerError, "preferences init failed")
		return
	}

	// Cache-aside: check Redis before hitting DB for the full profile response.
	cacheKey := "profile:" + userID
	if h.rdb != nil {
		if cached, err := h.rdb.Get(ctx, cacheKey).Result(); err == nil {
			var resp meResponse
			if json.Unmarshal([]byte(cached), &resp) == nil {
				c.JSON(http.StatusOK, resp)
				return
			}
		}
	}

	h.respondWithProfile(c, userID, cacheKey)
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

	if req.ProfileSlug != nil && !slugPattern.MatchString(*req.ProfileSlug) {
		errJSON(c, http.StatusBadRequest, "profile_slug must be 3-30 lowercase alphanumeric chars or hyphens, no leading/trailing hyphens")
		return
	}

	patch := db.ProfilePatch{
		DisplayName: req.DisplayName,
		AvatarURL:   req.AvatarURL,
		Bio:         req.Bio,
		IsPublic:    req.IsPublic,
		ProfileSlug: req.ProfileSlug,
	}
	if err := h.store.UpdateProfile(ctx, userID, patch); err != nil {
		if strings.Contains(err.Error(), "unique") {
			errJSON(c, http.StatusConflict, "profile_slug already taken")
			return
		}
		errJSON(c, http.StatusInternalServerError, "profile update failed")
		return
	}

	if req.Preferences != nil {
		p := req.Preferences
		prefsPatch := db.PrefsPatch{
			DefaultSort:         p.DefaultSort,
			DefaultStatusFilter: p.DefaultStatusFilter,
			DefaultGenreFilter:  p.DefaultGenreFilter,
			UITheme:             p.UITheme,
		}
		if err := h.store.UpdatePreferences(ctx, userID, prefsPatch); err != nil {
			errJSON(c, http.StatusInternalServerError, "preferences update failed")
			return
		}
	}

	// Invalidate profile cache so next GetMe reads fresh data.
	if h.rdb != nil {
		h.rdb.Del(ctx, "profile:"+userID)
	}

	h.respondWithProfile(c, userID, "")
}

// ── GET /users/me/stats ───────────────────────────────────────────────────────

func (h *Handler) GetMyStats(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "missing user identity")
		return
	}

	stats, err := h.store.GetStats(c.Request.Context(), userID)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "stats fetch failed")
		return
	}
	if stats == nil {
		c.JSON(http.StatusOK, statsResponse{GenreBreakdown: map[string]int{}})
		return
	}
	c.JSON(http.StatusOK, statsResponse{
		TotalWatched:   stats.TotalWatched,
		TotalEpisodes:  stats.TotalEpisodes,
		AvgRating:      stats.AvgRating,
		GenreBreakdown: stats.GenreBreakdown,
	})
}

// ── GET /users/:slug ──────────────────────────────────────────────────────────

func (h *Handler) GetBySlug(c *gin.Context) {
	slug := c.Param("slug")
	ctx := c.Request.Context()

	// Cache-aside for public profile by slug.
	cacheKey := "profile:slug:" + slug
	if h.rdb != nil {
		if cached, err := h.rdb.Get(ctx, cacheKey).Result(); err == nil {
			var p profileResponse
			if json.Unmarshal([]byte(cached), &p) == nil {
				c.JSON(http.StatusOK, p)
				return
			}
		}
	}

	row, err := h.store.GetProfileBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		errJSON(c, http.StatusNotFound, "profile not found")
		return
	}
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "profile fetch failed")
		return
	}

	if !row.IsPublic {
		errJSON(c, http.StatusNotFound, "profile not found")
		return
	}

	p := profileResponse{
		ID:          row.ID,
		Email:       row.Email,
		DisplayName: row.DisplayName,
		AvatarURL:   row.AvatarURL,
		Bio:         row.Bio,
		IsPublic:    row.IsPublic,
		ProfileSlug: row.ProfileSlug,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}

	if h.rdb != nil {
		if b, err := json.Marshal(p); err == nil {
			h.rdb.Set(ctx, cacheKey, b, slugCacheTTL)
		}
	}

	c.JSON(http.StatusOK, p)
}

// ── GET /users/admin/list ─────────────────────────────────────────────────────

type adminUserResponse struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	DisplayName string  `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
	Bio         *string `json:"bio"`
	IsPublic    bool    `json:"is_public"`
	ProfileSlug *string `json:"profile_slug"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type adminUserListResponse struct {
	Users []adminUserResponse `json:"users"`
	Total int64               `json:"total"`
}

func (h *Handler) AdminListUsers(c *gin.Context) {
	if c.GetHeader("X-User-Role") != "admin" {
		errJSON(c, http.StatusForbidden, "admin access required")
		return
	}

	q := strings.TrimSpace(c.Query("q"))
	page, limit := parsePagination(c)

	rows, total, err := h.store.AdminListUsers(c.Request.Context(), q, page, limit)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}

	users := make([]adminUserResponse, len(rows))
	for i, u := range rows {
		users[i] = adminUserResponse{
			ID:          u.ID,
			Email:       u.Email,
			DisplayName: u.DisplayName,
			AvatarURL:   u.AvatarURL,
			Bio:         u.Bio,
			IsPublic:    u.IsPublic,
			ProfileSlug: u.ProfileSlug,
			CreatedAt:   u.CreatedAt,
			UpdatedAt:   u.UpdatedAt,
		}
	}
	c.JSON(http.StatusOK, adminUserListResponse{Users: users, Total: total})
}

// ── Shared helpers ────────────────────────────────────────────────────────────

func parsePagination(c *gin.Context) (page, limit int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ = strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return
}

// respondWithProfile fetches the full profile+prefs+stats and responds with JSON.
// If cacheKey is non-empty, the response is written to Redis on success.
func (h *Handler) respondWithProfile(c *gin.Context, userID, cacheKey string) {
	ctx := c.Request.Context()

	profileRow, err := h.store.GetProfile(ctx, userID)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "profile fetch failed")
		return
	}

	p := profileResponse{
		ID:          profileRow.ID,
		Email:       profileRow.Email,
		DisplayName: profileRow.DisplayName,
		AvatarURL:   profileRow.AvatarURL,
		Bio:         profileRow.Bio,
		IsPublic:    profileRow.IsPublic,
		ProfileSlug: profileRow.ProfileSlug,
		CreatedAt:   profileRow.CreatedAt,
		UpdatedAt:   profileRow.UpdatedAt,
	}

	prefsRow, _ := h.store.GetPreferences(ctx, userID)
	var prefsPtr *preferencesResponse
	if prefsRow != nil {
		prefsPtr = &preferencesResponse{
			DefaultSort:         prefsRow.DefaultSort,
			DefaultStatusFilter: prefsRow.DefaultStatusFilter,
			DefaultGenreFilter:  prefsRow.DefaultGenreFilter,
			UITheme:             prefsRow.UITheme,
		}
	}

	statsRow, _ := h.store.GetStats(ctx, userID)
	var statsPtr *statsResponse
	if statsRow != nil {
		statsPtr = &statsResponse{
			TotalWatched:   statsRow.TotalWatched,
			TotalEpisodes:  statsRow.TotalEpisodes,
			AvgRating:      statsRow.AvgRating,
			GenreBreakdown: statsRow.GenreBreakdown,
		}
	}

	resp := meResponse{Profile: p, Preferences: prefsPtr, WatchStats: statsPtr}

	if h.rdb != nil && cacheKey != "" {
		if b, err := json.Marshal(resp); err == nil {
			h.rdb.Set(ctx, cacheKey, b, profileCacheTTL)
		}
	}

	c.JSON(http.StatusOK, resp)
}
