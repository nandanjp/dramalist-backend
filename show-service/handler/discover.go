package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"dramalist/show-service/db"
)

const discoverCacheTTL = 5 * time.Minute

// TrendingShows returns the 12 catalog entries most recently updated.
// GET /shows/public/trending
func (h *Handler) TrendingShows(c *gin.Context) {
	h.serveCatalogList(c, "shows:trending", "updated_at DESC", 12)
}

// RecentShows returns the 20 most recently created catalog entries.
// GET /shows/public/recent
func (h *Handler) RecentShows(c *gin.Context) {
	h.serveCatalogList(c, "shows:recent", "created_at DESC", 20)
}

func (h *Handler) serveCatalogList(c *gin.Context, cacheKey, orderBy string, limit int) {
	ctx := c.Request.Context()

	if h.rdb != nil {
		if cached, err := h.rdb.Get(ctx, cacheKey).Result(); err == nil {
			var entries []catalogResponse
			if json.Unmarshal([]byte(cached), &entries) == nil {
				c.JSON(http.StatusOK, entries)
				return
			}
		}
	}

	rows, err := h.querier.DiscoverCatalog(ctx, orderBy, limit)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}

	entries := make([]catalogResponse, 0, len(rows))
	for _, r := range rows {
		entries = append(entries, catalogRowToResponse(r))
	}

	if h.rdb != nil {
		if b, err := json.Marshal(entries); err == nil {
			h.rdb.Set(ctx, cacheKey, b, discoverCacheTTL)
		}
	}

	c.JSON(http.StatusOK, entries)
}

// catalogRowToResponse converts a db.CatalogRow to the handler response type.
func catalogRowToResponse(r db.CatalogRow) catalogResponse {
	genre := r.Genre
	if genre == nil {
		genre = []string{}
	}
	return catalogResponse{
		ID:              r.ID,
		MediaType:       r.MediaType,
		Title:           r.Title,
		OriginalTitle:   r.OriginalTitle,
		Synopsis:        r.Synopsis,
		PosterURL:       r.PosterURL,
		Year:            r.Year,
		Country:         r.Country,
		Language:        r.Language,
		EpisodeCount:    r.EpisodeCount,
		DurationMinutes: r.DurationMinutes,
		Genre:           genre,
		AiringStatus:    r.AiringStatus,
		CreatedBy:       r.CreatedBy,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
}

// invalidateDiscoverCache deletes both discovery cache keys.
// Called by write operations on catalog entries.
func (h *Handler) invalidateDiscoverCache() {
	if h.rdb == nil {
		return
	}
	h.rdb.Del(context.Background(), "shows:trending", "shows:recent")
}
