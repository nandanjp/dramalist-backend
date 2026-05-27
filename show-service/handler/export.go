package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ExportShow is the shape returned by the internal export endpoint.
// It mirrors the fields search-service indexes into Elasticsearch.
type ExportShow struct {
	ShowID        string   `json:"show_id"`
	UserID        string   `json:"user_id"`
	Title         string   `json:"title"`
	OriginalTitle *string  `json:"original_title"`
	Genre         []string `json:"genre"`
	Status        string   `json:"status"`
	Tags          []string `json:"tags"`
	Year          *int     `json:"year"`
	IsPublic      bool     `json:"is_public"`
}

// ExportAllShows streams all shows as a JSON array.
// Route: GET /internal/shows/all
// This route has no gateway nginx location — it is reachable only from within
// the Docker network. search-service calls it on cold start to backfill ES.
func (h *Handler) ExportAllShows(c *gin.Context) {
	ctx := c.Request.Context()

	rows, err := h.pool.Query(ctx,
		`SELECT id::text, user_id::text, title, original_title, genre,
		        status, tags, year, is_public
		 FROM shows`,
	)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "export failed")
		return
	}
	defer rows.Close()

	shows := make([]ExportShow, 0)
	for rows.Next() {
		var s ExportShow
		if err := rows.Scan(
			&s.ShowID, &s.UserID, &s.Title, &s.OriginalTitle, &s.Genre,
			&s.Status, &s.Tags, &s.Year, &s.IsPublic,
		); err != nil {
			errJSON(c, http.StatusInternalServerError, "scan failed")
			return
		}
		if s.Genre == nil {
			s.Genre = []string{}
		}
		if s.Tags == nil {
			s.Tags = []string{}
		}
		shows = append(shows, s)
	}
	c.JSON(http.StatusOK, shows)
}
