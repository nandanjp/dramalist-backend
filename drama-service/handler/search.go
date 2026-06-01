package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type searchResult struct {
	MDLID        int      `json:"mdl_id"`
	Slug         string   `json:"slug"`
	Title        string   `json:"title"`
	OriginalTitle *string `json:"original_title,omitempty"`
	PosterURL    *string  `json:"poster_url,omitempty"`
	Year         *int     `json:"year,omitempty"`
	Episodes     *int     `json:"episodes,omitempty"`
	Type         string   `json:"type"`
	Country      string   `json:"country"`
	Rating       *float64 `json:"rating,omitempty"`
}

func (h *Handler) Search(c *gin.Context) {
	q := c.Query("q")
	if q == "" {
		errJSON(c, http.StatusBadRequest, "q is required")
		return
	}

	page := 1
	if p := c.Query("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			page = n
		}
	}

	limit := 20
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}

	raw, hasMore, err := h.client.Search(c.Request.Context(), q, page, limit)
	if err != nil {
		errJSON(c, http.StatusBadGateway, "MDL search failed")
		return
	}

	results := make([]searchResult, 0, len(raw))
	for _, r := range raw {
		results = append(results, searchResult{
			MDLID:     r.MDLID,
			Slug:      r.Slug,
			Title:     r.Title,
			PosterURL: r.PosterURL,
			Year:      r.Year,
			Episodes:  r.EpisodeCount,
			Type:      r.Type,
			Country:   r.Country,
			Rating:    r.Rating,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"results":  results,
		"page":     page,
		"has_more": hasMore,
	})
}
