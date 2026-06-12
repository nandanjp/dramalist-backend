package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	dramadb "dramalist/drama-service/db"
)

type importRequest struct {
	TMDBID int `json:"tmdb_id" binding:"required"`
}

// countryToLanguage maps a 2-letter ISO country code to a language name.
func countryToLanguage(country *string) *string {
	if country == nil {
		return nil
	}
	m := map[string]string{
		"KR": "Korean",
		"JP": "Japanese",
		"CN": "Chinese",
		"TW": "Chinese",
		"HK": "Chinese",
		"TH": "Thai",
		"US": "English",
		"GB": "English",
	}
	if lang, ok := m[*country]; ok {
		return &lang
	}
	return nil
}

// tmdbStatusToAiringStatus converts TMDB's "status" field to our airing_status enum.
func tmdbStatusToAiringStatus(status string) string {
	switch strings.ToLower(status) {
	case "returning series", "in production", "planned":
		return "ongoing"
	case "ended", "canceled":
		return "completed"
	case "pilot":
		return "upcoming"
	default:
		return "completed"
	}
}

// showCreateRequest mirrors show-service's createCatalogRequest JSON structure.
type showCreateRequest struct {
	MediaType       string   `json:"media_type"`
	Title           string   `json:"title"`
	OriginalTitle   *string  `json:"original_title,omitempty"`
	Synopsis        *string  `json:"synopsis,omitempty"`
	PosterURL       *string  `json:"poster_url,omitempty"`
	Year            *int     `json:"year,omitempty"`
	Country         *string  `json:"country,omitempty"`
	Language        *string  `json:"language,omitempty"`
	EpisodeCount    *int     `json:"episode_count,omitempty"`
	DurationMinutes *int     `json:"duration_minutes,omitempty"`
	Genre           []string `json:"genre"`
	AiringStatus    string   `json:"airing_status"`
	TMDBID          *int     `json:"tmdb_id,omitempty"`
}

func (h *Handler) Import(c *gin.Context) {
	if c.GetHeader("X-User-Role") != "admin" {
		errJSON(c, http.StatusForbidden, "admin only")
		return
	}
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "missing user identity")
		return
	}

	var req importRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, "tmdb_id is required")
		return
	}

	ctx := c.Request.Context()

	// Conflict check: query show-service first (prefer API), fall back to direct DB.
	if exists, _ := dramadb.CheckTMDBExists(ctx, h.pool, req.TMDBID); exists {
		errJSON(c, http.StatusConflict, "already imported")
		return
	}

	detail, err := h.tmdb.FetchTV(ctx, req.TMDBID)
	if err != nil {
		slog.Warn("tmdb fetch failed", "tmdb_id", req.TMDBID, "err", err)
		errJSON(c, http.StatusBadGateway, "TMDB fetch failed")
		return
	}

	var year *int
	if len(detail.FirstAirDate) >= 4 {
		if y, err := strconv.Atoi(detail.FirstAirDate[:4]); err == nil && y > 0 {
			year = &y
		}
	}

	var country *string
	if len(detail.OriginCountry) > 0 {
		c := detail.OriginCountry[0]
		country = &c
	}

	genres := make([]string, 0, len(detail.Genres))
	for _, g := range detail.Genres {
		genres = append(genres, strings.ToLower(g.Name))
	}

	var episodeCount *int
	if detail.NumberOfEpisodes > 0 {
		ec := detail.NumberOfEpisodes
		episodeCount = &ec
	}

	var duration *int
	if len(detail.EpisodeRunTime) > 0 && detail.EpisodeRunTime[0] > 0 {
		d := detail.EpisodeRunTime[0]
		duration = &d
	}

	var synopsis *string
	if detail.Overview != "" {
		s := detail.Overview
		synopsis = &s
	}

	var originalTitle *string
	if detail.OriginalName != "" && detail.OriginalName != detail.Name {
		originalTitle = &detail.OriginalName
	}

	posterPath := h.tmdb.PosterURL(detail.PosterPath)
	var posterURL *string
	if posterPath != "" {
		posterURL = &posterPath
	}

	tmdbID := req.TMDBID

	// Create the catalog entry via show-service so the catalog.created Kafka event is published.
	catalogID, err := h.callShowServiceCreate(ctx, userID, showCreateRequest{
		MediaType:       "show",
		Title:           detail.Name,
		OriginalTitle:   originalTitle,
		Synopsis:        synopsis,
		PosterURL:       posterURL,
		Year:            year,
		Country:         country,
		Language:        countryToLanguage(country),
		EpisodeCount:    episodeCount,
		DurationMinutes: duration,
		Genre:           genres,
		AiringStatus:    tmdbStatusToAiringStatus(detail.Status),
		TMDBID:          &tmdbID,
	})
	if err != nil {
		slog.Error("show-service catalog create failed", "err", err, "tmdb_id", req.TMDBID)
		errJSON(c, http.StatusInternalServerError, "failed to create catalog entry")
		return
	}

	// Mirror poster image to MinIO via media-service.
	if posterPath != "" {
		if mirrored := h.mirrorImage(ctx, posterPath, "catalog", catalogID, "poster", userID); mirrored != "" {
			if _, dbErr := h.pool.Exec(ctx,
				`UPDATE catalog SET poster_url = $1 WHERE id = $2`, mirrored, catalogID,
			); dbErr != nil {
				slog.Warn("update catalog poster_url failed", "err", dbErr)
			}
		}
	}

	// Import top cast from TMDB credits.
	castImported := h.importCast(ctx, req.TMDBID, catalogID, userID)

	c.JSON(http.StatusCreated, gin.H{
		"id":           catalogID,
		"title":        detail.Name,
		"tmdb_id":      req.TMDBID,
		"cast_imported": castImported,
	})
}

