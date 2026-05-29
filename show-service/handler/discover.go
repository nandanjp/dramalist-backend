package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const discoverShowSQL = `
	SELECT id::text, user_id::text, title, original_title, genre,
	       status, episode_count, episodes_watched, year, country, language,
	       notes, tags, is_public, started_at, completed_at, created_at, updated_at
	FROM shows
	WHERE is_public = true
	ORDER BY %s
	LIMIT $1`

// TrendingShows returns the 12 most recently updated public shows.
// GET /shows/public/trending
func (h *Handler) TrendingShows(c *gin.Context) {
	h.discoverShows(c, "updated_at DESC", 12)
}

// RecentShows returns the 20 most recently created public shows.
// GET /shows/public/recent
func (h *Handler) RecentShows(c *gin.Context) {
	h.discoverShows(c, "created_at DESC", 20)
}

func (h *Handler) discoverShows(c *gin.Context, orderBy string, limit int) {
	ctx := c.Request.Context()

	rows, err := h.pool.Query(ctx,
		// Safe: orderBy is a hardcoded internal string, never user-supplied.
		"SELECT id::text, user_id::text, title, original_title, genre, "+
			"status, episode_count, episodes_watched, year, country, language, "+
			"notes, tags, is_public, started_at, completed_at, created_at, updated_at "+
			"FROM shows WHERE is_public = true ORDER BY "+orderBy+" LIMIT $1",
		limit,
	)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	shows := make([]showResponse, 0, limit)
	for rows.Next() {
		s, err := scanShow(rows)
		if err != nil {
			errJSON(c, http.StatusInternalServerError, "scan failed")
			return
		}
		shows = append(shows, s)
	}
	c.JSON(http.StatusOK, shows)
}
