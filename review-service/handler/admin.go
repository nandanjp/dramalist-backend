package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// AdminListReviews returns all reviews (public and private) paginated.
// GET /reviews/admin/all
func (h *Handler) AdminListReviews(c *gin.Context) {
	if c.GetHeader("X-User-Role") != "admin" {
		errJSON(c, http.StatusForbidden, "admin access required")
		return
	}

	page, limit := parsePagination(c)
	ctx := c.Request.Context()

	var total int64
	if err := h.pool.QueryRow(ctx, "SELECT COUNT(*) FROM reviews").Scan(&total); err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}

	rows, err := h.pool.Query(ctx,
		`SELECT id::text, catalog_id::text, catalog_title, user_id::text, rating, content,
		        contains_spoilers, is_public, created_at, updated_at
		 FROM reviews
		 ORDER BY created_at DESC
		 LIMIT $1 OFFSET $2`,
		limit, (page-1)*limit,
	)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	reviews := make([]reviewResponse, 0)
	for rows.Next() {
		r, err := scanReview(rows)
		if err != nil {
			errJSON(c, http.StatusInternalServerError, "scan failed")
			return
		}
		reviews = append(reviews, r)
	}

	c.JSON(http.StatusOK, listResponse{Reviews: reviews, Total: total, Page: page, Limit: limit})
}
