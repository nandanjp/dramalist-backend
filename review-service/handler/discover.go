package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type publicReviewPreview struct {
	ID             string    `json:"id"`
	CatalogID      string    `json:"catalog_id"`
	UserID         string    `json:"user_id"`
	Rating         float64   `json:"rating"`
	ContentSnippet *string   `json:"content_snippet"`
	CreatedAt      time.Time `json:"created_at"`
}

// RecentPublicReviews returns the 20 most recently created public reviews.
// Content is truncated to 200 characters for preview cards.
// GET /reviews/public/recent
func (h *Handler) RecentPublicReviews(c *gin.Context) {
	ctx := c.Request.Context()

	rows, err := h.pool.Query(ctx,
		`SELECT id::text, catalog_id::text, user_id::text, rating,
		        CASE WHEN content IS NOT NULL THEN LEFT(content, 200) ELSE NULL END,
		        created_at
		 FROM reviews
		 WHERE is_public = true
		 ORDER BY created_at DESC
		 LIMIT 20`,
	)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	previews := make([]publicReviewPreview, 0, 20)
	for rows.Next() {
		var p publicReviewPreview
		if err := rows.Scan(&p.ID, &p.CatalogID, &p.UserID, &p.Rating, &p.ContentSnippet, &p.CreatedAt); err != nil {
			errJSON(c, http.StatusInternalServerError, "scan failed")
			return
		}
		previews = append(previews, p)
	}
	c.JSON(http.StatusOK, previews)
}
