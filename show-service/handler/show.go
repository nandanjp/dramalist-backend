package handler

import (
	"context"
	"errors"
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

var validStatuses = map[string]bool{
	"watching":      true,
	"completed":     true,
	"dropped":       true,
	"plan_to_watch": true,
	"on_hold":       true,
}

var validSorts = map[string]string{
	"created_at_desc": "created_at DESC",
	"created_at_asc":  "created_at ASC",
	"updated_at_desc": "updated_at DESC",
	"title_asc":       "title ASC",
	"title_desc":      "title DESC",
}

type showResponse struct {
	ID              string     `json:"id"`
	UserID          string     `json:"user_id"`
	Title           string     `json:"title"`
	OriginalTitle   *string    `json:"original_title"`
	Genre           []string   `json:"genre"`
	Status          string     `json:"status"`
	EpisodeCount    *int       `json:"episode_count"`
	EpisodesWatched int        `json:"episodes_watched"`
	Year            *int       `json:"year"`
	Country         *string    `json:"country"`
	Language        *string    `json:"language"`
	Notes           *string    `json:"notes"`
	Tags            []string   `json:"tags"`
	IsPublic        bool       `json:"is_public"`
	StartedAt       *time.Time `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type createShowRequest struct {
	Title           string     `json:"title" binding:"required"`
	OriginalTitle   *string    `json:"original_title"`
	Genre           []string   `json:"genre"`
	Status          string     `json:"status"`
	EpisodeCount    *int       `json:"episode_count"`
	EpisodesWatched int        `json:"episodes_watched"`
	Year            *int       `json:"year"`
	Country         *string    `json:"country"`
	Language        *string    `json:"language"`
	Notes           *string    `json:"notes"`
	Tags            []string   `json:"tags"`
	IsPublic        bool       `json:"is_public"`
	StartedAt       *time.Time `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at"`
}

type patchShowRequest struct {
	Title           *string    `json:"title"`
	OriginalTitle   *string    `json:"original_title"`
	Genre           *[]string  `json:"genre"`
	Status          *string    `json:"status"`
	EpisodeCount    *int       `json:"episode_count"`
	EpisodesWatched *int       `json:"episodes_watched"`
	Year            *int       `json:"year"`
	Country         *string    `json:"country"`
	Language        *string    `json:"language"`
	Notes           *string    `json:"notes"`
	Tags            *[]string  `json:"tags"`
	IsPublic        *bool      `json:"is_public"`
	StartedAt       *time.Time `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at"`
}

type listResponse struct {
	Shows []showResponse `json:"shows"`
	Total int64          `json:"total"`
	Page  int            `json:"page"`
	Limit int            `json:"limit"`
}

// ── GET /shows ────────────────────────────────────────────────────────────────

func (h *Handler) ListShows(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "missing user identity")
		return
	}

	page, limit, orderBy := parsePagination(c)
	ctx := c.Request.Context()

	args := []any{userID}
	conds := []string{"user_id = $1"}
	n := 2

	if s := c.Query("status"); s != "" {
		if !validStatuses[s] {
			errJSON(c, http.StatusBadRequest, "invalid status")
			return
		}
		conds = append(conds, fmt.Sprintf("status = $%d", n))
		args = append(args, s)
		n++
	}
	if g := c.Query("genre"); g != "" {
		conds = append(conds, fmt.Sprintf("genre @> ARRAY[$%d::text]", n))
		args = append(args, g)
		n++
	}

	where := strings.Join(conds, " AND ")

	var total int64
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM shows WHERE %s", where)
	if err := h.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}

	args = append(args, limit, (page-1)*limit)
	rows, err := h.pool.Query(ctx,
		fmt.Sprintf(`SELECT id::text, user_id::text, title, original_title, genre,
		             status, episode_count, episodes_watched, year, country, language,
		             notes, tags, is_public, started_at, completed_at, created_at, updated_at
		             FROM shows WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d`,
			where, orderBy, n, n+1),
		args...,
	)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	shows := make([]showResponse, 0)
	for rows.Next() {
		s, err := scanShow(rows)
		if err != nil {
			errJSON(c, http.StatusInternalServerError, "scan failed")
			return
		}
		shows = append(shows, s)
	}

	c.JSON(http.StatusOK, listResponse{Shows: shows, Total: total, Page: page, Limit: limit})
}

// ── POST /shows ───────────────────────────────────────────────────────────────

