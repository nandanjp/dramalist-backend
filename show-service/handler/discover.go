package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// TrendingShows returns the 12 catalog entries most recently added to any user's list.
// GET /shows/public/trending
func (h *Handler) TrendingShows(c *gin.Context) {
	h.discoverCatalog(c, "updated_at DESC", 12)
}

// RecentShows returns the 20 most recently created catalog entries.
// GET /shows/public/recent
func (h *Handler) RecentShows(c *gin.Context) {
	h.discoverCatalog(c, "created_at DESC", 20)
}

func (h *Handler) discoverCatalog(c *gin.Context, orderBy string, limit int) {
	ctx := c.Request.Context()

	// Safe: orderBy is a hardcoded internal string, never user-supplied.
	rows, err := h.pool.Query(ctx,
		"SELECT "+catalogSelectCols+" FROM catalog ORDER BY "+orderBy+" LIMIT $1",
		limit,
	)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	entries := make([]catalogResponse, 0, limit)
	for rows.Next() {
		entry, err := scanCatalog(rows)
		if err != nil {
			errJSON(c, http.StatusInternalServerError, "scan failed")
			return
		}
		entries = append(entries, entry)
	}
	c.JSON(http.StatusOK, entries)
}
