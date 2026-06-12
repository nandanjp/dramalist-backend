package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

// ── Domain types ──────────────────────────────────────────────────────────────

var validStatuses = map[string]bool{
	"watching":      true,
	"completed":     true,
	"dropped":       true,
	"plan_to_watch": true,
	"on_hold":       true,
}

var validMediaTypes = map[string]bool{
	"show":  true,
	"movie": true,
	"anime": true,
}

var validSorts = map[string]string{
	"created_at_desc": "ule.created_at DESC",
	"created_at_asc":  "ule.created_at ASC",
	"updated_at_desc": "ule.updated_at DESC",
	"title_asc":       "c.title ASC",
	"title_desc":      "c.title DESC",
}

type listEntryResponse struct {
	ID              string     `json:"id"`
	UserID          string     `json:"user_id"`
	CatalogID       string     `json:"catalog_id"`
	Status          string     `json:"status"`
	EpisodesWatched int        `json:"episodes_watched"`
	Notes           *string    `json:"notes"`
	Tags            []string   `json:"tags"`
	IsPublic        bool       `json:"is_public"`
	StartedAt       *time.Time `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	// Denormalized catalog fields for convenience
	MediaType       string     `json:"media_type"`
	Title           string     `json:"title"`
	OriginalTitle   *string    `json:"original_title"`
	PosterURL       *string    `json:"poster_url"`
	Year            *int       `json:"year"`
	Genre           []string   `json:"genre"`
	EpisodeCount    *int       `json:"episode_count"`
	DurationMinutes *int       `json:"duration_minutes"`
}

type createListEntryRequest struct {
	CatalogID       string   `json:"catalog_id" binding:"required"`
	Status          string   `json:"status"`
	EpisodesWatched int      `json:"episodes_watched"`
	Notes           *string  `json:"notes"`
	Tags            []string `json:"tags"`
	IsPublic        bool     `json:"is_public"`
	StartedAt       *string  `json:"started_at"`
	CompletedAt     *string  `json:"completed_at"`
}

type patchListEntryRequest struct {
	Status          *string  `json:"status"`
	EpisodesWatched *int     `json:"episodes_watched"`
	Notes           *string  `json:"notes"`
	Tags            []string `json:"tags"`
	IsPublic        *bool    `json:"is_public"`
	StartedAt       *string  `json:"started_at"`
	CompletedAt     *string  `json:"completed_at"`
}

const listEntrySelectCols = `ule.id::text, ule.user_id::text, ule.catalog_id::text,
    ule.status, ule.episodes_watched, ule.notes, ule.tags,
    ule.is_public, ule.started_at, ule.completed_at, ule.created_at, ule.updated_at,
    c.media_type, c.title, c.original_title, c.poster_url, c.year, c.genre,
    c.episode_count, c.duration_minutes`

func scanListEntry(row interface {
	Scan(...any) error
}) (listEntryResponse, error) {
	var e listEntryResponse
	err := row.Scan(
		&e.ID, &e.UserID, &e.CatalogID,
		&e.Status, &e.EpisodesWatched, &e.Notes, &e.Tags,
		&e.IsPublic, &e.StartedAt, &e.CompletedAt, &e.CreatedAt, &e.UpdatedAt,
		&e.MediaType, &e.Title, &e.OriginalTitle, &e.PosterURL, &e.Year, &e.Genre,
		&e.EpisodeCount, &e.DurationMinutes,
	)
	if e.Tags == nil {
		e.Tags = []string{}
	}
	if e.Genre == nil {
		e.Genre = []string{}
	}
	return e, err
}

const listEntryJoin = `FROM user_list_entries ule
    JOIN catalog c ON c.id = ule.catalog_id`

// ── Handlers ──────────────────────────────────────────────────────────────────

// ListEntries returns the authenticated user's list entries.
// GET /list
func (h *Handler) ListEntries(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
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

	orderBy := validSorts[c.DefaultQuery("sort", "created_at_desc")]
	if orderBy == "" {
		orderBy = "ule.created_at DESC"
	}

	where := []string{"ule.user_id = $1"}
	args := []any{userID}
	idx := 2

	if status := c.Query("status"); status != "" && validStatuses[status] {
		where = append(where, fmt.Sprintf("ule.status = $%d", idx))
		args = append(args, status)
		idx++
	}
	if mt := c.Query("media_type"); mt != "" && validMediaTypes[mt] {
		where = append(where, fmt.Sprintf("c.media_type = $%d", idx))
		args = append(args, mt)
		idx++
	}
	if genre := c.Query("genre"); genre != "" {
		where = append(where, fmt.Sprintf("$%d = ANY(c.genre)", idx))
		args = append(args, genre)
		idx++
	}

	whereClause := strings.Join(where, " AND ")

	var total int
	countArgs := append([]any{}, args...)
	if err := h.pool.QueryRow(ctx,
		"SELECT COUNT(*) "+listEntryJoin+" WHERE "+whereClause, countArgs...,
	).Scan(&total); err != nil {
		errJSON(c, http.StatusInternalServerError, "count failed")
		return
	}

	args = append(args, limit, offset)
	rows, err := h.pool.Query(ctx,
		"SELECT "+listEntrySelectCols+" "+listEntryJoin+" WHERE "+whereClause+
			fmt.Sprintf(" ORDER BY %s LIMIT $%d OFFSET $%d", orderBy, idx, idx+1),
		args...,
	)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	entries := make([]listEntryResponse, 0)
	for rows.Next() {
		entry, err := scanListEntry(rows)
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

// GetListEntry returns a single list entry.
// GET /list/:id
func (h *Handler) GetListEntry(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetHeader("X-User-Id")
	ctx := c.Request.Context()

	entry, err := scanListEntry(h.pool.QueryRow(ctx,
		"SELECT "+listEntrySelectCols+" "+listEntryJoin+" WHERE ule.id = $1 AND ule.user_id = $2",
		id, userID,
	))
	if err != nil {
		if err == pgx.ErrNoRows {
			errJSON(c, http.StatusNotFound, "not found")
			return
		}
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}
	c.JSON(http.StatusOK, entry)
}

// CreateListEntry adds a catalog entry to the user's list.
// POST /list
func (h *Handler) CreateListEntry(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")

	var req createListEntryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.Status == "" {
		req.Status = "plan_to_watch"
	}
	if !validStatuses[req.Status] {
		errJSON(c, http.StatusBadRequest, "invalid status")
		return
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}

	ctx := c.Request.Context()
	entry, err := scanListEntry(h.pool.QueryRow(ctx,
		`INSERT INTO user_list_entries
		 (user_id, catalog_id, status, episodes_watched, notes, tags, is_public, started_at, completed_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		 ON CONFLICT (user_id, catalog_id) DO UPDATE
		   SET status           = EXCLUDED.status,
		       episodes_watched = EXCLUDED.episodes_watched,
		       notes            = EXCLUDED.notes,
		       tags             = EXCLUDED.tags,
		       is_public        = EXCLUDED.is_public,
		       started_at       = EXCLUDED.started_at,
		       completed_at     = EXCLUDED.completed_at,
		       updated_at       = NOW()
		 RETURNING id::text, user_id::text, catalog_id::text,
		           status, episodes_watched, notes, tags, is_public,
		           started_at, completed_at, created_at, updated_at,
		           (SELECT media_type FROM catalog WHERE id = catalog_id),
		           (SELECT title       FROM catalog WHERE id = catalog_id),
		           (SELECT original_title FROM catalog WHERE id = catalog_id),
		           (SELECT poster_url  FROM catalog WHERE id = catalog_id),
		           (SELECT year        FROM catalog WHERE id = catalog_id),
		           (SELECT genre       FROM catalog WHERE id = catalog_id),
		           (SELECT episode_count FROM catalog WHERE id = catalog_id),
		           (SELECT duration_minutes FROM catalog WHERE id = catalog_id)`,
		userID, req.CatalogID, req.Status, req.EpisodesWatched,
		req.Notes, req.Tags, req.IsPublic, req.StartedAt, req.CompletedAt,
	))
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "insert failed")
		return
	}
	c.JSON(http.StatusCreated, entry)
}

// UpdateListEntry patches a user's list entry.
// PATCH /list/:id
func (h *Handler) UpdateListEntry(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetHeader("X-User-Id")

	var req patchListEntryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.Status != nil && !validStatuses[*req.Status] {
		errJSON(c, http.StatusBadRequest, "invalid status")
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

	if req.Status != nil {
		appendSet("status", *req.Status)
	}
	if req.EpisodesWatched != nil {
		appendSet("episodes_watched", *req.EpisodesWatched)
	}
	if req.Notes != nil {
		appendSet("notes", *req.Notes)
	}
	if req.Tags != nil {
		appendSet("tags", req.Tags)
	}
	if req.IsPublic != nil {
		appendSet("is_public", *req.IsPublic)
	}
	if req.StartedAt != nil {
		appendSet("started_at", *req.StartedAt)
	}
	if req.CompletedAt != nil {
		appendSet("completed_at", *req.CompletedAt)
	}

	if len(sets) == 0 {
		errJSON(c, http.StatusBadRequest, "no fields to update")
		return
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id, userID)

	entry, err := scanListEntry(h.pool.QueryRow(ctx,
		"UPDATE user_list_entries ule SET "+strings.Join(sets, ", ")+
			fmt.Sprintf(" FROM catalog c WHERE c.id = ule.catalog_id AND ule.id = $%d AND ule.user_id = $%d RETURNING %s",
				i, i+1, listEntrySelectCols),
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
	c.JSON(http.StatusOK, entry)
}

// DeleteListEntry removes an entry from the user's list.
// DELETE /list/:id
func (h *Handler) DeleteListEntry(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetHeader("X-User-Id")
	ctx := c.Request.Context()

	result, err := h.pool.Exec(ctx,
		"DELETE FROM user_list_entries WHERE id = $1 AND user_id = $2",
		id, userID,
	)
	if err != nil || result.RowsAffected() == 0 {
		errJSON(c, http.StatusNotFound, "not found")
		return
	}
	c.Status(http.StatusNoContent)
}

// ListPublicEntries returns another user's public list entries.
// GET /list/users/:userID
func (h *Handler) ListPublicEntries(c *gin.Context) {
	targetUserID := c.Param("userID")
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

	where := []string{"ule.user_id = $1", "ule.is_public = true"}
	args := []any{targetUserID}
	idx := 2

	if status := c.Query("status"); status != "" && validStatuses[status] {
		where = append(where, fmt.Sprintf("ule.status = $%d", idx))
		args = append(args, status)
		idx++
	}

	whereClause := strings.Join(where, " AND ")

	var total int
	countArgs := append([]any{}, args...)
	if err := h.pool.QueryRow(ctx,
		"SELECT COUNT(*) "+listEntryJoin+" WHERE "+whereClause, countArgs...,
	).Scan(&total); err != nil {
		errJSON(c, http.StatusInternalServerError, "count failed")
		return
	}

	args = append(args, limit, offset)
	rows, err := h.pool.Query(ctx,
		"SELECT "+listEntrySelectCols+" "+listEntryJoin+" WHERE "+whereClause+
			fmt.Sprintf(" ORDER BY ule.updated_at DESC LIMIT $%d OFFSET $%d", idx, idx+1),
		args...,
	)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	entries := make([]listEntryResponse, 0)
	for rows.Next() {
		entry, err := scanListEntry(rows)
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