func (h *Handler) CreateShow(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "missing user identity")
		return
	}

	var req createShowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Status == "" {
		req.Status = "plan_to_watch"
	}
	if !validStatuses[req.Status] {
		errJSON(c, http.StatusBadRequest, "invalid status")
		return
	}
	if req.Genre == nil {
		req.Genre = []string{}
	}
	if req.Tags == nil {
		req.Tags = []string{}
	}

	ctx := c.Request.Context()

	var showID string
	err := h.pool.QueryRow(ctx,
		`INSERT INTO shows
		   (user_id, title, original_title, genre, status, episode_count,
		    episodes_watched, year, country, language, notes, tags,
		    is_public, started_at, completed_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		 RETURNING id::text`,
		userID, req.Title, req.OriginalTitle, req.Genre, req.Status,
		req.EpisodeCount, req.EpisodesWatched, req.Year, req.Country,
		req.Language, req.Notes, req.Tags, req.IsPublic, req.StartedAt, req.CompletedAt,
	).Scan(&showID)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "create failed")
		return
	}

	show, err := h.fetchShow(ctx, showID)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "fetch failed")
		return
	}

	go h.producer.Publish(context.Background(), kafka.ShowEvent{
		Event:         "show.created",
		ShowID:        showID,
		UserID:        userID,
		Title:         show.Title,
		OriginalTitle: show.OriginalTitle,
		Genre:         show.Genre,
		Status:        show.Status,
		Tags:          show.Tags,
		Year:          show.Year,
		IsPublic:      show.IsPublic,
	})

	c.JSON(http.StatusCreated, show)
}

// ── GET /shows/:id ────────────────────────────────────────────────────────────

func (h *Handler) GetShow(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	showID := c.Param("id")
	ctx := c.Request.Context()

	show, err := h.fetchShow(ctx, showID)
	if errors.Is(err, pgx.ErrNoRows) {
		errJSON(c, http.StatusNotFound, "show not found")
		return
	}
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "fetch failed")
		return
	}

	if show.UserID != userID && !show.IsPublic {
		errJSON(c, http.StatusNotFound, "show not found")
		return
	}

	c.JSON(http.StatusOK, show)
}

// ── PATCH /shows/:id ──────────────────────────────────────────────────────────

func (h *Handler) UpdateShow(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "missing user identity")
		return
	}
	showID := c.Param("id")

	var req patchShowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errJSON(c, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Status != nil && !validStatuses[*req.Status] {
		errJSON(c, http.StatusBadRequest, "invalid status")
		return
	}

	ctx := c.Request.Context()

	// Verify ownership before updating.
	var ownerID string
	err := h.pool.QueryRow(ctx, "SELECT user_id::text FROM shows WHERE id = $1", showID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		errJSON(c, http.StatusNotFound, "show not found")
		return
	}
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "fetch failed")
		return
	}
	if ownerID != userID {
		errJSON(c, http.StatusForbidden, "not your show")
		return
	}

	cols := []string{}
	args := []any{showID}
	n := 2

	if req.Title != nil {
		cols = append(cols, fmt.Sprintf("title = $%d", n)); args = append(args, *req.Title); n++
	}
	if req.OriginalTitle != nil {
		cols = append(cols, fmt.Sprintf("original_title = $%d", n)); args = append(args, *req.OriginalTitle); n++
	}
	if req.Genre != nil {
		cols = append(cols, fmt.Sprintf("genre = $%d", n)); args = append(args, *req.Genre); n++
	}
	if req.Status != nil {
		cols = append(cols, fmt.Sprintf("status = $%d", n)); args = append(args, *req.Status); n++
	}
	if req.EpisodeCount != nil {
		cols = append(cols, fmt.Sprintf("episode_count = $%d", n)); args = append(args, *req.EpisodeCount); n++
	}
	if req.EpisodesWatched != nil {
		cols = append(cols, fmt.Sprintf("episodes_watched = $%d", n)); args = append(args, *req.EpisodesWatched); n++
	}
	if req.Year != nil {
		cols = append(cols, fmt.Sprintf("year = $%d", n)); args = append(args, *req.Year); n++
	}
	if req.Country != nil {
		cols = append(cols, fmt.Sprintf("country = $%d", n)); args = append(args, *req.Country); n++
	}
	if req.Language != nil {
		cols = append(cols, fmt.Sprintf("language = $%d", n)); args = append(args, *req.Language); n++
	}
	if req.Notes != nil {
		cols = append(cols, fmt.Sprintf("notes = $%d", n)); args = append(args, *req.Notes); n++
	}
	if req.Tags != nil {
		cols = append(cols, fmt.Sprintf("tags = $%d", n)); args = append(args, *req.Tags); n++
	}
	if req.IsPublic != nil {
		cols = append(cols, fmt.Sprintf("is_public = $%d", n)); args = append(args, *req.IsPublic); n++
	}
	if req.StartedAt != nil {
		cols = append(cols, fmt.Sprintf("started_at = $%d", n)); args = append(args, *req.StartedAt); n++
	}
	if req.CompletedAt != nil {
		cols = append(cols, fmt.Sprintf("completed_at = $%d", n)); args = append(args, *req.CompletedAt); n++
	}

	if len(cols) == 0 {
		// Nothing to update; return current state.
		show, _ := h.fetchShow(ctx, showID)
		c.JSON(http.StatusOK, show)
		return
	}

	cols = append(cols, "updated_at = NOW()")
	query := fmt.Sprintf("UPDATE shows SET %s WHERE id = $1", strings.Join(cols, ", "))
	if _, err := h.pool.Exec(ctx, query, args...); err != nil {
		errJSON(c, http.StatusInternalServerError, "update failed")
		return
	}

	show, err := h.fetchShow(ctx, showID)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "fetch failed")
		return
	}

	go h.producer.Publish(context.Background(), kafka.ShowEvent{
		Event:         "show.updated",
		ShowID:        showID,
		UserID:        userID,
		Title:         show.Title,
		OriginalTitle: show.OriginalTitle,
		Genre:         show.Genre,
		Status:        show.Status,
		Tags:          show.Tags,
		Year:          show.Year,
		IsPublic:      show.IsPublic,
	})

	c.JSON(http.StatusOK, show)
}

