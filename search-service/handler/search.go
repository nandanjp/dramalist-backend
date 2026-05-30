package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"dramalist/search-service/elastic"
)

type searchResponse struct {
	Results []elastic.SearchResult `json:"results"`
	Total   int64                  `json:"total"`
	Page    int                    `json:"page"`
	Limit   int                    `json:"limit"`
}

func (h *Handler) Search(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit < 1 || limit > 100 {
		limit = 20
	}

	yearFrom, _ := strconv.Atoi(c.Query("year_from"))
	yearTo, _ := strconv.Atoi(c.Query("year_to"))

	params := elastic.SearchParams{
		Query:        c.Query("q"),
		MediaType:    c.Query("media_type"),
		Genre:        c.Query("genre"),
		YearFrom:     yearFrom,
		YearTo:       yearTo,
		Country:      c.Query("country"),
		Language:     c.Query("language"),
		AiringStatus: c.Query("airing_status"),
		Page:         page,
		Limit:        limit,
	}

	results, total, err := h.es.Search(c.Request.Context(), params)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "search failed")
		return
	}

	c.JSON(http.StatusOK, searchResponse{
		Results: results,
		Total:   total,
		Page:    page,
		Limit:   limit,
	})
}