// callShowServiceCreate POSTs to show-service's /catalog endpoint and returns the new catalog UUID.
func (h *Handler) callShowServiceCreate(ctx context.Context, userID string, payload showCreateRequest) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.showURL+"/catalog", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", userID)
	req.Header.Set("X-User-Role", "admin")

	resp, err := imageHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("show-service returned %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if result.ID == "" {
		return "", fmt.Errorf("show-service returned empty id")
	}
	return result.ID, nil
}

func strPtrIfNonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// importCast fetches TMDB credits for tmdbID and upserts the top 20 cast members.
func (h *Handler) importCast(ctx context.Context, tmdbID int, catalogID, userID string) int {
	credits, err := h.tmdb.FetchTVCredits(ctx, tmdbID)
	if err != nil {
		slog.Warn("importCast: fetch credits failed", "tmdb_id", tmdbID, "err", err)
		return 0
	}

	imported := 0
	limit := 20
	for i, cm := range credits {
		if i >= limit {
			break
		}

		params := dramadb.ActorParams{
			Name:         cm.Name,
			TMDBPersonID: cm.ID,
		}
		if cm.OriginalName != "" && cm.OriginalName != cm.Name {
			params.NativeName = &cm.OriginalName
		}
		if cm.ProfilePath != "" {
			u := h.tmdb.ProfileURL(cm.ProfilePath)
			params.ProfileImageURL = &u
		}

		actorID, err := dramadb.UpsertActor(ctx, h.pool, params)
		if err != nil {
			slog.Warn("importCast: upsert actor failed", "name", cm.Name, "err", err)
			continue
		}

		if cm.ProfilePath != "" && params.ProfileImageURL != nil {
			if mirrored := h.mirrorImage(ctx, *params.ProfileImageURL, "actor", actorID, "profile", userID); mirrored != "" {
				_ = dramadb.UpdateActorProfileImage(ctx, h.pool, actorID, mirrored)
			}
		}

		role := "supporting"
		if cm.Order < 5 {
			role = "main"
		}
		if err := dramadb.InsertCastMember(ctx, h.pool, catalogID, actorID, cm.Character, role, cm.Order); err != nil {
			slog.Warn("importCast: insert cast member failed", "actor_id", actorID, "err", err)
			continue
		}
		imported++
	}
	return imported
}
