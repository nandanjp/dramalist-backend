package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type searchResult struct {
	AnilistID     int      `json:"anilist_id"`
	Title         string   `json:"title"`
	OriginalTitle *string  `json:"original_title,omitempty"`
	CoverImage    *string  `json:"cover_image,omitempty"`
	Year          *int     `json:"year,omitempty"`
	Episodes      *int     `json:"episodes,omitempty"`
	Format        string   `json:"format"`
	Status        string   `json:"status"`
	Genres        []string `json:"genres"`
	AverageScore  *int     `json:"average_score,omitempty"`
	Synopsis      *string  `json:"synopsis,omitempty"`
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

	pageInfo, media, err := h.client.Search(c.Request.Context(), q, page, limit)
	if err != nil {
		errJSON(c, http.StatusBadGateway, "AniList search failed")
		return
	}

	results := make([]searchResult, 0, len(media))
	for _, m := range media {
		r := searchResult{
			AnilistID:     m.ID,
			Title:         m.PrimaryTitle(),
			OriginalTitle: m.Title.Native,
			CoverImage:    m.CoverImage.Large,
			Year:          m.StartDate.Year,
			Episodes:      m.Episodes,
			Format:        m.Format,
			Status:        m.Status,
			Genres:        m.Genres,
			AverageScore:  m.AverageScore,
			Synopsis:      m.Description,
		}
		if r.Genres == nil {
			r.Genres = []string{}
		}
		results = append(results, r)
	}

	c.JSON(http.StatusOK, gin.H{
		"results":   results,
		"page_info": pageInfo,
	})
}