// ── DELETE /shows/:id ─────────────────────────────────────────────────────────

func (h *Handler) DeleteShow(c *gin.Context) {
	userID := c.GetHeader("X-User-Id")
	if userID == "" {
		errJSON(c, http.StatusUnauthorized, "missing user identity")
		return
	}
	showID := c.Param("id")
	ctx := c.Request.Context()

	tag, err := h.pool.Exec(ctx,
		"DELETE FROM shows WHERE id = $1 AND user_id = $2",
		showID, userID,
	)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "delete failed")
		return
	}
	if tag.RowsAffected() == 0 {
		errJSON(c, http.StatusNotFound, "show not found")
		return
	}

	go h.producer.Publish(context.Background(), kafka.ShowEvent{
		Event:  "show.deleted",
		ShowID: showID,
		UserID: userID,
	})

	c.Status(http.StatusNoContent)
}

// ── GET /shows/users/:userID ──────────────────────────────────────────────────

func (h *Handler) ListPublicShows(c *gin.Context) {
	targetUserID := c.Param("userID")
	page, limit, orderBy := parsePagination(c)
	ctx := c.Request.Context()

	args := []any{targetUserID}
	conds := []string{"user_id = $1", "is_public = true"}
	n := 2

	if s := c.Query("status"); s != "" {
		if !validStatuses[s] {
			errJSON(c, http.StatusBadRequest, "invalid status")
			return
		}
		conds = append(conds, fmt.Sprintf("status = $%d", n))
		args = append(args, s)
		n++
	}
	if g := c.Query("genre"); g != "" {
		conds = append(conds, fmt.Sprintf("genre @> ARRAY[$%d::text]", n))
		args = append(args, g)
		n++
	}

	where := strings.Join(conds, " AND ")

	var total int64
	if err := h.pool.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM shows WHERE %s", where), args...).Scan(&total); err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}

	args = append(args, limit, (page-1)*limit)
	rows, err := h.pool.Query(ctx,
		fmt.Sprintf(`SELECT id::text, user_id::text, title, original_title, genre,
		             status, episode_count, episodes_watched, year, country, language,
		             notes, tags, is_public, started_at, completed_at, created_at, updated_at
		             FROM shows WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d`,
			where, orderBy, n, n+1),
		args...,
	)
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	shows := make([]showResponse, 0)
	for rows.Next() {
		s, err := scanShow(rows)
		if err != nil {
			errJSON(c, http.StatusInternalServerError, "scan failed")
			return
		}
		shows = append(shows, s)
	}

	c.JSON(http.StatusOK, listResponse{Shows: shows, Total: total, Page: page, Limit: limit})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanShow(row scanner) (showResponse, error) {
	var s showResponse
	err := row.Scan(
		&s.ID, &s.UserID, &s.Title, &s.OriginalTitle, &s.Genre,
		&s.Status, &s.EpisodeCount, &s.EpisodesWatched, &s.Year,
		&s.Country, &s.Language, &s.Notes, &s.Tags, &s.IsPublic,
		&s.StartedAt, &s.CompletedAt, &s.CreatedAt, &s.UpdatedAt,
	)
	if s.Genre == nil {
		s.Genre = []string{}
	}
	if s.Tags == nil {
		s.Tags = []string{}
	}
	return s, err
}

func (h *Handler) fetchShow(ctx context.Context, showID string) (showResponse, error) {
	row := h.pool.QueryRow(ctx, `SELECT id::text, user_id::text, title, original_title, genre,
	      status, episode_count, episodes_watched, year, country, language,
	      notes, tags, is_public, started_at, completed_at, created_at, updated_at
	      FROM shows WHERE id = $1`, showID)
	return scanShow(row)
}

func parsePagination(c *gin.Context) (page, limit int, orderBy string) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ = strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit < 1 || limit > 100 {
		limit = 20
	}

	sort := c.DefaultQuery("sort", "created_at_desc")
	orderBy = validSorts[sort]
	if orderBy == "" {
		orderBy = "created_at DESC"
	}
	return
}
