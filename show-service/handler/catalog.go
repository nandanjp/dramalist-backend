package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"dramalist/show-service/kafka"
)

// ── Domain types ──────────────────────────────────────────────────────────────

type catalogResponse struct {
	ID              string     `json:"id"`
	MediaType       string     `json:"media_type"`
	Title           string     `json:"title"`
	OriginalTitle   *string    `json:"original_title"`
	Synopsis        *string    `json:"synopsis"`
	PosterURL       *string    `json:"poster_url"`
	Year            *int       `json:"year"`
	Country         *string    `json:"country"`
	Language        *string    `json:"language"`
	EpisodeCount    *int       `json:"episode_count"`
	DurationMinutes *int       `json:"duration_minutes"`
	Genre           []string   `json:"genre"`
	AiringStatus    string     `json:"airing_status"`
	CreatedBy       string     `json:"created_by"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type catalogDetailResponse struct {
	catalogResponse
	Cast []castMemberResponse `json:"cast"`
}

type createCatalogRequest struct {
	MediaType       string   `json:"media_type"`
	Title           string   `json:"title" binding:"required"`
	OriginalTitle   *string  `json:"original_title"`
	Synopsis        *string  `json:"synopsis"`
	PosterURL       *string  `json:"poster_url"`
	Year            *int     `json:"year"`
	Country         *string  `json:"country"`
	Language        *string  `json:"language"`
	EpisodeCount    *int     `json:"episode_count"`
	DurationMinutes *int     `json:"duration_minutes"`
	Genre           []string `json:"genre"`
	AiringStatus    string   `json:"airing_status"`
}

type patchCatalogRequest struct {
	MediaType       *string  `json:"media_type"`
	Title           *string  `json:"title"`
	OriginalTitle   *string  `json:"original_title"`
	Synopsis        *string  `json:"synopsis"`
	PosterURL       *string  `json:"poster_url"`
	Year            *int     `json:"year"`
	Country         *string  `json:"country"`
	Language        *string  `json:"language"`
	EpisodeCount    *int     `json:"episode_count"`
	DurationMinutes *int     `json:"duration_minutes"`
	Genre           []string `json:"genre"`
	AiringStatus    *string  `json:"airing_status"`
}

var validAiringStatuses = map[string]bool{
	"ongoing":   true,
	"completed": true,
	"upcoming":  true,
}

const catalogSelectCols = `id::text, media_type, title, original_title, synopsis, poster_url,
    year, country, language, episode_count, duration_minutes, genre,
    airing_status, created_by::text, created_at, updated_at`

func scanCatalog(row interface {
	Scan(...any) error
}) (catalogResponse, error) {
	var c catalogResponse
	err := row.Scan(
		&c.ID, &c.MediaType, &c.Title, &c.OriginalTitle, &c.Synopsis, &c.PosterURL,
		&c.Year, &c.Country, &c.Language, &c.EpisodeCount, &c.DurationMinutes, &c.Genre,
		&c.AiringStatus, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
	)
	if c.Genre == nil {
		c.Genre = []string{}
	}
	return c, err
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// ListCatalog returns a paginated, filtered list of catalog entries.
// GET /catalog
func (h *Handler) ListCatalog(c *gin.Context) {
	ctx := c.Request.Context()

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	// Build dynamic WHERE clause
	where := []string{"1=1"}
	args := []any{}
	idx := 1

	if q := strings.TrimSpace(c.Query("q")); q != "" {
		where = append(where, fmt.Sprintf(
			"(lower(title) LIKE lower($%d) OR lower(original_title) LIKE lower($%d))",
			idx, idx,
		))
		args = append(args, "%"+q+"%")
		idx++
	}
	if mt := c.Query("media_type"); mt != "" && validMediaTypes[mt] {
		where = append(where, fmt.Sprintf("media_type = $%d", idx))
		args = append(args, mt)
		idx++
	}
	if genre := c.Query("genre"); genre != "" {
		where = append(where, fmt.Sprintf("$%d = ANY(genre)", idx))
		args = append(args, genre)
		idx++
	}
	if yrFrom := c.Query("year_from"); yrFrom != "" {
		if y, err := strconv.Atoi(yrFrom); err == nil {
			where = append(where, fmt.Sprintf("year >= $%d", idx))
			args = append(args, y)
			idx++
		}
	}
	if yrTo := c.Query("year_to"); yrTo != "" {
		if y, err := strconv.Atoi(yrTo); err == nil {
			where = append(where, fmt.Sprintf("year <= $%d", idx))
			args = append(args, y)
			idx++
		}
	}
	if country := c.Query("country"); country != "" {
		where = append(where, fmt.Sprintf("lower(country) = lower($%d)", idx))
		args = append(args, country)
		idx++
	}
	if lang := c.Query("language"); lang != "" {
		where = append(where, fmt.Sprintf("lower(language) = lower($%d)", idx))
		args = append(args, lang)
		idx++
	}
	if as := c.Query("airing_status"); as != "" && validAiringStatuses[as] {
		where = append(where, fmt.Sprintf("airing_status = $%d", idx))
		args = append(args, as)
		idx++
	}

	whereClause := strings.Join(where, " AND ")

	var total int
	countArgs := append([]any{}, args...)
	if err := h.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM catalog WHERE "+whereClause, countArgs...,
	).Scan(&total); err != nil {
		errJSON(c, http.StatusInternalServerError, "count failed")
		return
	}

	args = append(args, limit, offset)
	rows, err := h.pool.Query(ctx,
		"SELECT "+catalogSelectCols+" FROM catalog WHERE "+whereClause+
			fmt.Sprintf(" ORDER BY title ASC LIMIT $%d OFFSET $%d", idx, idx+1),
		args...,
	)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	entries := make([]catalogResponse, 0)
	for rows.Next() {
		entry, err := scanCatalog(rows)
		if err != nil {
			errJSON(c, http.StatusInternalServerError, "scan failed")
			return
		}
		entries = append(entries, entry)
	}

	c.JSON(http.StatusOK, gin.H{
		"entries": entries,
		"total":   total,
		"page":    page,
		"limit":   limit,
	})
}

// GetCatalogEntry returns a single catalog entry with its full cast.
// GET /catalog/:id
func (h *Handler) GetCatalogEntry(c *gin.Context) {
	id := c.Param("id")
	ctx := c.Request.Context()

	entry, err := scanCatalog(h.pool.QueryRow(ctx,
		"SELECT "+catalogSelectCols+" FROM catalog WHERE id = $1", id,
	))
	if err != nil {
		if err == pgx.ErrNoRows {
			errJSON(c, http.StatusNotFound, "not found")
			return
		}
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}

	cast, err := h.fetchCast(ctx, id)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "cast query failed")
		return
	}

	c.JSON(http.StatusOK, catalogDetailResponse{
		catalogResponse: entry,
		Cast:            cast,
	})
}

// CreateCatalogEntry creates a new catalog entry. Admin only.
// POST /catalog
func (h *Handler) CreateCatalogEntry(c *gin.Context) {
	if c.GetHeader("X-User-Role") != "admin" {
		errJSON(c, http.StatusForbidden, "admin only")
		return
	}
	userID := c.GetHeader("X-User-Id")

	var req createCatalogRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.MediaType == "" {
		req.MediaType = "show"
	}
	if !validMediaTypes[req.MediaType] {
		errJSON(c, http.StatusBadRequest, "invalid media_type")
		return
	}
	if req.AiringStatus == "" {
		req.AiringStatus = "completed"
	}
	if !validAiringStatuses[req.AiringStatus] {
		errJSON(c, http.StatusBadRequest, "invalid airing_status")
		return
	}
	if req.Genre == nil {
		req.Genre = []string{}
	}

	ctx := c.Request.Context()
	entry, err := scanCatalog(h.pool.QueryRow(ctx,
		`INSERT INTO catalog
		 (media_type, title, original_title, synopsis, poster_url, year, country, language,
		  episode_count, duration_minutes, genre, airing_status, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		 RETURNING `+catalogSelectCols,
		req.MediaType, req.Title, req.OriginalTitle, req.Synopsis, req.PosterURL,
		req.Year, req.Country, req.Language, req.EpisodeCount, req.DurationMinutes,
		req.Genre, req.AiringStatus, userID,
	))
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "insert failed")
		return
	}

	h.producer.Publish(c.Request.Context(), kafka.CatalogEvent{
		Event:     "catalog.created",
		CatalogID: entry.ID,
		MediaType: entry.MediaType,
		Title:     entry.Title,
		Genre:     entry.Genre,
		Year:      entry.Year,
		IsPublic:  true,
	})

	c.JSON(http.StatusCreated, entry)
}

// UpdateCatalogEntry patches a catalog entry. Admin only.
// PATCH /catalog/:id
func (h *Handler) UpdateCatalogEntry(c *gin.Context) {
	if c.GetHeader("X-User-Role") != "admin" {
		errJSON(c, http.StatusForbidden, "admin only")
		return
	}
	id := c.Param("id")

	var req patchCatalogRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.MediaType != nil && !validMediaTypes[*req.MediaType] {
		errJSON(c, http.StatusBadRequest, "invalid media_type")
		return
	}
	if req.AiringStatus != nil && !validAiringStatuses[*req.AiringStatus] {
		errJSON(c, http.StatusBadRequest, "invalid airing_status")
		return
	}

	ctx := c.Request.Context()

	sets := []string{}
	args := []any{}
	i := 1
	appendSet := func(col string, val any) {
		sets = append(sets, fmt.Sprintf("%s = $%d", col, i))
		args = append(args, val)
		i++
	}

	if req.MediaType != nil {
		appendSet("media_type", *req.MediaType)
	}
	if req.Title != nil {
		appendSet("title", *req.Title)
	}
	if req.OriginalTitle != nil {
		appendSet("original_title", *req.OriginalTitle)
	}
	if req.Synopsis != nil {
		appendSet("synopsis", *req.Synopsis)
	}
	if req.PosterURL != nil {
		appendSet("poster_url", *req.PosterURL)
	}
	if req.Year != nil {
		appendSet("year", *req.Year)
	}
	if req.Country != nil {
		appendSet("country", *req.Country)
	}
	if req.Language != nil {
		appendSet("language", *req.Language)
	}
	if req.EpisodeCount != nil {
		appendSet("episode_count", *req.EpisodeCount)
	}
	if req.DurationMinutes != nil {
		appendSet("duration_minutes", *req.DurationMinutes)
	}
	if req.Genre != nil {
		appendSet("genre", req.Genre)
	}
	if req.AiringStatus != nil {
		appendSet("airing_status", *req.AiringStatus)
	}

	if len(sets) == 0 {
		errJSON(c, http.StatusBadRequest, "no fields to update")
		return
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)

	entry, err := scanCatalog(h.pool.QueryRow(ctx,
		"UPDATE catalog SET "+strings.Join(sets, ", ")+
			fmt.Sprintf(" WHERE id = $%d RETURNING %s", i, catalogSelectCols),
		args...,
	))
	if err != nil {
		if err == pgx.ErrNoRows {
			errJSON(c, http.StatusNotFound, "not found")
			return
		}
		errJSON(c, http.StatusInternalServerError, "update failed")
		return
	}

	h.producer.Publish(c.Request.Context(), kafka.CatalogEvent{
		Event:     "catalog.updated",
		CatalogID: entry.ID,
		MediaType: entry.MediaType,
		Title:     entry.Title,
		Genre:     entry.Genre,
		Year:      entry.Year,
		IsPublic:  true,
	})

	c.JSON(http.StatusOK, entry)
}

// DeleteCatalogEntry removes a catalog entry. Admin only.
// DELETE /catalog/:id
func (h *Handler) DeleteCatalogEntry(c *gin.Context) {
	if c.GetHeader("X-User-Role") != "admin" {
		errJSON(c, http.StatusForbidden, "admin only")
		return
	}
	id := c.Param("id")
	ctx := c.Request.Context()

	result, err := h.pool.Exec(ctx, "DELETE FROM catalog WHERE id = $1", id)
	if err != nil || result.RowsAffected() == 0 {
		errJSON(c, http.StatusNotFound, "not found")
		return
	}

	h.producer.Publish(c.Request.Context(), kafka.CatalogEvent{
		Event:     "catalog.deleted",
		CatalogID: id,
	})

	c.Status(http.StatusNoContent)
}
