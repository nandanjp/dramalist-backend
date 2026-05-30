package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ExportCatalogEntry is the shape returned by the internal export endpoint.
// It mirrors the fields search-service indexes into Elasticsearch.
type ExportCatalogEntry struct {
	CatalogID     string   `json:"catalog_id"`
	MediaType     string   `json:"media_type"`
	Title         string   `json:"title"`
	OriginalTitle *string  `json:"original_title"`
	Synopsis      *string  `json:"synopsis"`
	Genre         []string `json:"genre"`
	AiringStatus  string   `json:"airing_status"`
	Year          *int     `json:"year"`
	Country       *string  `json:"country"`
	Language      *string  `json:"language"`
	PosterURL     *string  `json:"poster_url"`
}

// ExportAllCatalog streams all catalog entries as a JSON array.
// Route: GET /internal/catalog/all
// Reachable only from within the cluster. Used by search-service for cold-start backfill.
func (h *Handler) ExportAllCatalog(c *gin.Context) {
	ctx := c.Request.Context()

	rows, err := h.pool.Query(ctx,
		`SELECT id::text, media_type, title, original_title, synopsis,
		        genre, airing_status, year, country, language, poster_url
		 FROM catalog`,
	)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "export failed")
		return
	}
	defer rows.Close()

	entries := make([]ExportCatalogEntry, 0)
	for rows.Next() {
		var e ExportCatalogEntry
		if err := rows.Scan(
			&e.CatalogID, &e.MediaType, &e.Title, &e.OriginalTitle, &e.Synopsis,
			&e.Genre, &e.AiringStatus, &e.Year, &e.Country, &e.Language, &e.PosterURL,
		); err != nil {
			errJSON(c, http.StatusInternalServerError, "scan failed")
			return
		}
		if e.Genre == nil {
			e.Genre = []string{}
		}
		entries = append(entries, e)
	}
	c.JSON(http.StatusOK, entries)
}
