package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"dramalist/drama-service/tmdb"
)

type searchResult struct {
	TMDBID        int      `json:"tmdb_id"`
	Title         string   `json:"title"`
	OriginalTitle string   `json:"original_title,omitempty"`
	PosterURL     string   `json:"poster_url,omitempty"`
	Year          *int     `json:"year,omitempty"`
	Country       string   `json:"country,omitempty"`
	VoteAverage   float64  `json:"vote_average,omitempty"`
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

	raw, totalPages, err := h.tmdb.SearchTV(c.Request.Context(), q, page)
	if err != nil {
		errJSON(c, http.StatusBadGateway, "TMDB search failed")
		return
	}

	results := make([]searchResult, 0, len(raw))
	for _, r := range raw {
		sr := searchResult{
			TMDBID:       r.ID,
			Title:        r.Name,
			OriginalTitle: r.OriginalName,
			VoteAverage:  r.VoteAverage,
		}
		if r.PosterPath != "" {
			sr.PosterURL = h.tmdb.PosterURL(r.PosterPath)
		}
		if len(r.FirstAirDate) >= 4 {
			if y, err := strconv.Atoi(r.FirstAirDate[:4]); err == nil {
				sr.Year = &y
			}
		}
		if len(r.OriginCountry) > 0 {
			sr.Country = tmdb.OriginCountryName(r.OriginCountry[0])
		}
		results = append(results, sr)
	}

	c.JSON(http.StatusOK, gin.H{
		"results":     results,
		"page":        page,
		"total_pages": totalPages,
		"has_more":    page < totalPages,
	})
}
